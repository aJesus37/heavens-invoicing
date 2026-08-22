package schedule

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// --- fakes ---

type statusCall struct {
	id     string
	status string
}

type fakeInvoices struct {
	mu          sync.Mutex
	templates   map[string]*model.Invoice
	byID        map[string]*model.Invoice
	clones      []*model.Invoice
	statusCalls []statusCall
	nextNumber  int64
	cloneErr    error
	statusErr   error
	listErr     error
}

func newFakeInvoices() *fakeInvoices {
	return &fakeInvoices{
		templates:  map[string]*model.Invoice{},
		byID:       map[string]*model.Invoice{},
		nextNumber: 100,
	}
}

func (f *fakeInvoices) addTemplate(tpl *model.Invoice) *model.Invoice {
	f.mu.Lock()
	defer f.mu.Unlock()
	if tpl.ID == "" {
		tpl.ID = fmt.Sprintf("tpl-%d", len(f.templates)+1)
	}
	f.templates[tpl.ID] = tpl
	return tpl
}

// seed registers a live invoice (e.g. an overdue one) directly.
func (f *fakeInvoices) seed(inv *model.Invoice) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[inv.ID] = inv
}

func copyInv(in *model.Invoice) *model.Invoice {
	out := *in
	return &out
}

func (f *fakeInvoices) CloneFromTemplate(_ context.Context, templateID string, issueDate, dueDate time.Time) (*model.Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cloneErr != nil {
		return nil, f.cloneErr
	}
	tpl, ok := f.templates[templateID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	f.nextNumber++
	inv := &model.Invoice{
		ID:        fmt.Sprintf("inv-%d", f.nextNumber),
		ClientID:  tpl.ClientID,
		Number:    f.nextNumber,
		Status:    "draft",
		IssueDate: issueDate,
		DueDate:   dueDate,
		Notes:     tpl.Notes,
		Total:     tpl.Total,
		Items:     tpl.Items,
	}
	f.clones = append(f.clones, inv)
	f.byID[inv.ID] = inv
	return copyInv(inv), nil
}

func (f *fakeInvoices) Get(_ context.Context, id string) (*model.Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	inv, ok := f.byID[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return copyInv(inv), nil
}

func (f *fakeInvoices) UpdateStatus(_ context.Context, id, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return f.statusErr
	}
	inv, ok := f.byID[id]
	if !ok {
		return repo.ErrNotFound
	}
	inv.Status = status
	f.statusCalls = append(f.statusCalls, statusCall{id: id, status: status})
	return nil
}

func (f *fakeInvoices) ListByStatus(_ context.Context, statuses ...string) ([]*model.Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]*model.Invoice, 0)
	for _, inv := range f.byID {
		if slices.Contains(statuses, inv.Status) {
			out = append(out, copyInv(inv))
		}
	}
	slices.SortFunc(out, func(a, b *model.Invoice) int { return int(a.Number - b.Number) })
	return out, nil
}

type fakeClients struct {
	mu      sync.Mutex
	byID    map[string]*model.Client
	getErrs map[string]error
}

func newFakeClients(cs ...*model.Client) *fakeClients {
	f := &fakeClients{byID: map[string]*model.Client{}, getErrs: map[string]error{}}
	for _, c := range cs {
		f.byID[c.ID] = c
	}
	return f
}

func (f *fakeClients) Get(_ context.Context, id string) (*model.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.getErrs[id]; ok {
		return nil, err
	}
	c, ok := f.byID[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	out := *c
	return &out, nil
}

type fakeRecurring struct {
	mu          sync.Mutex
	active      []*model.RecurringSchedule
	listCalls   int
	updateCalls []*model.RecurringSchedule
	updateErr   error
	listErr     error
}

func (f *fakeRecurring) ListActive(_ context.Context) ([]*model.RecurringSchedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]*model.RecurringSchedule, 0, len(f.active))
	for _, s := range f.active {
		cp := *s
		if s.LastSentDate != nil {
			d := *s.LastSentDate
			cp.LastSentDate = &d
		}
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeRecurring) Update(_ context.Context, s *model.RecurringSchedule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	cp := *s
	if s.LastSentDate != nil {
		d := *s.LastSentDate
		cp.LastSentDate = &d
	}
	f.updateCalls = append(f.updateCalls, &cp)
	return nil
}

