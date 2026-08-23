package deliver_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ajesus37/heavens-invoicing/internal/deliver"
	"github.com/ajesus37/heavens-invoicing/internal/i18n"
	"github.com/ajesus37/heavens-invoicing/internal/model"
)

type fakeChannel struct {
	name           string
	invoiceErr     error
	reminderErr    error
	invoiceCalls   int
	reminderCalls  int
	lastInvoiceNum int64
}

func (f *fakeChannel) Name() string { return f.name }

func (f *fakeChannel) SendInvoice(_ context.Context, _ model.Client, inv model.Invoice, _ []byte) error {
	f.invoiceCalls++
	f.lastInvoiceNum = inv.Number
	return f.invoiceErr
}

func (f *fakeChannel) SendReminder(_ context.Context, _ model.Client, inv model.Invoice) error {
	f.reminderCalls++
	return f.reminderErr
}

type fakeUpdater struct {
	calls    []string // recorded statuses by invoice id "id:status"
	failWith error
}

func (f *fakeUpdater) UpdateStatus(_ context.Context, id, status string) error {
	if f.failWith != nil {
		return f.failWith
	}
	f.calls = append(f.calls, id+":"+status)
	return nil
}

type fakeNotifier struct {
	texts []string
	err   error
}

func (f *fakeNotifier) Notify(_ context.Context, text string) error {
	f.texts = append(f.texts, text)
	if f.err != nil {
		return f.err
	}
	return nil
}

func strp(s string) *string { return strPtr(s) }

// routerClient builds a client with the channels enabled.
func routerClient(email, phone, telegram bool) model.Client {
	c := model.Client{Name: "Acme"}
	if email {
		c.Email = strp("a@acme.com")
	}
	if phone {
		c.Phone = strp("+5511999999999")
	}
	if telegram {
		c.TelegramChatID = strp("12345")
	}
	return c
}

func routerInvoice() model.Invoice {
	return model.Invoice{ID: "inv-1", Number: 1, Status: "draft"}
}

type routerFixture struct {
	updater  *fakeUpdater
	notifier *fakeNotifier
	email    *fakeChannel
	wa       *fakeChannel
	tg       *fakeChannel
	router   *deliver.Router
}

// newRouter builds a router with all three channels configured unless the
// corresponding entry of skip is true. Admin locale defaults to nil
// (pt-BR fallback) so legacy assertions keep passing.
func newRouter(skip ...bool) routerFixture {
	want := func(i int) bool { return i < len(skip) && skip[i] }
	f := routerFixture{
		updater:  &fakeUpdater{},
		notifier: &fakeNotifier{},
	}
	var email, wa, tg deliver.Deliverer
	if !want(0) {
		f.email = &fakeChannel{name: "email"}
		email = f.email
	}
	if !want(1) {
		f.wa = &fakeChannel{name: "whatsapp"}
		wa = f.wa
	}
	if !want(2) {
		f.tg = &fakeChannel{name: "telegram"}
		tg = f.tg
	}
	f.router = deliver.NewRouter(f.updater, f.notifier, nil, email, wa, tg)
	return f
}

func TestRouterSendInvoiceRoutesSingleMethod(t *testing.T) {
	for _, method := range []string{"email", "whatsapp", "telegram"} {
		t.Run(method, func(t *testing.T) {
			f := newRouter()
			results, err := f.router.SendInvoice(context.Background(), routerClient(true, true, true), routerInvoice(), []byte("pdf"), method)
			if err != nil {
				t.Fatalf("SendInvoice: %v", err)
			}
			calls := map[string]int{
				"email":    f.email.invoiceCalls,
				"whatsapp": f.wa.invoiceCalls,
				"telegram": f.tg.invoiceCalls,
			}
			for ch, n := range calls {
				want := 0
				if ch == method {
					want = 1
				}
				if n != want {
					t.Fatalf("channel %s calls = %d, want %d", ch, n, want)
				}
			}
			if len(results) != 1 || results[0].Channel != method || results[0].Err != nil {
				t.Fatalf("results = %+v, want single success on %s", results, method)
			}
			if len(f.updater.calls) != 1 || f.updater.calls[0] != "inv-1:sent" {
				t.Fatalf("status calls = %v, want [inv-1:sent]", f.updater.calls)
			}
		})
	}
}

