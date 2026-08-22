package api_test

import (
	"testing"

	"github.com/jesus/invoice-app/internal/model"
)

func TestClientsCRUDLifecycle(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	// create
	rec := do(t, h, "POST", "/api/clients", map[string]any{
		"name":             "Acme",
		"email":            "a@acme.com",
		"phone":            "+5511999999999",
		"telegram_chat_id": "424242",
	})
	assertStatus(t, rec, 201, "create client")
	created := decode[model.Client](t, rec)
	if created.ID == "" || created.Name != "Acme" || created.Email == nil || *created.Email != "a@acme.com" {
		t.Fatalf("created = %+v", created)
	}

	// list
	rec = do(t, h, "GET", "/api/clients", nil)
	assertStatus(t, rec, 200, "list clients")
	list := decode[[]model.Client](t, rec)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}

	// get one
	rec = do(t, h, "GET", "/api/clients/"+created.ID, nil)
	assertStatus(t, rec, 200, "get client")

	// update
	rec = do(t, h, "PUT", "/api/clients/"+created.ID, map[string]any{"name": "Acme SA"})
	assertStatus(t, rec, 200, "update client")
	updated := decode[model.Client](t, rec)
	if updated.Name != "Acme SA" {
		t.Fatalf("updated = %+v", updated)
	}

	// delete
	rec = do(t, h, "DELETE", "/api/clients/"+created.ID, nil)
	assertStatus(t, rec, 204, "delete client")
	rec = do(t, h, "GET", "/api/clients/"+created.ID, nil)
	assertStatus(t, rec, 404, "get deleted client")
}

func TestClientsValidationAndErrors(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	tests := []struct {
		name   string
		method string
		path   string
		body   any
		want   int
	}{
		{"create without name", "POST", "/api/clients", map[string]any{"email": "x@y.z"}, 400},
		{"create blank name", "POST", "/api/clients", map[string]any{"name": "   "}, 400},
		{"update missing name", "PUT", "/api/clients/some-id", map[string]any{}, 400},
		{"get unknown", "GET", "/api/clients/nope", nil, 404},
		{"update unknown", "PUT", "/api/clients/nope", map[string]any{"name": "X"}, 404},
		{"delete unknown", "DELETE", "/api/clients/nope", nil, 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, tt.method, tt.path, tt.body)
			assertStatus(t, rec, tt.want, tt.name)
		})
	}

	if rec := doRaw(t, h, "POST", "/api/clients", "{not json"); rec.Code != 400 {
		t.Fatalf("malformed json: status = %d, want 400", rec.Code)
	}
}

func TestClientsForgedIdentityIgnored(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	rec := do(t, h, "POST", "/api/clients", map[string]any{
		"name": "Forjado",
		"id":   "attacker-chosen-id",
	})
	assertStatus(t, rec, 201, "create with forged id")
	created := decode[model.Client](t, rec)
	if created.ID == "attacker-chosen-id" || created.ID == "" {
		t.Fatalf("server must assign the id, got %q", created.ID)
	}
}

func TestClientUpdateEchoesStoredTimestamps(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	rec := do(t, h, "POST", "/api/clients", map[string]any{"name": "Acme"})
	assertStatus(t, rec, 201, "create")
	created := decode[model.Client](t, rec)

	updated := decode[model.Client](t, do(t, h, "PUT", "/api/clients/"+created.ID, map[string]any{"name": "Acme SA"}))
	if updated.CreatedAt.IsZero() || !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("update response CreatedAt = %v, want stored %v", updated.CreatedAt, created.CreatedAt)
	}
	if updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatalf("UpdatedAt went backwards: %v -> %v", created.UpdatedAt, updated.UpdatedAt)
	}
}
