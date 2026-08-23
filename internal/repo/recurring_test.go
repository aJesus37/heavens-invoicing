package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ajesus37/heavens-invoicing/internal/model"
	"github.com/ajesus37/heavens-invoicing/internal/repo"
)

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func recurringSeed(t *testing.T, r *repo.Repos) (*model.Client, *model.Invoice) {
	t.Helper()
	ctx := context.Background()
	client, err := r.Clients.Create(ctx, &model.Client{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	tpl := &model.Invoice{
		ClientID:  client.ID,
		IssueDate: mustDate("2026-08-01"),
		DueDate:   mustDate("2026-09-01"),
		Items: []*model.InvoiceItem{
			{Description: "Serviço", UnitPrice: 10000, Quantity: 1},
		},
	}
	if err := r.Invoices.Create(ctx, tpl); err != nil {
		t.Fatal(err)
	}
	return client, tpl
}

func TestRecurringCRUD(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client, tpl := recurringSeed(t, r)

	s := &model.RecurringSchedule{
		ClientID:          client.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "monthly",
		NextSendDate:      mustDate("2026-09-01"),
		DeliveryMethod:    "email",
	}
	if err := r.Recurring.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	if s.ID == "" || !s.Active {
		t.Fatalf("expected ID and active default, got id=%q active=%v", s.ID, s.Active)
	}

	got, err := r.Recurring.Get(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Frequency != "monthly" || got.DeliveryMethod != "email" || !got.NextSendDate.Equal(mustDate("2026-09-01")) {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.Active != true {
		t.Fatal("active should round-trip as true")
	}

	got.Frequency = "weekly"
	got.LastSentDate = func() *time.Time { d := mustDate("2026-09-01"); return &d }()
	if err := r.Recurring.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, _ := r.Recurring.Get(ctx, s.ID)
	if got2.Frequency != "weekly" || got2.LastSentDate == nil || !got2.LastSentDate.Equal(mustDate("2026-09-01")) {
		t.Fatalf("update failed: %+v", got2)
	}

	list, err := r.Recurring.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}

	if err := r.Recurring.Delete(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Recurring.Get(ctx, s.ID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestRecurringValidation(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client, tpl := recurringSeed(t, r)

	base := model.RecurringSchedule{
		ClientID:          client.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "monthly",
		DeliveryMethod:    "all",
		NextSendDate:      mustDate("2026-09-01"),
	}

	badFreq := base
	badFreq.Frequency = "daily"
	if err := r.Recurring.Create(ctx, &badFreq); err == nil {
		t.Fatal("want error for invalid frequency")
	}

	badMethod := base
	badMethod.DeliveryMethod = "carrier-pigeon"
	if err := r.Recurring.Create(ctx, &badMethod); err == nil {
		t.Fatal("want error for invalid delivery method")
	}

	if err := r.Recurring.Create(ctx, &base); err != nil {
		t.Fatal(err)
	}
	base.Frequency = "daily"
	if err := r.Recurring.Update(ctx, &base); err == nil {
		t.Fatal("want error updating to invalid frequency")
	}
}

func TestRecurringNotFound(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	if _, err := r.Recurring.Get(ctx, "missing"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := r.Recurring.Delete(ctx, "missing"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("delete want ErrNotFound, got %v", err)
	}
}

func TestRecurringActiveToggles(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client, tpl := recurringSeed(t, r)

	s := &model.RecurringSchedule{
		ClientID:          client.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "monthly",
		NextSendDate:      mustDate("2026-09-01"),
		DeliveryMethod:    "email",
	}
	if err := r.Recurring.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	if !s.Active {
		t.Fatal("new schedule should default to active")
	}

	// Pause and persist.
	s.Active = false
	if err := r.Recurring.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	got, err := r.Recurring.Get(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Active {
		t.Fatal("update did not persist paused state")
	}

	// Resume and persist.
	got.Active = true
	if err := r.Recurring.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, _ := r.Recurring.Get(ctx, s.ID)
	if !got2.Active {
		t.Fatal("update did not persist resumed state")
	}
}

func TestRecurringListOrderedByNextSendDate(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client, tpl := recurringSeed(t, r)

	for i, day := range []string{"2026-10-01", "2026-09-01", "2026-12-01"} {
		s := &model.RecurringSchedule{
			ClientID:          client.ID,
			InvoiceTemplateID: tpl.ID,
			Frequency:         "monthly",
			NextSendDate:      mustDate(day),
			DeliveryMethod:    "all",
		}
		s.ID = ""
		if err := r.Recurring.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
		_ = i
	}
	list, err := r.Recurring.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-09-01", "2026-10-01", "2026-12-01"}
	for i, w := range want {
		if list[i].NextSendDate.Format("2006-01-02") != w {
			t.Fatalf("position %d = %s, want %s", i, list[i].NextSendDate.Format("2006-01-02"), w)
		}
	}
}

func TestRecurringListActiveFiltersDeactivated(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client, tpl := recurringSeed(t, r)

	active := &model.RecurringSchedule{
		ClientID:          client.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "monthly",
		NextSendDate:      mustDate("2026-09-01"),
		DeliveryMethod:    "all",
	}
	if err := r.Recurring.Create(ctx, active); err != nil {
		t.Fatal(err)
	}
	inactive := &model.RecurringSchedule{
		ClientID:          client.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "weekly",
		NextSendDate:      mustDate("2026-08-01"), // earlier on purpose
		DeliveryMethod:    "email",
	}
	if err := r.Recurring.Create(ctx, inactive); err != nil {
		t.Fatal(err)
	}
	inactive.Active = false
	if err := r.Recurring.Update(ctx, inactive); err != nil {
		t.Fatal(err)
	}

	list, err := r.Recurring.ListActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != active.ID || !list[0].Active {
		t.Fatalf("list active = %+v, want only the active schedule", list)
	}
}
