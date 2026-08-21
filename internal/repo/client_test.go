package repo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
)

func openTestDB(t *testing.T) *repo.Repos {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.New(conn)
}

func strPtr(s string) *string { return &s }

func TestClientCRUD(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()

	c := &model.Client{Name: "Acme", Email: strPtr("a@acme.com"), Phone: strPtr("+5511999999999"), PIXKey: strPtr("a@acme.com")}
	created, err := r.Clients.Create(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatal("expected ID and timestamps set")
	}

	got, err := r.Clients.Get(ctx, created.ID)
	if err != nil || got.Name != "Acme" {
		t.Fatalf("get: %v %+v", err, got)
	}

	got.Name = "Acme SA"
	if err := r.Clients.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, _ := r.Clients.Get(ctx, created.ID)
	if got2.Name != "Acme SA" {
		t.Fatal("update failed")
	}

	list, err := r.Clients.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}

	if err := r.Clients.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Clients.Get(ctx, created.ID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestClientNotFound(t *testing.T) {
	r := openTestDB(t)
	if _, err := r.Clients.Get(context.Background(), "missing-id"); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
