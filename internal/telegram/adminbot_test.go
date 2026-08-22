package telegram

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
)

type statusCall struct {
	id     string
	status string
}

type fakeInvoices struct {
	byNumber    map[int64]*model.Invoice
	getErrs     map[int64]error
	updateErr   error
	updateCalls []statusCall
	listed      []string
	listResult  []*model.Invoice
	listErr     error
}

func (f *fakeInvoices) GetByNumber(_ context.Context, number int64) (*model.Invoice, error) {
	if err, ok := f.getErrs[number]; ok {
		return nil, err
	}
	if inv, ok := f.byNumber[number]; ok {
		return inv, nil
	}
	return nil, repo.ErrNotFound
}

func (f *fakeInvoices) UpdateStatus(_ context.Context, id, status string) error {
	f.updateCalls = append(f.updateCalls, statusCall{id: id, status: status})
	return f.updateErr
}

func (f *fakeInvoices) ListByStatus(_ context.Context, statuses ...string) ([]*model.Invoice, error) {
	f.listed = statuses
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

type fakeClients struct {
	names []string
	err   error
}

func (f *fakeClients) List(_ context.Context) ([]*model.Client, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]*model.Client, 0, len(f.names))
	for _, n := range f.names {
		out = append(out, &model.Client{Name: n})
	}
	return out, nil
}

var frozenNow = time.Date(2026, 8, 21, 15, 4, 0, 0, time.UTC)

func newTestBot(invoices *fakeInvoices, clients *fakeClients) (*AdminBot, *fakeInvoices, *fakeClients) {
	if invoices == nil {
		invoices = &fakeInvoices{}
	}
	if clients == nil {
		clients = &fakeClients{}
	}
	bot := NewAdminBot(nil, "777", invoices, clients)
	bot.now = func() time.Time { return frozenNow }
	return bot, invoices, clients
}