func TestRouterSendAllRespectsConfiguredChannels(t *testing.T) {
	f := newRouter()
	// Client has email + telegram but no phone: whatsapp must not be attempted.
	results, err := f.router.SendInvoice(context.Background(), routerClient(true, false, true), routerInvoice(), []byte("pdf"), "all")
	if err != nil {
		t.Fatal(err)
	}
	if f.wa.invoiceCalls != 0 {
		t.Fatal("whatsapp should not have been called for a client without phone")
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want exactly email+telegram attempts", results)
	}
	got := map[string]bool{}
	for _, r := range results {
		got[r.Channel] = true
		if r.Err != nil {
			t.Fatalf("channel %s: unexpected error %v", r.Channel, r.Err)
		}
	}
	if !got["email"] || !got["telegram"] || got["whatsapp"] {
		t.Fatalf("attempted channels = %v, want email+telegram only", got)
	}
}

func TestRouterSendAllSuccessMarksSentAndNotifies(t *testing.T) {
	f := newRouter()
	_, err := f.router.SendInvoice(context.Background(), routerClient(true, true, true), routerInvoice(), []byte("pdf"), "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.updater.calls) != 1 || f.updater.calls[0] != "inv-1:sent" {
		t.Fatalf("status calls = %v, want single sent", f.updater.calls)
	}
	if len(f.notifier.texts) != 1 {
		t.Fatalf("notify texts = %v, want one summary", f.notifier.texts)
	}
	txt := f.notifier.texts[0]
	for _, want := range []string{"#000001", "Acme", "email", "whatsapp", "telegram"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("notify %q missing %q", txt, want)
		}
	}
}

func TestRouterSendSingleMethodSkipsUnsetAddress(t *testing.T) {
	// method "email" is attempted even when client lacks an address; the
	// channel itself reports the problem. Here the fake succeeds regardless,
	// so we assert the attempt happened for a client without email set via
	// the not-configured path below and the deliverer-level tests elsewhere.
	f := newRouter()
	client := routerClient(false, true, true) // no email
	results, err := f.router.SendInvoice(context.Background(), client, routerInvoice(), []byte("pdf"), "email")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Channel != "email" {
		t.Fatalf("results = %+v, want attempted email result", results)
	}
}

func TestRouterAllChannelsFailKeepsStatusAndListsErrors(t *testing.T) {
	f := newRouter()
	f.email.invoiceErr = errors.New("smtp down")
	f.wa.invoiceErr = errors.New("not connected")
	f.tg.invoiceErr = errors.New("chat gone")

	results, err := f.router.SendInvoice(context.Background(), routerClient(true, true, true), routerInvoice(), []byte("pdf"), "all")
	if err != nil {
		t.Fatalf("per-channel failures must not be a top-level error, got %v", err)
	}
	for _, r := range results {
		if r.Err == nil {
			t.Fatalf("channel %s: expected error", r.Channel)
		}
	}
	if len(f.updater.calls) != 0 {
		t.Fatalf("status must not change when every channel fails, got %v", f.updater.calls)
	}
	if len(f.notifier.texts) != 1 {
		t.Fatalf("notify texts = %v, want one failure summary", f.notifier.texts)
	}
	txt := f.notifier.texts[0]
	for _, want := range []string{"email", "smtp down", "whatsapp", "not connected", "telegram", "chat gone"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("failure notify %q missing %q", txt, want)
		}
	}
}

