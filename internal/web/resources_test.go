package web_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
)

func strPtr(s string) *string { return &s }

// seedClient creates a client directly through the repo and returns its id.
func seedClient(t *testing.T, repos *repo.Repos, name string) string {
	t.Helper()
	c, err := repos.Clients.Create(context.Background(), &model.Client{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func TestProductCreateWithBRLDecimal(t *testing.T) {
	ts, _ := newTestEnv(t)

	resp, body := postForm(t, ts, "/products/new", url.Values{
		"name":        {"Consultoria"},
		"description": {"Hora de consultoria técnica"},
		"unit_price":  {"1.234,56"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create product: got %d want 303\n%s", resp.StatusCode, body)
	}

	status, body := get(t, ts, "/products")
	if status != http.StatusOK {
		t.Fatalf("list products: got %d want 200", status)
	}
	for _, marker := range []string{"Consultoria", "R$ 1.234,56"} {
		if !strings.Contains(body, marker) {
			t.Errorf("product list missing %q", marker)
		}
	}

	// Invalid price must re-render with an error banner.
	resp2, body2 := postForm(t, ts, "/products/new", url.Values{
		"name":       {"Ruim"},
		"unit_price": {"abc"},
	})
	if resp2.StatusCode != http.StatusBadRequest || !strings.Contains(body2, "Preço unitário") {
		t.Fatalf("invalid price: got %d want 400 with banner", resp2.StatusCode)
	}
}

func TestProductEditFlow(t *testing.T) {
	ts, repos := newTestEnv(t)

	if resp, _ := postForm(t, ts, "/products/new", url.Values{"name": {"Licença"}, "unit_price": {"100,00"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup create failed: got %d want 303", resp.StatusCode)
	}

	list, err := repos.Products.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 product, got %d (%v)", len(list), err)
	}
	id := list[0].ID

	status, body := get(t, ts, "/products/"+id+"/edit")
	if status != http.StatusOK || !strings.Contains(body, "Editar produto") || !strings.Contains(body, `value="100,00"`) {
		t.Fatalf("edit form missing prefilled price (status=%d)", status)
	}

	respUp, _ := postForm(t, ts, "/products/"+id+"/edit", url.Values{
		"name":       {"Licença Anual"},
		"unit_price": {"1200"},
		"active":     {"on"},
	})
	if respUp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update: got %d want 303", respUp.StatusCode)
	}
	_, body = get(t, ts, "/products")
	if !strings.Contains(body, "Licença Anual") || !strings.Contains(body, "R$ 1.200,00") {
		t.Fatal("update did not persist")
	}
}

func TestInvoiceCreateFlowEndToEnd(t *testing.T) {
	ts, repos := newTestEnv(t)
	clientID := seedClient(t, repos, "Cliente Fatura")

	form := url.Values{
		"client_id":    {clientID},
		"issue_date":   {"2026-08-01"},
		"due_date":     {"2026-08-15"},
		"notes":        {"Obrigado pela preferência"},
		"item_desc_0":  {"Desenvolvimento"},
		"item_qty_0":   {"2"},
		"item_price_0": {"1500,50"},
		"item_desc_1":  {"Hospedagem"},
		"item_qty_1":   {"1"},
		"item_price_1": {"99,9"},
	}
	resp, body := postForm(t, ts, "/invoices/new", form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create invoice: got %d want 303\n%s", resp.StatusCode, body)
	}

	detailPath := resp.Header.Get("Location")
	status, detail := get(t, ts, detailPath)
	if status != http.StatusOK {
		t.Fatalf("detail: got %d want 200", status)
	}
	for _, marker := range []string{
		"Fatura #000001",
		"Cliente Fatura",
		"01/08/2026",
		"15/08/2026",
		"R$ 3.100,90", // 2*1500,50 + 99,90
		"rascunho",
	} {
		if !strings.Contains(detail, marker) {
			t.Errorf("invoice detail missing %q", marker)
		}
	}

	// Due date before issue date must fail loudly.
	bad := url.Values{
		"client_id":    {clientID},
		"issue_date":   {"2026-09-10"},
		"due_date":     {"2026-09-01"},
		"item_desc_0":  {"X"},
		"item_price_0": {"10,00"},
	}
	respBad, _ := postForm(t, ts, "/invoices/new", bad)
	if respBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad dates: got %d want 400", respBad.StatusCode)
	}

	// Status tab filter shows the draft invoice; paid tab stays empty.
	_, allDrafts := get(t, ts, "/invoices?status=draft")
	if !strings.Contains(allDrafts, "#000001") {
		t.Error("draft filter should contain the new invoice")
	}
	_, allPaid := get(t, ts, "/invoices?status=paid")
	if strings.Contains(allPaid, "#000001") {
		t.Error("paid filter should not contain a draft invoice")
	}
}

func TestInvoiceMarkPaidHidesActions(t *testing.T) {
	ts, repos := newTestEnv(t)
	clientID := seedClient(t, repos, "Pagador")

	ctx := context.Background()
	inv := &model.Invoice{
		ClientID:  clientID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 8, 20),
		Items: []*model.InvoiceItem{{
			Description: "Serviço", Quantity: 1, UnitPrice: 5000,
		}},
	}
	if err := repos.Invoices.Create(ctx, inv); err != nil {
		t.Fatal(err)
	}

	// A draft offers both actions.
	_, body := get(t, ts, "/invoices/"+inv.ID)
	for _, marker := range []string{"Marcar paga", "Enviar fatura"} {
		if !strings.Contains(body, marker) {
			t.Errorf("draft detail missing action %q", marker)
		}
	}

	resp, _ := postForm(t, ts, "/invoices/"+inv.ID+"/mark-paid", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("mark paid: got %d want 303", resp.StatusCode)
	}
	got, err := repos.Invoices.Get(ctx, inv.ID)
	if err != nil || got.Status != "paid" {
		t.Fatalf("mark paid did not persist (status=%q err=%v)", got.Status, err)
	}

	// Paid invoices must not offer either action.
	_, body = get(t, ts, "/invoices/"+inv.ID)
	if strings.Contains(body, "Marcar paga") || strings.Contains(body, "Enviar fatura") {
		t.Error("paid invoice still offers mark-paid/send actions")
	}
	if !strings.Contains(body, "paga") {
		t.Error("paid invoice detail missing status badge")
	}

	// Resending a paid invoice is rejected with a clear reason instead of
	// downgrading it to "sent".
	respSend, bodySend := postForm(t, ts, "/invoices/"+inv.ID+"/send", url.Values{"method": {"email"}})
	if respSend.StatusCode != http.StatusConflict {
		t.Fatalf("send paid invoice: got %d want 409", respSend.StatusCode)
	}
	if !strings.Contains(bodySend, "já está paga") {
		t.Errorf("send paid invoice response missing reason: %s", bodySend)
	}
	got, err = repos.Invoices.Get(ctx, inv.ID)
	if err != nil || got.Status != "paid" {
		t.Fatalf("paid status was changed by resend (status=%q err=%v)", got.Status, err)
	}
}

func TestInvoiceCancelledKeepsSendOnly(t *testing.T) {
	ts, repos := newTestEnv(t)
	clientID := seedClient(t, repos, "Devedor")

	inv := &model.Invoice{
		ClientID:  clientID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 8, 20),
		Items:     []*model.InvoiceItem{{Description: "Serviço", Quantity: 1, UnitPrice: 1000}},
	}
	if err := repos.Invoices.Create(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	if err := repos.Invoices.UpdateStatus(context.Background(), inv.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}

	_, body := get(t, ts, "/invoices/"+inv.ID)
	if strings.Contains(body, "Marcar paga") || strings.Contains(body, "Mark paid") {
		t.Error("cancelled invoice still offers mark-paid")
	}
	if strings.Contains(body, "Enviar fatura") || strings.Contains(body, "Send invoice") {
		t.Error("cancelled invoice still offers send action")
	}
	if !strings.Contains(body, "cancelada") && !strings.Contains(body, "Cancelled") && !strings.Contains(body, "cancelled") {
		t.Error("cancelled invoice should show disabled message")
	}

	// Sending a cancelled invoice must be rejected with 409.
	resp, bodySend := postForm(t, ts, "/invoices/"+inv.ID+"/send", url.Values{"method": {"email"}})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("send cancelled invoice: got %d want 409", resp.StatusCode)
	}
	if !strings.Contains(bodySend, "cancelada") && !strings.Contains(strings.ToLower(bodySend), "cancelled") {
		t.Errorf("send cancelled response missing reason: %s", bodySend)
	}
	// Ensure status did not flip to sent.
	got, err := repos.Invoices.Get(context.Background(), inv.ID)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("cancelled status was changed by send (status=%q err=%v)", got.Status, err)
	}
}

func TestInvoiceSendFragmentOnDraft(t *testing.T) {
	ts, repos := newTestEnv(t)

	// The router only attempts channels the client has contacts for, so
	// seed all three to exercise every result row.
	c, err := repos.Clients.Create(context.Background(), &model.Client{
		Name:           "Pagador",
		Email:          strPtr("pagador@x.com"),
		Phone:          strPtr("+5511988887777"),
		TelegramChatID: strPtr("4242"),
	})
	if err != nil {
		t.Fatal(err)
	}

	inv := &model.Invoice{
		ClientID:  c.ID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 8, 20),
		Items:     []*model.InvoiceItem{{Description: "Serviço", Quantity: 1, UnitPrice: 5000}},
	}
	if err := repos.Invoices.Create(context.Background(), inv); err != nil {
		t.Fatal(err)
	}

	// Send endpoint returns an HTML fragment; with every channel unconfigured
	// the router reports per-channel failures rather than crashing.
	respSend, bodySend := postForm(t, ts, "/invoices/"+inv.ID+"/send", url.Values{"method": {"all"}})
	if respSend.StatusCode != http.StatusOK {
		t.Fatalf("send fragment: got %d want 200", respSend.StatusCode)
	}
	for _, marker := range []string{"E-mail", "Telegram", "WhatsApp"} {
		if !strings.Contains(bodySend, marker) {
			t.Errorf("send fragment missing channel %q", marker)
		}
	}

	// Unknown method is rejected.
	respBadMethod, _ := postForm(t, ts, "/invoices/"+inv.ID+"/send", url.Values{"method": {"fax"}})
	if respBadMethod.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid method: got %d want 400", respBadMethod.StatusCode)
	}
}