func TestAdminBotHandleCommands(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		text     string
		invoices *fakeInvoices
		clients  *fakeClients
		want     string
		contains []string
		check    func(t *testing.T, got string, inv *fakeInvoices, cl *fakeClients)
	}{
		{
			name:     "paid marks invoice paid",
			text:     "/paid 3",
			invoices: &fakeInvoices{byNumber: map[int64]*model.Invoice{3: {ID: "inv-3", Number: 3}}},
			want:     "Fatura #3 marcada como paga ✓",
			check: func(t *testing.T, _ string, inv *fakeInvoices, _ *fakeClients) {
				t.Helper()
				if len(inv.updateCalls) != 1 || inv.updateCalls[0] != (statusCall{id: "inv-3", status: "paid"}) {
					t.Fatalf("update calls = %+v, want [{inv-3 paid}]", inv.updateCalls)
				}
			},
		},
		{
			name:     "paid is case-insensitive",
			text:     "/PAID 7",
			invoices: &fakeInvoices{byNumber: map[int64]*model.Invoice{7: {ID: "i7", Number: 7}}},
			want:     "Fatura #7 marcada como paga ✓",
		},
		{
			name:     "paid unknown invoice",
			text:     "/paid 99",
			invoices: &fakeInvoices{},
			want:     "Fatura #99 não encontrada",
		},
		{
			name:     "paid lookup error surfaced",
			text:     "/paid 5",
			invoices: &fakeInvoices{getErrs: map[int64]error{5: errors.New("db down")}},
			contains: []string{"Erro", "db down"},
		},
		{
			name:     "paid update error surfaced",
			text:     "/paid 2",
			invoices: &fakeInvoices{byNumber: map[int64]*model.Invoice{2: {ID: "i2"}}, updateErr: errors.New("locked")},
			contains: []string{"Erro", "locked"},
		},
		{
			name:     "paid without number shows usage",
			text:     "/paid",
			invoices: &fakeInvoices{},
			want:     "Uso: /paid <número da fatura>",
		},
		{
			name:     "paid with non-numeric argument",
			text:     "/paid abc",
			invoices: &fakeInvoices{},
			want:     `Número de fatura inválido: "abc"`,
		},
		{
			name:     "paid rejects zero",
			text:     "/paid 0",
			invoices: &fakeInvoices{},
			want:     `Número de fatura inválido: "0"`,
		},
		{
			name:     "paid rejects negative",
			text:     "/paid -3",
			invoices: &fakeInvoices{},
			want:     `Número de fatura inválido: "-3"`,
		},
		{
			name:     "paid strips botname suffix from command",
			text:     "/paid@MyInvoiceBot 3",
			invoices: &fakeInvoices{byNumber: map[int64]*model.Invoice{3: {ID: "inv-3", Number: 3}}},
			want:     "Fatura #3 marcada como paga ✓",
		},
		{
			name: "status lists sent and overdue with BRL and dd/mm",
			text: "/status",
			invoices: &fakeInvoices{listResult: []*model.Invoice{
				{Number: 5, Total: 123456, Status: "sent", DueDate: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)},
				{Number: 4, Total: 50000, Status: "overdue", DueDate: time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)},
			}},
			want: "Fatura #5 - R$ 1.234,56 - venc. 05/09 - sent\n" +
				"Fatura #4 - R$ 500,00 - venc. 01/08 - overdue",
			check: func(t *testing.T, _ string, inv *fakeInvoices, _ *fakeClients) {
				t.Helper()
				if !slices.Equal(inv.listed, []string{"sent", "overdue"}) {
					t.Fatalf("statuses queried = %v, want [sent overdue]", inv.listed)
				}
			},
		},
		{
			name:     "status empty",
			text:     "/status",
			invoices: &fakeInvoices{},
			want:     "Nenhuma fatura pendente.",
		},
		{
			name: "upcoming keeps today through day 7, drops yesterday and day 8",
			text: "/upcoming",
			invoices: &fakeInvoices{listResult: []*model.Invoice{
				{Number: 10, Total: 12345, Status: "sent", DueDate: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)},
				{Number: 11, Total: 12345, Status: "sent", DueDate: time.Date(2026, 8, 24, 18, 0, 0, 0, time.UTC)},
				{Number: 12, Total: 12345, Status: "overdue", DueDate: time.Date(2026, 8, 28, 23, 0, 0, 0, time.UTC)},
				{Number: 13, Total: 12345, Status: "sent", DueDate: time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)},
				{Number: 14, Total: 12345, Status: "overdue", DueDate: time.Date(2026, 8, 20, 23, 59, 0, 0, time.UTC)},
			}},
			want: "Fatura #10 - R$ 123,45 - venc. 21/08 - sent\n" +
				"Fatura #11 - R$ 123,45 - venc. 24/08 - sent\n" +
				"Fatura #12 - R$ 123,45 - venc. 28/08 - overdue",
		},
		{
			name:     "upcoming none within window",
			text:     "/upcoming",
			invoices: &fakeInvoices{listResult: []*model.Invoice{{Number: 13, DueDate: time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)}}},
			want:     "Nenhuma fatura vence nos próximos 7 dias.",
		},
		{
			name:    "clients one per line",
			text:    "/clients",
			clients: &fakeClients{names: []string{"Alice", "Bob Ltda"}},
			want:    "Alice\nBob Ltda",
		},
		{
			name: "clients empty",
			text: "/clients",
			want: "Nenhum cliente cadastrado.",
		},
		{
			name:     "unknown command yields help",
			text:     "/bogus arg",
			contains: []string{"/paid", "/status", "/upcoming", "/clients"},
		},
		{
			name:     "plain text yields help",
			text:     "oi, tudo bem?",
			contains: []string{"/paid", "/status", "/upcoming", "/clients"},
		},
		{
			name:     "empty message yields help",
			text:     "   ",
			contains: []string{"/paid", "/status", "/upcoming", "/clients"},
		},
		{
			name:     "status case-insensitive",
			text:     "/STATUS",
			invoices: &fakeInvoices{},
			want:     "Nenhuma fatura pendente.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot, inv, cl := newTestBot(tt.invoices, tt.clients)

			got := bot.Handle(ctx, tt.text)

			if tt.want != "" && got != tt.want {
				t.Fatalf("reply:\n%q\nwant:\n%q", got, tt.want)
			}
			for _, sub := range tt.contains {
				if !strings.Contains(got, sub) {
					t.Fatalf("reply %q missing %q", got, sub)
				}
			}
			if tt.check != nil {
				tt.check(t, got, inv, cl)
			}
		})
	}
}