type sendCall struct {
	kind   string // invoice|reminder
	number int64
	method string
	pdfLen int
}

// fakeSender mimics deliver.Router semantics: a configured failing method
// yields per-channel errors with a nil top-level error; success flips the
// invoice to "sent" through the invoice store (reminders never do).
type fakeSender struct {
	mu          sync.Mutex
	calls       []sendCall
	failMethods map[string]bool
	invoices    *fakeInvoices
	reminderErr error
}

func (f *fakeSender) record(call sendCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeSender) snapshot() []sendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeSender) SendInvoice(_ context.Context, c model.Client, inv model.Invoice, pdfBytes []byte, method string) ([]deliver.ChannelResult, error) {
	f.record(sendCall{kind: "invoice", number: inv.Number, method: method, pdfLen: len(pdfBytes)})
	if f.failMethods[method] {
		if method == deliver.MethodAll {
			return []deliver.ChannelResult{}, nil // client without any channel
		}
		return []deliver.ChannelResult{{Channel: method, Err: errors.New("channel down")}}, nil
	}
	if f.invoices != nil {
		_ = f.invoices.UpdateStatus(context.Background(), inv.ID, "sent")
	}
	return []deliver.ChannelResult{{Channel: method}}, nil
}

func (f *fakeSender) SendReminder(_ context.Context, c model.Client, inv model.Invoice, method string) ([]deliver.ChannelResult, error) {
	f.record(sendCall{kind: "reminder", number: inv.Number, method: method})
	if f.reminderErr != nil {
		return nil, f.reminderErr
	}
	if f.failMethods[method] {
		return []deliver.ChannelResult{}, nil
	}
	return []deliver.ChannelResult{{Channel: deliver.MethodEmail}}, nil
}

type fakeNotifier struct {
	mu    sync.Mutex
	texts []string
	err   error
}

func (f *fakeNotifier) Notify(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, text)
	return f.err
}

func (f *fakeNotifier) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.texts)
}

type countingRenderer struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (c *countingRenderer) render(w io.Writer, _ pdf.SenderInfo, _ model.Client, _ model.Invoice) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	_, err := io.WriteString(w, "%PDF-fake")
	return err
}

// --- harness ---

const testNow = "2026-03-31"

type harness struct {
	sched    *Scheduler
	invoices *fakeInvoices
	clients  *fakeClients
	recur    *fakeRecurring
	sender   *fakeSender
	notifier *fakeNotifier
	render   *countingRenderer
}

func newHarness(now time.Time, cs ...*model.Client) *harness {
	h := &harness{
		invoices: newFakeInvoices(),
		clients:  newFakeClients(append([]*model.Client{testClient}, cs...)...),
		recur:    &fakeRecurring{},
		sender:   &fakeSender{failMethods: map[string]bool{}, invoices: nil},
		notifier: &fakeNotifier{},
		render:   &countingRenderer{},
	}
	h.sched = New(h.recur, h.invoices, h.clients, h.sender, h.notifier, pdf.SenderInfo{}, func() time.Time { return now })
	h.sched.renderPDF = h.render.render
	h.sender.invoices = h.invoices
	return h
}

var testClient = &model.Client{ID: "client-1", Name: "Acme Ltda"}

func testTemplate(clientID string) *model.Invoice {
	return &model.Invoice{
		ClientID: clientID,
		Status:   "draft",
		Notes:    "mensalidade",
		Items: []*model.InvoiceItem{
			{Description: "Serviço", UnitPrice: 15000, Quantity: 1},
		},
	}
}

// --- unit: date arithmetic ---

