package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
)

func seedClient(t *testing.T, r *repo.Repos) *model.Client {
	t.Helper()
	c, err := r.Clients.Create(context.Background(), &model.Client{Name: "Invoice Test Client"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newDraftInvoice(clientID string, items ...*model.InvoiceItem) *model.Invoice {
	return &model.Invoice{
		ClientID:  clientID,
		IssueDate: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
		DueDate:   time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC),
		Items:     items,
	}
}

func TestInvoiceCreateAssignsNumberAndTotals(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client := seedClient(t, r)

	inv := newDraftInvoice(client.ID,
		&model.InvoiceItem{Description: "Consulting", UnitPrice: 100, Quantity: 2},
		&model.InvoiceItem{Description: "Support", UnitPrice: 250, Quantity: 1},
	)
	if err := r.Invoices.Create(ctx, inv); err != nil {
		t.Fatal(err)
	}
	if inv.Number != 1 {
		t.Fatalf("first invoice number = %d, want 1", inv.Number)
	}
	if inv.Subtotal != 450 || inv.Total != 450 {
		t.Fatalf("totals = %d/%d, want 450/450", inv.Subtotal, inv.Total)
	}
	if inv.Items[0].Total != 200 || inv.Items[1].Total != 250 {
		t.Fatalf("item totals = %d/%d, want 200/250", inv.Items[0].Total, inv.Items[1].Total)
	}
	if inv.ID == "" || inv.CreatedAt.IsZero() || inv.UpdatedAt.IsZero() {
		t.Fatal("expected ID and timestamps set")
	}

	second := newDraftInvoice(client.ID,
		&model.InvoiceItem{Description: "Follow-up", UnitPrice: 500, Quantity: 1},
	)
	if err := r.Invoices.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	if second.Number != 2 {
		t.Fatalf("second invoice number = %d, want 2", second.Number)
	}
}

func TestInvoiceCreateRejectsInvalid(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client := seedClient(t, r)

	cases := []*model.Invoice{
		newDraftInvoice(client.ID), // no items
		newDraftInvoice(client.ID, &model.InvoiceItem{Description: "Zero qty", UnitPrice: 100, Quantity: 0}),
		newDraftInvoice(client.ID, &model.InvoiceItem{Description: "", UnitPrice: 100, Quantity: 1}),
		func() *model.Invoice {
			inv := newDraftInvoice(client.ID, &model.InvoiceItem{Description: "X", UnitPrice: 100, Quantity: 1})
			inv.Status = "shipped"
			return inv
		}(),
	}
	for i, inv := range cases {
		if err := r.Invoices.Create(ctx, inv); err == nil {
			t.Fatalf("case %d: expected error, got nil", i)
		}
	}

	list, err := r.Invoices.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected nothing persisted after failed creates, got %d invoices", len(list))
	}
}

func TestInvoiceGetLoadsItems(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client := seedClient(t, r)

	product := &model.Product{Name: "Widget", UnitPrice: 300}
	if _, err := r.Products.Create(ctx, product); err != nil {
		t.Fatal(err)
	}

	inv := newDraftInvoice(client.ID,
		&model.InvoiceItem{Description: "First", UnitPrice: 100, Quantity: 2},
		&model.InvoiceItem{ProductID: strPtr(product.ID), Description: "Second", UnitPrice: 250, Quantity: 3},
	)
	if err := r.Invoices.Create(ctx, inv); err != nil {
		t.Fatal(err)
	}

	got, err := r.Invoices.Get(ctx, inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClientID != client.ID || got.Status != "draft" {
		t.Fatalf("unexpected invoice header: %+v", got)
	}
	if !got.IssueDate.Equal(inv.IssueDate) || !got.DueDate.Equal(inv.DueDate) {
		t.Fatalf("dates round-trip failed: %v/%v", got.IssueDate, got.DueDate)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(got.Items))
	}
	first, second := got.Items[0], got.Items[1]
	if first.Description != "First" || first.UnitPrice != 100 || first.Quantity != 2 || first.Total != 200 {
		t.Fatalf("item 1 mismatch: %+v", first)
	}
	if second.Description != "Second" || second.UnitPrice != 250 || second.Quantity != 3 || second.Total != 750 {
		t.Fatalf("item 2 mismatch: %+v", second)
	}
	if second.ProductID == nil || *second.ProductID != product.ID {
		t.Fatalf("item 2 product_id = %v, want %s", second.ProductID, product.ID)
	}
	if first.ProductID != nil {
		t.Fatalf("item 1 product_id = %v, want nil", first.ProductID)
	}

	if _, err := r.Invoices.Get(ctx, "missing-id"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestInvoiceUpdateStatus(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client := seedClient(t, r)

	inv := newDraftInvoice(client.ID, &model.InvoiceItem{Description: "Work", UnitPrice: 100, Quantity: 1})
	if err := r.Invoices.Create(ctx, inv); err != nil {
		t.Fatal(err)
	}

	if err := r.Invoices.UpdateStatus(ctx, inv.ID, "sent"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Invoices.Get(ctx, inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "sent" {
		t.Fatalf("status = %q, want sent", got.Status)
	}

	if err := r.Invoices.UpdateStatus(ctx, inv.ID, "bogus"); err == nil {
		t.Fatal("expected error for invalid status")
	}
	if err := r.Invoices.UpdateStatus(ctx, "missing-id", "paid"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestInvoiceCloneFromTemplate(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client := seedClient(t, r)

	tpl := newDraftInvoice(client.ID,
		&model.InvoiceItem{Description: "Monthly retainer", UnitPrice: 15000, Quantity: 1},
		&model.InvoiceItem{Description: "Hosting", UnitPrice: 5000, Quantity: 2},
	)
	tpl.Notes = "Recurring monthly work"
	tpl.PIXKey = strPtr("pix@example.com")
	if err := r.Invoices.Create(ctx, tpl); err != nil {
		t.Fatal(err)
	}

	issue := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	clone, err := r.Invoices.CloneFromTemplate(ctx, tpl.ID, issue, due)
	if err != nil {
		t.Fatal(err)
	}

	if clone.ID == "" || clone.ID == tpl.ID {
		t.Fatalf("clone ID = %q, want new ID", clone.ID)
	}
	if clone.Number != tpl.Number+1 {
		t.Fatalf("clone number = %d, want %d", clone.Number, tpl.Number+1)
	}
	if clone.Status != "draft" {
		t.Fatalf("clone status = %q, want draft", clone.Status)
	}
	if !clone.IssueDate.Equal(issue) || !clone.DueDate.Equal(due) {
		t.Fatalf("clone dates = %v/%v, want %v/%v", clone.IssueDate, clone.DueDate, issue, due)
	}
	if clone.Notes != tpl.Notes {
		t.Fatalf("clone notes = %q, want %q", clone.Notes, tpl.Notes)
	}
	if clone.PIXKey == nil || *clone.PIXKey != *tpl.PIXKey {
		t.Fatalf("clone pix_key = %v, want %v", clone.PIXKey, *tpl.PIXKey)
	}
	if clone.Subtotal != 25000 || clone.Total != 25000 {
		t.Fatalf("clone totals = %d/%d, want 25000/25000", clone.Subtotal, clone.Total)
	}
	if len(clone.Items) != 2 {
		t.Fatalf("clone items = %d, want 2", len(clone.Items))
	}
	for i, item := range clone.Items {
		src := tpl.Items[i]
		if item.Description != src.Description || item.UnitPrice != src.UnitPrice ||
			item.Quantity != src.Quantity || item.Total != src.Total {
			t.Fatalf("clone item %d mismatch: %+v vs %+v", i, item, src)
		}
		if item.InvoiceID != clone.ID {
			t.Fatalf("clone item %d invoice_id = %q, want %q", i, item.InvoiceID, clone.ID)
		}
	}

	// Template itself unchanged.
	tplGot, err := r.Invoices.Get(ctx, tpl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tplGot.Number != tpl.Number || tplGot.Status != tpl.Status {
		t.Fatalf("template changed: number=%d status=%q", tplGot.Number, tplGot.Status)
	}
	if len(tplGot.Items) != 2 {
		t.Fatalf("template items = %d, want 2", len(tplGot.Items))
	}
}

func TestInvoiceListByStatus(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	client := seedClient(t, r)

	for range 3 {
		inv := newDraftInvoice(client.ID, &model.InvoiceItem{Description: "Line", UnitPrice: 100, Quantity: 1})
		if err := r.Invoices.Create(ctx, inv); err != nil {
			t.Fatal(err)
		}
	}
	all, err := r.Invoices.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("list = %d invoices, want 3", len(all))
	}
	// Ordered by number DESC.
	if all[0].Number != 3 || all[1].Number != 2 || all[2].Number != 1 {
		t.Fatalf("list order = %v, want [3 2 1]", []int64{all[0].Number, all[1].Number, all[2].Number})
	}
	for _, inv := range all {
		if inv.Items != nil {
			t.Fatal("List should not load items")
		}
	}

	if err := r.Invoices.UpdateStatus(ctx, all[0].ID, "sent"); err != nil {
		t.Fatal(err)
	}
	if err := r.Invoices.UpdateStatus(ctx, all[1].ID, "sent"); err != nil {
		t.Fatal(err)
	}
	if err := r.Invoices.UpdateStatus(ctx, all[2].ID, "paid"); err != nil {
		t.Fatal(err)
	}

	sent, err := r.Invoices.ListByStatus(ctx, "sent")
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 {
		t.Fatalf("ListByStatus(sent) = %d invoices, want 2", len(sent))
	}
	paid, err := r.Invoices.ListByStatus(ctx, "paid")
	if err != nil {
		t.Fatal(err)
	}
	if len(paid) != 1 || paid[0].ID != all[2].ID {
		t.Fatalf("ListByStatus(paid) unexpected: %+v", paid)
	}
	drafts, err := r.Invoices.ListByStatus(ctx, "draft")
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 0 {
		t.Fatalf("ListByStatus(draft) = %d invoices, want 0", len(drafts))
	}
	mixed, err := r.Invoices.ListByStatus(ctx, "sent", "cancelled")
	if err != nil {
		t.Fatal(err)
	}
	if len(mixed) != 2 {
		t.Fatalf("ListByStatus(sent,cancelled) = %d invoices, want 2", len(mixed))
	}
}