// recordingAPI records SendMessage traffic and serves scripted update batches;
// once the script runs dry it returns empty results until ctx is canceled.
// All state is mutex-guarded because Run runs on its own goroutine.
type recordingAPI struct {
	mu      sync.Mutex
	sent    []sentMessage
	sendErr error

	script  [][]Update
	offsets []int64
	polls   int
}

type sentMessage struct {
	chatID string
	text   string
}

func (r *recordingAPI) SendMessage(_ context.Context, chatID, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, sentMessage{chatID: chatID, text: text})
	return r.sendErr
}

func (r *recordingAPI) GetUpdates(_ context.Context, offset int64) ([]Update, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offsets = append(r.offsets, offset)
	i := r.polls
	r.polls++
	if i < len(r.script) {
		return r.script[i], nil
	}
	return nil, nil
}

func (r *recordingAPI) failSends(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendErr = errors.New(msg)
}

func (r *recordingAPI) lastOffset() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.offsets) == 0 {
		return -1
	}
	return r.offsets[len(r.offsets)-1]
}

func (r *recordingAPI) sentSnapshot() []sentMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.sent)
}

func TestAdminBotProcessUpdateFiltersAndReplies(t *testing.T) {
	ctx := context.Background()
	api := &recordingAPI{}
	bot, _, _ := newTestBot(nil, nil)
	bot.api = api

	sent, err := bot.processUpdate(ctx, Update{UpdateID: 1})
	if err != nil || sent || len(api.sent) != 0 {
		t.Fatalf("non-message update: sent=%v err=%v sent=%v", sent, err, api.sent)
	}

	foreign := Update{UpdateID: 2, Message: &Message{Chat: Chat{ID: 999}, Text: "/clients"}}
	sent, err = bot.processUpdate(ctx, foreign)
	if err != nil || sent || len(api.sent) != 0 {
		t.Fatalf("foreign chat should be ignored silently: sent=%v err=%v api=%+v", sent, err, api.sent)
	}

	mediaOnly := Update{UpdateID: 5, Message: &Message{Chat: Chat{ID: 777}}}
	sent, err = bot.processUpdate(ctx, mediaOnly)
	if err != nil || sent || len(api.sent) != 0 {
		t.Fatalf("empty-text message (sticker/photo) should be ignored silently: sent=%v err=%v api=%+v", sent, err, api.sent)
	}

	own := Update{UpdateID: 3, Message: &Message{Chat: Chat{ID: 777}, Text: "/clients"}}
	sent, err = bot.processUpdate(ctx, own)
	if err != nil || !sent {
		t.Fatalf("admin chat update: sent=%v err=%v", sent, err)
	}
	if len(api.sent) != 1 || api.sent[0].chatID != "777" || api.sent[0].text != "Nenhum cliente cadastrado." {
		t.Fatalf("reply mismatch: %+v", api.sent)
	}

	api.failSends("send failed")
	failing := Update{UpdateID: 4, Message: &Message{Chat: Chat{ID: 777}, Text: "/clients"}}
	if _, err = bot.processUpdate(ctx, failing); err == nil || !strings.Contains(err.Error(), "send failed") {
		t.Fatalf("want send error propagated, got %v", err)
	}
}

func TestAdminBotRunPollsOffsetsAndStopsOnCancel(t *testing.T) {
	api := &recordingAPI{script: [][]Update{{
		{UpdateID: 10, Message: &Message{Chat: Chat{ID: 777}, Text: "/clients"}},
		{UpdateID: 11, Message: &Message{Chat: Chat{ID: 999}, Text: "/clients"}},
		{UpdateID: 12},
	}}}
	bot, _, _ := newTestBot(nil, nil)
	bot.api = api
	bot.interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- bot.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for api.lastOffset() != 13 {
		if time.Now().After(deadline) {
			t.Fatalf("offsets never advanced to 13 (last %d)", api.lastOffset())
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on canceled ctx")
	}

	sent := api.sentSnapshot()
	if len(sent) != 1 || sent[0].chatID != "777" || sent[0].text != "Nenhum cliente cadastrado." {
		t.Fatalf("replies = %+v, want exactly one reply to chat 777", sent)
	}
}