func TestRecurringCreateFlow(t *testing.T) {
	ts, repos := newTestEnv(t)
	clientID := seedClient(t, repos, "Assinante")

	ctx := context.Background()
	tpl := &model.Invoice{
		ClientID:  clientID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 8, 10),
		Items:     []*model.InvoiceItem{{Description: "Mensalidade", Quantity: 1, UnitPrice: 9900}},
	}
	if err := repos.Invoices.Create(ctx, tpl); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"client_id":           {clientID},
		"invoice_template_id": {tpl.ID},
		"frequency":           {"monthly"},
		"delivery_method":     {"email"},
		"next_send_date":      {"2026-08-25"},
	}
	resp, body := postForm(t, ts, "/recurring/new", form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create recurring: got %d want 303\n%s", resp.StatusCode, body)
	}

	status, listBody := get(t, ts, "/recurring")
	if status != http.StatusOK {
		t.Fatalf("list: got %d want 200", status)
	}
	for _, marker := range []string{"Assinante", "#000001", "Mensal", "25/08/2026", "E-mail"} {
		if !strings.Contains(listBody, marker) {
			t.Errorf("recurring list missing %q", marker)
		}
	}

	schedules, err := repos.Recurring.List(ctx)
	if err != nil || len(schedules) != 1 || !schedules[0].Active {
		t.Fatalf("schedule not persisted (%v)", err)
	}

	// Delete through the page keeps the UI honest.
	delResp, _ := postForm(t, ts, "/recurring/"+schedules[0].ID+"/delete", url.Values{})
	if delResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete: got %d want 303", delResp.StatusCode)
	}
	schedules, _ = repos.Recurring.List(ctx)
	if len(schedules) != 0 {
		t.Fatal("delete did not persist")
	}
}