func TestNextSendDate(t *testing.T) {
	tests := []struct {
		name string
		from string
		freq string
		want string
	}{
		{"weekly adds seven days", "2026-01-15", "weekly", "2026-01-22"},
		{"monthly mid-month keeps day", "2026-01-15", "monthly", "2026-02-15"},
		{"monthly month-end clamps to 28", "2026-03-31", "monthly", "2026-04-28"},
		{"monthly jan-31 lands on feb-28", "2026-01-31", "monthly", "2026-02-28"},
		{"quarterly clamps", "2026-03-31", "quarterly", "2026-06-28"},
		{"yearly crosses year clamped", "2026-01-31", "yearly", "2027-01-28"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := nextSendDate(day(tt.from), tt.freq)
			if !ok {
				t.Fatalf("nextSendDate(%s, %s) rejected frequency", tt.from, tt.freq)
			}
			if !got.Equal(day(tt.want)) {
				t.Fatalf("nextSendDate(%s, %s) = %s, want %s", tt.from, tt.freq, got.Format("2006-01-02"), tt.want)
			}
		})
	}
}

// --- recurring fire ---

func TestTickFiresDueMonthlySchedule(t *testing.T) {
	h := newHarness(day(testNow))
	tpl := h.invoices.addTemplate(testTemplate(testClient.ID))
	h.recur.active = []*model.RecurringSchedule{{
		ID:                "sched-1",
		ClientID:          testClient.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "monthly",
		NextSendDate:      day("2026-03-30"), // due yesterday
		DeliveryMethod:    "email",
	}}

	err := h.sched.Tick(context.Background())

	if err != nil {
		t.Fatalf("tick returned error: %v", err)
	}
	if len(h.invoices.clones) != 1 {
		t.Fatalf("clones = %d, want 1", len(h.invoices.clones))
	}
	clone := h.invoices.clones[0]
	if !clone.IssueDate.Equal(day(testNow)) || !clone.DueDate.Equal(day("2026-04-30")) {
		t.Fatalf("clone dates = %s..%s, want %s..%s",
			clone.IssueDate.Format("2006-01-02"), clone.DueDate.Format("2006-01-02"),
			testNow, "2026-04-30")
	}
	if clone.Status != "sent" {
		t.Fatalf("clone status = %q, want sent (flipped by router)", clone.Status)
	}
	if h.render.snapshotCalls() != 1 {
		t.Fatalf("pdf renders = %d, want 1", h.render.snapshotCalls())
	}
	calls := h.sender.snapshot()
	if len(calls) != 1 || calls[0].kind != "invoice" || calls[0].method != "email" || calls[0].number != clone.Number || calls[0].pdfLen == 0 {
		t.Fatalf("sender calls = %+v, want one invoice call via email with pdf bytes", calls)
	}
	if len(h.recur.updateCalls) != 1 {
		t.Fatalf("schedule updates = %d, want 1", len(h.recur.updateCalls))
	}
	upd := h.recur.updateCalls[0]
	if upd.LastSentDate == nil || !upd.LastSentDate.Equal(day(testNow)) {
		t.Fatalf("last_sent_date = %v, want %s", upd.LastSentDate, testNow)
	}
	if !upd.NextSendDate.Equal(day("2026-04-28")) { // Mar-31 monthly -> clamped
		t.Fatalf("next_send_date = %s, want 2026-04-28", upd.NextSendDate.Format("2006-01-02"))
	}
}

func TestTickLeavesFutureScheduleUntouched(t *testing.T) {
	h := newHarness(day(testNow))
	tpl := h.invoices.addTemplate(testTemplate(testClient.ID))
	h.recur.active = []*model.RecurringSchedule{{
		ID:                "sched-1",
		ClientID:          testClient.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "weekly",
		NextSendDate:      day("2026-04-01"), // tomorrow
		DeliveryMethod:    "email",
	}}

	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}
	if len(h.invoices.clones) != 0 || len(h.sender.snapshot()) != 0 || len(h.recur.updateCalls) != 0 {
		t.Fatalf("future schedule was touched: clones=%d sends=%d updates=%d",
			len(h.invoices.clones), len(h.sender.snapshot()), len(h.recur.updateCalls))
	}
}

