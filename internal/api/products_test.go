package api_test

import (
	"testing"

	"github.com/ajesus37/heavens-invoicing/internal/model"
)

func TestProductsCRUDLifecycle(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	rec := do(t, h, "POST", "/api/products", map[string]any{
		"name":       "Consultoria",
		"unit_price": 150000,
	})
	assertStatus(t, rec, 201, "create product")
	created := decode[model.Product](t, rec)
	if created.ID == "" || created.Name != "Consultoria" || created.UnitPrice != 150000 || !created.Active || created.Currency != "BRL" {
		t.Fatalf("created = %+v", created)
	}

	rec = do(t, h, "GET", "/api/products", nil)
	assertStatus(t, rec, 200, "list products")
	list := decode[[]model.Product](t, rec)
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}

	rec = do(t, h, "PUT", "/api/products/"+created.ID, map[string]any{
		"name":       "Consultoria Plus",
		"unit_price": 200000,
		"active":     false,
	})
	assertStatus(t, rec, 200, "update product")
	updated := decode[model.Product](t, rec)
	if updated.Name != "Consultoria Plus" || updated.UnitPrice != 200000 || updated.Active {
		t.Fatalf("updated = %+v", updated)
	}

	rec = do(t, h, "DELETE", "/api/products/"+created.ID, nil)
	assertStatus(t, rec, 204, "delete product")
	rec = do(t, h, "GET", "/api/products/"+created.ID, nil)
	assertStatus(t, rec, 404, "get deleted product")
}

func TestProductsValidationAndErrors(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	tests := []struct {
		name   string
		method string
		path   string
		body   any
		want   int
	}{
		{"create without name", "POST", "/api/products", map[string]any{"unit_price": 100}, 400},
		{"create negative price", "POST", "/api/products", map[string]any{"name": "X", "unit_price": -1}, 400},
		{"get unknown", "GET", "/api/products/nope", nil, 404},
		{"update unknown", "PUT", "/api/products/nope", map[string]any{"name": "X"}, 404},
		{"delete unknown", "DELETE", "/api/products/nope", nil, 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, tt.method, tt.path, tt.body)
			assertStatus(t, rec, tt.want, tt.name)
		})
	}

	if rec := doRaw(t, h, "POST", "/api/products", "{oops"); rec.Code != 400 {
		t.Fatalf("malformed json: status = %d, want 400", rec.Code)
	}
}

func TestProductsUpdateRejectsNegativePrice(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	rec := do(t, h, "POST", "/api/products", map[string]any{"name": "X", "unit_price": 500})
	assertStatus(t, rec, 201, "seed product")
	id := decode[model.Product](t, rec).ID

	if rec := do(t, h, "PUT", "/api/products/"+id, map[string]any{"name": "X", "unit_price": -5}); rec.Code != 400 {
		t.Fatalf("negative update: status = %d, want 400", rec.Code)
	}

	got := decode[model.Product](t, do(t, h, "GET", "/api/products/"+id, nil))
	if got.UnitPrice != 500 {
		t.Fatalf("price changed despite rejected update: %d", got.UnitPrice)
	}
}

func TestProductsForgedIdentityIgnored(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	rec := do(t, h, "POST", "/api/products", map[string]any{
		"name":       "Forjado",
		"unit_price": 100,
		"id":         "attacker-chosen-id",
	})
	assertStatus(t, rec, 201, "create with forged id")
	created := decode[model.Product](t, rec)
	if created.ID == "attacker-chosen-id" || created.ID == "" {
		t.Fatalf("server must assign the id, got %q", created.ID)
	}
}

func TestProductUpdateEchoesStoredTimestamps(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	rec := do(t, h, "POST", "/api/products", map[string]any{"name": "X", "unit_price": 100})
	assertStatus(t, rec, 201, "create")
	created := decode[model.Product](t, rec)

	updated := decode[model.Product](t, do(t, h, "PUT", "/api/products/"+created.ID, map[string]any{
		"name":       "Y",
		"unit_price": 200,
	}))
	if updated.CreatedAt.IsZero() {
		t.Fatal("update response lost CreatedAt")
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("CreatedAt changed: %v -> %v", created.CreatedAt, updated.CreatedAt)
	}
	if updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatalf("UpdatedAt went backwards: %v -> %v", created.UpdatedAt, updated.UpdatedAt)
	}
}