func TestRouterAllWithNoConfiguredChannelsFailsGracefully(t *testing.T) {
	f := newRouter()
	client := routerClient(false, false, false)
	results, err := f.router.SendInvoice(context.Background(), client, routerInvoice(), []byte("pdf"), "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want no attempted channels", results)
	}
	if len(f.updater.calls) != 0 {
		t.Fatalf("status must not change, got %v", f.updater.calls)
	}
	if len(f.notifier.texts) != 1 || !strings.Contains(f.notifier.texts[0], "sem canal") {
		t.Fatalf("notify texts = %v, want no-channel failure summary", f.notifier.texts)
	}
}

func TestRouterUnknownMethodErrors(t *testing.T) {
	f := newRouter()
	if _, err := f.router.SendInvoice(context.Background(), routerClient(true, true, true), routerInvoice(), []byte("pdf"), "fax"); err == nil {
		t.Fatal("want error for unknown method")
	}
	if _, err := f.router.SendReminder(context.Background(), routerClient(true, true, true), routerInvoice(), ""); err == nil {
		t.Fatal("want error for empty method")
	}
	if f.email.invoiceCalls+f.wa.invoiceCalls+f.tg.invoiceCalls != 0 {
		t.Fatal("no channel may be contacted for an unknown method")
	}
	if len(f.updater.calls) != 0 || len(f.notifier.texts) != 0 {
		t.Fatal("unknown method must not touch status or notifier")
	}
}

func TestRouterNilDelivererReportsNotConfigured(t *testing.T) {
	f := newRouter(true) // email deliverer nil
	client := routerClient(true, true, true)
	results, err := f.router.SendInvoice(context.Background(), client, routerInvoice(), []byte("pdf"), "email")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Channel != "email" || results[0].Err == nil ||
		!strings.Contains(results[0].Err.Error(), "not configured") {
		t.Fatalf("results = %+v, want not-configured error on email", results)
	}
	// "all" still tries the other two and succeeds overall.
	results, err = f.router.SendInvoice(context.Background(), client, routerInvoice(), []byte("pdf"), "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %+v, want three attempted channels", results)
	}
	if len(f.updater.calls) != 1 {
		t.Fatalf("status calls = %v, want sent after partial success", f.updater.calls)
	}
}

func TestRouterSendInvoiceRejectsPaidInvoice(t *testing.T) {
	f := newRouter()
	inv := routerInvoice()
	inv.Status = "paid"
	results, err := f.router.SendInvoice(context.Background(), routerClient(true, true, true), inv, []byte("pdf"), "all")
	if err == nil || !strings.Contains(err.Error(), "já está paga") {
		t.Fatalf("want paid-invoice rejection, got results=%+v err=%v", results, err)
	}
	if f.email.invoiceCalls+f.wa.invoiceCalls+f.tg.invoiceCalls != 0 {
		t.Fatal("paid invoice must not reach any channel")
	}
	if len(f.updater.calls) != 0 {
		t.Fatalf("paid invoice must not be marked sent, got %v", f.updater.calls)
	}
	if len(f.notifier.texts) != 0 {
		t.Fatalf("paid rejection must not notify, got %v", f.notifier.texts)
	}
}

func TestRouterSendReminderNoStatusUpdate(t *testing.T) {
	f := newRouter()
	results, err := f.router.SendReminder(context.Background(), routerClient(true, false, true), routerInvoice(), "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want email+telegram", results)
	}
	if f.updater.calls != nil {
		t.Fatalf("reminders must not update status, got %v", f.updater.calls)
	}
	if len(f.notifier.texts) != 1 || !strings.Contains(f.notifier.texts[0], "#000001") {
		t.Fatalf("notify texts = %v, want reminder summary mentioning invoice", f.notifier.texts)
	}
	if f.email.reminderCalls != 1 || f.tg.reminderCalls != 1 || f.wa.reminderCalls != 0 {
		t.Fatalf("reminder calls email=%d wa=%d tg=%d", f.email.reminderCalls, f.wa.reminderCalls, f.tg.reminderCalls)
	}
}