// The API persists user-entered dates as UTC midnight; on hosts away from
// UTC the scheduler must still treat them as their local calendar day.
func TestTickFiresUTCStoredScheduleOnLocalDay(t *testing.T) {
	loc := time.FixedZone("UTC+3", 3*3600)
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, loc) // local noon of the intended day
	h := newHarness(now)
	tpl := h.invoices.addTemplate(testTemplate(testClient.ID))
	h.recur.active = []*model.RecurringSchedule{{
		ID:                "sched-tz",
		ClientID:          testClient.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "monthly",
		// Exactly what time.Parse("2006-01-02") stores for 2026-04-01.
		NextSendDate:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		DeliveryMethod: "email",
	}}

	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}
	if len(h.invoices.clones) != 1 {
		t.Fatalf("clones = %d, want 1: UTC-stored date must fire during its local day", len(h.invoices.clones))
	}
	if len(h.recur.updateCalls) != 1 {
		t.Fatalf("updates = %d, want 1", len(h.recur.updateCalls))
	}
	wantNext := time.Date(2026, 5, 1, 0, 0, 0, 0, loc)
	if got := h.recur.updateCalls[0].NextSendDate; !got.Equal(wantNext) {
		t.Fatalf("next_send_date = %s, want %s (local midnight)", got, wantNext)
	}
}

func TestOverdueDaysCountedInClockLocation(t *testing.T) {
	loc := time.FixedZone("UTC+3", 3*3600)
	now := time.Date(2026, 4, 11, 12, 0, 0, 0, loc)
	h := newHarness(now)
	seedOverdueInvoice(h, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)) // stored like the API does

	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}
	texts := h.notifier.snapshot()
	if len(texts) != 1 || !strings.Contains(texts[0], "venceu há 10 dias") {
		t.Fatalf("notifications = %+v, want ask counting 10 overdue days in clock location", texts)
	}
}

func TestTickInactiveSchedulesIgnored(t *testing.T) {
	h := newHarness(day(testNow))
	h.recur.active = nil // ListActive returns only active rows

	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}
	if len(h.invoices.clones) != 0 {
		t.Fatalf("inactive schedule fired: %+v", h.invoices.clones)
	}
}

func TestTickTotalDeliveryFailureKeepsScheduleAndNotifiesAdmin(t *testing.T) {
	h := newHarness(day(testNow))
	tpl := h.invoices.addTemplate(testTemplate(testClient.ID))
	h.recur.active = []*model.RecurringSchedule{{
		ID:                "sched-1",
		ClientID:          testClient.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "monthly",
		NextSendDate:      day("2026-03-01"),
		DeliveryMethod:    "email",
	}}
	h.sender.failMethods["email"] = true

	err := h.sched.Tick(context.Background())

	if err == nil || !strings.Contains(err.Error(), "sched-1") {
		t.Fatalf("tick error = %v, want failure mentioning sched-1", err)
	}
	if len(h.invoices.clones) != 1 {
		t.Fatalf("clone should have been attempted, got %d", len(h.invoices.clones))
	}
	if clone := h.invoices.clones[0]; clone.Status != "draft" {
		t.Fatalf("clone status = %q, want draft (nothing sent)", clone.Status)
	}
	if len(h.recur.updateCalls) != 0 {
		t.Fatalf("failed schedule must not advance, got updates %+v", h.recur.updateCalls)
	}
	texts := h.notifier.snapshot()
	if len(texts) != 1 || !strings.Contains(texts[0], "sched-1") || !strings.Contains(texts[0], "falhou") {
		t.Fatalf("admin notifications = %+v, want one failure notice", texts)
	}
}

func TestTickIsolatesPerScheduleFailures(t *testing.T) {
	bob := &model.Client{ID: "client-2", Name: "Bob"}
	h := newHarness(day(testNow), bob)
	tplA := h.invoices.addTemplate(testTemplate(testClient.ID))
	tplB := h.invoices.addTemplate(testTemplate(bob.ID))
	h.recur.active = []*model.RecurringSchedule{
		{ID: "sched-bad", ClientID: bob.ID, InvoiceTemplateID: tplB.ID, Frequency: "monthly", NextSendDate: day("2026-03-01"), DeliveryMethod: "telegram"},
		{ID: "sched-good", ClientID: testClient.ID, InvoiceTemplateID: tplA.ID, Frequency: "monthly", NextSendDate: day("2026-03-02"), DeliveryMethod: "email"},
	}
	h.sender.failMethods["telegram"] = true

	err := h.sched.Tick(context.Background())

	if err == nil || !strings.Contains(err.Error(), "sched-bad") {
		t.Fatalf("tick error = %v, want failure mentioning sched-bad", err)
	}
	calls := h.sender.snapshot()
	if len(calls) != 2 {
		t.Fatalf("both schedules should be attempted, got %+v", calls)
	}
	var goodUpdate bool
	for _, u := range h.recur.updateCalls {
		if u.ID == "sched-good" {
			goodUpdate = true
		}
		if u.ID == "sched-bad" {
			t.Fatal("failing schedule must not advance")
		}
	}
	if !goodUpdate {
		t.Fatal("healthy schedule must still advance despite sibling failure")
	}
}

