package schedule

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
)

// allFailingSender fails every invoice delivery so the scheduler must roll
// back each clone it attempts.
type allFailingSender struct{}

func (allFailingSender) SendInvoice(_ context.Context, _ model.Client, _ model.Invoice, _ []byte, _ string) ([]deliver.ChannelResult, error) {
	return []deliver.ChannelResult{{Channel: "email", Err: errors.New("down")}}, nil
}

func (allFailingSender) SendReminder(_ context.Context, _ model.Client, _ model.Invoice, _ string) ([]deliver.ChannelResult, error) {
	return []deliver.ChannelResult{{Channel: "email"}}, nil
}

func sp(s string) *string { return &s }

// TestNoOrphanDraftsAcrossDays proves the fix for the orphan-draft bug: a
// permanently-failing schedule must not accumulate one draft per day. Across
// several daily ticks only the template draft (seeded up front) survives;
// every clone a failed fire created is rolled back.
func TestNoOrphanDraftsAcrossDays(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	repos := repo.New(conn)
	ctx := context.Background()

	client, err := repos.Clients.Create(ctx, &model.Client{ID: "c1", Name: "Acme", Email: sp("a@b.com")})
	if err != nil {
		t.Fatal(err)
	}
	tpl := &model.Invoice{
		ClientID:  client.ID,
		IssueDate: day("2026-09-01"),
		DueDate:   day("2026-10-01"),
		Items:     []*model.InvoiceItem{{Description: "Serviço", UnitPrice: 1000, Quantity: 1}},
	}
	if err := repos.Invoices.Create(ctx, tpl); err != nil {
		t.Fatal(err)
	}
	sched := &model.RecurringSchedule{
		ClientID:          client.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "monthly",
		NextSendDate:      day("2026-09-01"),
		DeliveryMethod:    "email",
	}
	if err := repos.Recurring.Create(ctx, sched); err != nil {
		t.Fatal(err)
	}

	sender := allFailingSender{}
	notifier := &fakeNotifier{}
	var clock time.Time
	base := day("2026-09-01")
	s := New(repos.Recurring, repos.Invoices, repos.Clients, sender, notifier,
		func() i18n.Lang { return i18n.PtBR }, pdf.SenderInfo{}, func() time.Time { return clock })

	const days = 5
	for i := 0; i < days; i++ {
		clock = base.AddDate(0, 0, i)
		// Every fire fails delivery; Tick surfaces that as an error, which is
		// expected here. We only care that no draft accumulates, so ignore
		// the per-fire failure and assert on the invoice store afterwards.
		_ = s.Tick(ctx)
	}

	drafts, err := repos.Invoices.ListByStatus(ctx, "draft")
	if err != nil {
		t.Fatal(err)
	}
	// Exactly one draft (the template) — no accumulation across the 5 days.
	if len(drafts) != 1 {
		t.Fatalf("draft invoices after %d failing ticks = %d, want exactly 1 (the template)", days, len(drafts))
	}
}
