package repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
)

func TestProductCRUD(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()

	p := &model.Product{Name: "Consulting", UnitPrice: 15000}
	created, err := r.Products.Create(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("expected ID and timestamps set")
	}

	got, err := r.Products.Get(ctx, created.ID)
	if err != nil || got.Name != "Consulting" || got.UnitPrice != 15000 {
		t.Fatalf("get: %v %+v", err, got)
	}

	got.Name = "Consulting Pro"
	got.Description = "Senior consulting"
	got.UnitPrice = 20000
	got.Currency = "USD"
	got.Active = false
	if err := r.Products.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, _ := r.Products.Get(ctx, created.ID)
	if got2.Name != "Consulting Pro" || got2.Description != "Senior consulting" ||
		got2.UnitPrice != 20000 || got2.Currency != "USD" || got2.Active {
		t.Fatalf("update failed: %+v", got2)
	}

	list, err := r.Products.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}

	if err := r.Products.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Products.Get(ctx, created.ID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestProductNotFound(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()
	if _, err := r.Products.Get(ctx, "missing-id"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := r.Products.Update(ctx, &model.Product{ID: "missing-id"}); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("update: want ErrNotFound, got %v", err)
	}
	if err := r.Products.Delete(ctx, "missing-id"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("delete: want ErrNotFound, got %v", err)
	}
}

func TestProductDefaults(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()

	created, err := r.Products.Create(ctx, &model.Product{Name: "No Explicit Defaults", UnitPrice: 500})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Products.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Currency != "BRL" {
		t.Fatalf("expected default currency BRL, got %q", got.Currency)
	}
	if !got.Active {
		t.Fatal("expected default active=true")
	}
	if got.Description != "" {
		t.Fatalf("expected empty description, got %q", got.Description)
	}
	list, err := r.Products.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
}

func TestProductListActive(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()

	for _, p := range []*model.Product{
		{Name: "Beta Service", UnitPrice: 100},
		{Name: "Alpha Product", UnitPrice: 200},
		{Name: "Gamma Retired", UnitPrice: 300},
	} {
		if _, err := r.Products.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	all, err := r.Products.List(ctx)
	if err != nil || len(all) != 3 {
		t.Fatalf("list: %v %d", err, len(all))
	}
	if all[0].Name != "Alpha Product" || all[1].Name != "Beta Service" || all[2].Name != "Gamma Retired" {
		t.Fatalf("expected name order, got %v", []string{all[0].Name, all[1].Name, all[2].Name})
	}

	gamma := findProduct(all, "Gamma Retired")
	gamma.Active = false
	if err := r.Products.Update(ctx, gamma); err != nil {
		t.Fatal(err)
	}

	activeList, err := r.Products.ListActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(activeList) != 2 {
		t.Fatalf("ListActive: want 2 products, got %d", len(activeList))
	}
	if activeList[0].Name != "Alpha Product" || activeList[1].Name != "Beta Service" {
		t.Fatalf("unexpected ListActive contents: %+v", activeList)
	}
}

func findProduct(list []*model.Product, name string) *model.Product {
	for _, p := range list {
		if p.Name == name {
			return p
		}
	}
	return nil
}