func TestTickAttemptsEachScheduleOncePerDay(t *testing.T) {
	h := newHarness(day(testNow))
	tpl := h.invoices.addTemplate(testTemplate(testClient.ID))
	h.recur.active = []*model.RecurringSchedule{{
		ID:                "sched-1",
		ClientID:          testClient.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "weekly",
		NextSendDate:      day("2026-03-01"),
		DeliveryMethod:    "email",
	}}
	h.sender.failMethods["email"] = true

	for i := 0; i < 3; i++ {
		_ = h.sched.Tick(context.Background()) // several ticks on the same day
	}
	if got := len(h.sender.snapshot()); got != 1 {
		t.Fatalf("same-day ticks fired %d times, want exactly 1 (retry tomorrow)", got)
	}

	// The next day the schedule is retried.
	h2 := newHarness(day("2026-04-01"))
	tpl2 := h2.invoices.addTemplate(testTemplate(testClient.ID))
	h2.recur.active = []*model.RecurringSchedule{{
		ID:                "sched-1",
		ClientID:          testClient.ID,
		InvoiceTemplateID: tpl2.ID,
		Frequency:         "weekly",
		NextSendDate:      day("2026-03-01"),
		DeliveryMethod:    "email",
	}}
	h2.sender.failMethods["email"] = true
	_ = h2.sched.Tick(context.Background())
	if got := len(h2.sender.snapshot()); got != 1 {
		t.Fatalf("fresh day should attempt once, got %d", got)
	}
}

// --- overdue confirmation flow ---

func seedOverdueInvoice(h *harness, dueDate time.Time) *model.Invoice {
	inv := &model.Invoice{
		ID:       "overdue-1",
		ClientID: testClient.ID,
		Number:   42,
		Status:   "sent",
		Total:    123456,
		DueDate:  dueDate,
	}
	h.invoices.seed(inv)
	return inv
}

func TestOverdueAskThenRemindThenMarkOverdue(t *testing.T) {
	now := day(testNow)
	h := newHarness(now)
	seedOverdueInvoice(h, now.AddDate(0, 0, -10))

	// First tick: past grace (10 > 7) -> ask the admin, no reminder yet.
	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	texts := h.notifier.snapshot()
	if len(texts) != 1 ||
		!strings.Contains(texts[0], "Fatura #000042") ||
		!strings.Contains(texts[0], "R$ 1.234,56") ||
		!strings.Contains(texts[0], "Acme Ltda") ||
		!strings.Contains(texts[0], "venceu há 10 dias") ||
		!strings.Contains(texts[0], "/paid 42") {
		t.Fatalf("admin ask = %+v, want payment confirmation question", texts)
	}
	if got := len(h.sender.snapshot()); got != 0 {
		t.Fatalf("no reminder should be sent before reminderAfter elapses, got %+v", h.sender.snapshot())
	}
	if _, ok := h.sched.pending["overdue-1"]; !ok {
		t.Fatal("pending confirmation not registered")
	}

	// A few hours later: still waiting, nothing new happens.
	h2 := newHarness(now.Add(2 * time.Hour))
	seedOverdueInvoice(h2, now.AddDate(0, 0, -10))
	h2.sched.pending["overdue-1"] = now // pretend the ask already happened
	if err := h2.sched.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if got := len(h2.notifier.snapshot()) + len(h2.sender.snapshot()); got != 0 {
		t.Fatalf("pending ask under reminderAfter must stay quiet, got %d actions", got)
	}

	// More than reminderAfter later and still unpaid: remind the client and
	// flag the invoice overdue.
	h3 := newHarness(now.Add(25 * time.Hour))
	seedOverdueInvoice(h3, now.AddDate(0, 0, -10))
	h3.sched.pending["overdue-1"] = now
	if err := h3.sched.Tick(context.Background()); err != nil {
		t.Fatalf("third tick: %v", err)
	}
	calls := h3.sender.snapshot()
	if len(calls) != 1 || calls[0].kind != "reminder" || calls[0].method != "all" || calls[0].number != 42 {
		t.Fatalf("reminder calls = %+v, want one reminder for #42 via all", calls)
	}
	stored, err := h3.invoices.Get(context.Background(), "overdue-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "overdue" {
		t.Fatalf("status after reminder = %q, want overdue", stored.Status)
	}
	if _, ok := h3.sched.pending["overdue-1"]; ok {
		t.Fatal("confirmation should be resolved after the reminder")
	}
}