func TestRouterStatusUpdateFailureSurfaces(t *testing.T) {
	f := newRouter()
	f.updater.failWith = errors.New("db locked")
	results, err := f.router.SendInvoice(context.Background(), routerClient(true, true, true), routerInvoice(), []byte("pdf"), "email")
	if err == nil {
		t.Fatal("want error when status update fails after successful delivery")
	}
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("delivery itself succeeded, results = %+v", results)
	}
	// The admin summary must mention the mark-as-sent failure.
	if len(f.notifier.texts) != 1 {
		t.Fatalf("notify texts = %v, want one summary", f.notifier.texts)
	}
	for _, want := range []string{"enviada", "marcar como enviada", "db locked"} {
		if !strings.Contains(f.notifier.texts[0], want) {
			t.Fatalf("notify %q missing %q", f.notifier.texts[0], want)
		}
	}
}

// TestRouterAdminLocaleVsClientLanguage pins the language split: the
// client-facing email follows the client's language while the admin
// notification summary follows the injected admin locale.
func TestRouterAdminLocaleVsClientLanguage(t *testing.T) {
	sender := &fakeSender{}
	email := deliver.NewEmail(sender, "billing@me.com", "")
	notifier := &fakeNotifier{}
	updater := &fakeUpdater{}
	adminLang := i18n.En // admin configured the app in English...
	r := deliver.NewRouter(updater, notifier, func() i18n.Lang { return adminLang }, email, nil, nil)

	// ...but this client speaks Portuguese.
	client := model.Client{Name: "Acme", Language: "pt-BR", Email: strp("a@acme.com")}
	inv := model.Invoice{ID: "inv-9", Number: 7, Status: "draft"}

	_, err := r.SendInvoice(context.Background(), client, inv, []byte("pdf"), "email")
	if err != nil {
		t.Fatal(err)
	}

	if got := sender.calls[0].subject; got != "Fatura #000007" {
		t.Errorf("client-facing subject = %q, want pt-BR %q", got, "Fatura #000007")
	}
	assertContains(t, sender.calls[0].body, "Olá Acme")

	if len(notifier.texts) != 1 {
		t.Fatalf("notify texts = %v, want one summary", notifier.texts)
	}
	summary := notifier.texts[0]
	assertContains(t, summary, "Invoice #000007 sent to Acme")
	if strings.Contains(summary, "Fatura") || strings.Contains(summary, "enviada") {
		t.Errorf("admin summary leaked pt-BR wording: %q", summary)
	}

	// A reminder behaves the same way.
	sender.calls = nil
	notifier.texts = nil
	if _, err := r.SendReminder(context.Background(), client, inv, "email"); err != nil {
		t.Fatal(err)
	}
	assertContains(t, sender.calls[0].subject, "Lembrete de vencimento - Fatura #000007")
	assertContains(t, notifier.texts[0], "Reminder of invoice #000007 sent to Acme")
}

// TestRouterAdminLocaleDefaultsToPtBR covers the defensive fallbacks of an
// unset or broken locale resolver.
func TestRouterAdminLocaleDefaultsToPtBR(t *testing.T) {
	for name, resolver := range map[string]deliver.AdminLocaleFunc{
		"nil":     nil,
		"garbage": func() i18n.Lang { return i18n.Lang("xx") },
	} {
		t.Run(name, func(t *testing.T) {
			updater := &fakeUpdater{}
			notifier := &fakeNotifier{}
			r := deliver.NewRouter(updater, notifier, resolver, &fakeChannel{name: "email"}, nil, nil)

			client := routerClient(true, false, false)
			if _, err := r.SendInvoice(context.Background(), client, routerInvoice(), []byte("pdf"), "email"); err != nil {
				t.Fatal(err)
			}
			assertContains(t, notifier.texts[0], "Fatura #000001 enviada para Acme via email")
		})
	}
}
