package repo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ajesus37/heavens-invoicing/internal/db"
	"github.com/ajesus37/heavens-invoicing/internal/model"
	"github.com/ajesus37/heavens-invoicing/internal/repo"
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

	c := &model.Client{Name: "Acme", Email: strPtr("a@acme.com"), Phone: strPtr("+5511999999999"), PIXKey: strPtr("a@acme.com"), Language: "en"}
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
	if got.Language != "en" {
		t.Fatalf("language round-trip: got %q, want en", got.Language)
	}

	got.Name = "Acme SA"
	got.Language = "pt-BR"
	if err := r.Clients.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, _ := r.Clients.Get(ctx, created.ID)
	if got2.Name != "Acme SA" {
		t.Fatal("update failed")
	}
	if got2.Language != "pt-BR" {
		t.Fatalf("language update failed: got %q, want pt-BR", got2.Language)
	}

	list, err := r.Clients.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	if list[0].Language != "pt-BR" {
		t.Fatalf("list language: got %q, want pt-BR", list[0].Language)
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

func TestClientNullables(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()

	created, err := r.Clients.Create(ctx, &model.Client{Name: "No Optionals"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Clients.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*string{got.Email, got.Phone, got.TelegramChatID, got.PIXKey} {
		if p != nil {
			t.Fatalf("expected nil optional after round-trip, got %q", *p)
		}
	}
	list, err := r.Clients.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	c := list[0]
	if c.Email != nil || c.Phone != nil || c.TelegramChatID != nil || c.PIXKey != nil {
		t.Fatal("expected nil optionals in List result")
	}
}
