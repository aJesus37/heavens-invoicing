package repo_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
)

func TestInvoiceNumberConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// allow multiple connections via database/sql pool
	conn.SetMaxOpenConns(10)
	r := repo.New(conn)
	ctx := context.Background()

	// create a client to own invoices
	c, err := r.Clients.Create(ctx, &model.Client{Name: "Concurrent Client"})
	if err != nil {
		t.Fatal(err)
	}

	const workers = 5
	var wg sync.WaitGroup
	wg.Add(workers)
	start := make(chan struct{})
	errs := make([]error, workers)
	invoices := make([]*model.Invoice, workers)

	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			inv := &model.Invoice{
				ClientID:  c.ID,
				IssueDate: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
				DueDate:   time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC),
				Items: []*model.InvoiceItem{
					{Description: "Concurrent Item", UnitPrice: 100, Quantity: 1},
				},
			}
			errs[idx] = r.Invoices.Create(ctx, inv)
			invoices[idx] = inv
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d Create failed: %v", i, err)
		}
	}
	// assert distinct numbers and that we got 1..5 (order not guaranteed)
	seen := make(map[int64]bool)
	for i, inv := range invoices {
		if inv.Number == 0 {
			t.Fatalf("worker %d invoice number == 0", i)
		}
		if seen[inv.Number] {
			t.Fatalf("duplicate number %d (worker %d)", inv.Number, i)
		}
		seen[inv.Number] = true
	}
	if len(seen) != workers {
		t.Fatalf("expected %d distinct numbers, got %d: %v", workers, len(seen), seen)
	}
	// also verify via List that DB has 5 invoices with distinct numbers
	list, err := r.Invoices.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != workers {
		t.Fatalf("List = %d invoices, want %d", len(list), workers)
	}
	seenDB := make(map[int64]bool)
	for _, inv := range list {
		if seenDB[inv.Number] {
			t.Fatalf("DB duplicate number %d", inv.Number)
		}
		seenDB[inv.Number] = true
	}
}