func TestOverdueAlreadyPaidGetsNoAskOrReminder(t *testing.T) {
	now := day(testNow)
	h := newHarness(now)
	inv := seedOverdueInvoice(h, now.AddDate(0, 0, -12))
	inv.Status = "paid" // admin paid it before the tick ran

	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := len(h.notifier.snapshot()) + len(h.sender.snapshot()); got != 0 {
		t.Fatalf("paid invoice must be ignored, got %d actions", got)
	}

	// Stale pending entry for a paid invoice is reconciled away.
	h2 := newHarness(now.Add(25 * time.Hour))
	paid := seedOverdueInvoice(h2, now.AddDate(0, 0, -12))
	paid.Status = "paid"
	h2.sched.pending["overdue-1"] = now
	if err := h2.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(h2.sender.snapshot()) != 0 {
		t.Fatalf("paid invoice must not be reminded: %+v", h2.sender.snapshot())
	}
	if _, ok := h2.sched.pending["overdue-1"]; ok {
		t.Fatal("stale pending entry for paid invoice should be dropped")
	}
}

func TestOverdueWithinGraceIsIgnored(t *testing.T) {
	now := day(testNow)
	h := newHarness(now)
	seedOverdueInvoice(h, now.AddDate(0, 0, -7)) // exactly graceDays old: not yet

	if err := h.sched.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := len(h.notifier.snapshot()) + len(h.sender.snapshot()); got != 0 {
		t.Fatalf("invoice inside grace window must be ignored, got %d actions", got)
	}
}

func TestOverdueReminderFailureRetriesLater(t *testing.T) {
	now := day(testNow)
	h := newHarness(now)
	seedOverdueInvoice(h, now.AddDate(0, 0, -10))
	h.sched.pending["overdue-1"] = now.Add(-25 * time.Hour)
	h.sender.failMethods[deliver.MethodAll] = true

	err := h.sched.Tick(context.Background())

	if err == nil || !strings.Contains(err.Error(), "overdue-1") {
		t.Fatalf("tick error = %v, want failure mentioning overdue-1", err)
	}
	stored, _ := h.invoices.Get(context.Background(), "overdue-1")
	if stored.Status != "sent" {
		t.Fatalf("status after failed reminder = %q, want sent (retry later)", stored.Status)
	}
	askedAt := h.sched.pending["overdue-1"]
	if askedAt.IsZero() || !askedAt.After(now.Add(-time.Minute)) {
		t.Fatalf("askedAt should reset to ~now for a later retry, got %v", askedAt)
	}
}

// --- plumbing ---

func TestTickPropagatesListErrors(t *testing.T) {
	h := newHarness(day(testNow))
	h.recur.listErr = errors.New("db down")
	if err := h.sched.Tick(context.Background()); err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("tick error = %v, want db down", err)
	}

	h2 := newHarness(day(testNow))
	h2.invoices.listErr = errors.New("db down")
	if err := h2.sched.Tick(context.Background()); err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("tick error = %v, want db down", err)
	}
}

func TestRunTicksUntilContextCanceled(t *testing.T) {
	h := newHarness(time.Now())
	h.sched.interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.sched.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for h.recur.snapshotListCalls() < 3 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("run loop never reached 3 ticks (got %d)", h.recur.snapshotListCalls())
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on canceled ctx")
	}
}

func (c *countingRenderer) snapshotCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (f *fakeRecurring) snapshotListCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls
}
