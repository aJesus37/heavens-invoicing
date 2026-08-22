package api_test

import (
	"testing"
	"time"
)

// recurringWire mirrors the recurring JSON contract.
type recurringWire struct {
	ID                string `json:"id"`
	ClientID          string `json:"client_id"`
	InvoiceTemplateID string `json:"invoice_template_id"`
	Frequency         string `json:"frequency"`
	DeliveryMethod    string `json:"delivery_method"`
	NextSendDate      string `json:"next_send_date"`
	Active            bool   `json:"active"`
}

func TestRecurringCreateListDelete(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	client := mustCreateClient(t, env.repos, "Acme")
	tpl := mustCreateInvoiceViaAPI(t, h, client.ID)

	rec := do(t, h, "POST", "/api/recurring", map[string]any{
		"client_id":           client.ID,
		"invoice_template_id": tpl.ID,
		"frequency":           "monthly",
		"delivery_method":     "email",
		"next_send_date":      "2026-09-01",
	})
	assertStatus(t, rec, 201, "create recurring")
	created := decode[recurringWire](t, rec)
	if created.ID == "" || created.Frequency != "monthly" || !created.Active ||
		created.NextSendDate != "2026-09-01" {
		t.Fatalf("created = %+v", created)
	}

	list := decode[[]recurringWire](t, do(t, h, "GET", "/api/recurring", nil))
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}

	assertStatus(t, do(t, h, "DELETE", "/api/recurring/"+created.ID, nil), 204, "delete recurring")
	if rec := do(t, h, "GET", "/api/recurring", nil); len(decode[[]recurringWire](t, rec)) != 0 {
		t.Fatal("expected empty list after delete")
	}
}

func TestRecurringNextSendDateDefaultsToToday(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler
	client := mustCreateClient(t, env.repos, "Acme")
	tpl := mustCreateInvoiceViaAPI(t, h, client.ID)

	rec := do(t, h, "POST", "/api/recurring", map[string]any{
		"client_id":           client.ID,
		"invoice_template_id": tpl.ID,
		"frequency":           "weekly",
		"delivery_method":     "all",
	})
	assertStatus(t, rec, 201, "create recurring without date")
	created := decode[recurringWire](t, rec)
	today := time.Now().Format("2006-01-02")
	if created.NextSendDate != today {
		t.Fatalf("next_send_date = %q, want today %q", created.NextSendDate, today)
	}
}

func TestRecurringValidation(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler
	client := mustCreateClient(t, env.repos, "Acme")
	tpl := mustCreateInvoiceViaAPI(t, h, client.ID)

	valid := map[string]any{
		"client_id":           client.ID,
		"invoice_template_id": tpl.ID,
		"frequency":           "monthly",
		"delivery_method":     "all",
	}

	mutate := func(overrides map[string]any) map[string]any {
		body := map[string]any{}
		for k, v := range valid {
			body[k] = v
		}
		for k, v := range overrides {
			body[k] = v
		}
		return body
	}

	tests := []struct {
		name string
		body map[string]any
	}{
		{"invalid frequency", mutate(map[string]any{"frequency": "daily"})},
		{"invalid delivery method", mutate(map[string]any{"delivery_method": "fax"})},
		{"unknown client", mutate(map[string]any{"client_id": "ghost"})},
		{"unknown template", mutate(map[string]any{"invoice_template_id": "ghost"})},
		{"missing template", mutate(map[string]any{"invoice_template_id": ""})},
		{"bad next_send_date", mutate(map[string]any{"next_send_date": "09/2026/01"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, "POST", "/api/recurring", tt.body)
			assertStatus(t, rec, 400, tt.name)
		})
	}

	assertStatus(t, do(t, h, "DELETE", "/api/recurring/nope", nil), 404, "delete unknown schedule")
}
