package web_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jesus/invoice-app/internal/model"
)

func TestOldPortugueseRoutesAreGone(t *testing.T) {
	ts, _ := newTestEnv(t)

	exactGetPaths := []string{
		"/clientes",
		"/clientes/novo",
		"/produtos",
		"/produtos/novo",
		"/faturas",
		"/faturas/nova",
		"/recorrentes",
		"/recorrentes/novo",
		"/configuracoes",
		"/configuracoes/whatsapp/status",
		"/configuracoes/whatsapp/qr.png",
	}
	for _, old := range exactGetPaths {
		resp, err := ts.Client().Get(ts.URL + old)
		if err != nil {
			t.Fatalf("GET %s: %v", old, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: got %d want 404", old, resp.StatusCode)
		}
	}

	id := "test-id-123"
	cases := []struct {
		method string
		old    string
	}{
		{"GET", "/clientes/" + id},
		{"POST", "/clientes/" + id},
		{"GET", "/produtos/" + id + "/editar"},
		{"POST", "/produtos/" + id + "/editar"},
		{"GET", "/faturas/" + id},
		{"POST", "/faturas/" + id + "/enviar"},
		{"POST", "/faturas/" + id + "/marcar-paga"},
		{"POST", "/faturas/" + id + "/cancelar"},
		{"POST", "/recorrentes/" + id + "/excluir"},
		{"POST", "/recorrentes/" + id + "/alternar"},
		{"POST", "/configuracoes"},
		{"POST", "/configuracoes/whatsapp/conectar"},
		{"POST", "/clientes/novo"},
		{"POST", "/produtos/novo"},
		{"POST", "/faturas/nova"},
		{"POST", "/recorrentes/novo"},
	}
	for _, tc := range cases {
		var resp *http.Response
		var err error
		if tc.method == "GET" {
			resp, err = ts.Client().Get(ts.URL + tc.old)
		} else {
			resp, err = ts.Client().PostForm(ts.URL+tc.old, url.Values{})
		}
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.old, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s: got %d want 404", tc.method, tc.old, resp.StatusCode)
		}
	}
}

func TestNewEnglishRoutesWork(t *testing.T) {
	ts, repos := newTestEnv(t)

	english200 := []string{
		"/clients",
		"/clients/new",
		"/products",
		"/products/new",
		"/invoices",
		"/invoices/new",
		"/recurring",
		"/recurring/new",
		"/settings",
		"/settings/whatsapp/status",
	}
	for _, p := range english200 {
		status, body := get(t, ts, p)
		if status != http.StatusOK {
			t.Errorf("GET %s: got %d want 200\nbody: %s", p, status, body)
		}
	}

	// Detail routes need an existing record.
	clientID := seedClient(t, repos, "Route Test Client")
	status, body := get(t, ts, "/clients/"+clientID)
	if status != http.StatusOK {
		t.Fatalf("GET /clients/{id}: got %d want 200\nbody: %s", status, body)
	}
	if !strings.Contains(body, "Route Test Client") {
		t.Errorf("client detail missing name")
	}

	// Product edit: create product via repo, then GET edit page.
	ctx := context.Background()
	p, err := repos.Products.Create(ctx, &model.Product{Name: "RouteProd", UnitPrice: 1000})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	status, body = get(t, ts, "/products/"+p.ID+"/edit")
	if status != http.StatusOK {
		t.Fatalf("GET /products/{id}/edit: got %d want 200\nbody: %s", status, body)
	}
	if !strings.Contains(body, "RouteProd") {
		t.Errorf("product edit missing name")
	}

	// Invoice detail: seed invoice
	_, invID := seedInvoice(t, repos, "draft")
	status, body = get(t, ts, "/invoices/"+invID)
	if status != http.StatusOK {
		t.Fatalf("GET /invoices/{id}: got %d want 200 body: %s", status, body)
	}

	// POST new routes should work and redirect to English locations.
	// POST /clients/new -> 303 to /clients/{id}
	form := url.Values{"name": {"New Client Via English"}}
	resp, _ := postForm(t, ts, "/clients/new", form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /clients/new: got %d want 303", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/clients/") {
		t.Errorf("POST /clients/new redirect = %q want prefix /clients/", loc)
	}

	// POST /products/new -> 303 to /products
	resp, _ = postForm(t, ts, "/products/new", url.Values{"name": {"Prod2"}, "unit_price": {"10,00"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /products/new: got %d want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/products" {
		t.Errorf("POST /products/new redirect = %q want /products", loc)
	}

	// POST /invoices/new -> 303 to /invoices/{id}
	invForm := url.Values{
		"client_id":    {clientID},
		"issue_date":   {"2026-08-10"},
		"due_date":     {"2026-08-20"},
		"item_desc_0":  {"Svc"},
		"item_qty_0":   {"1"},
		"item_price_0": {"100,00"},
	}
	resp, body2 := postForm(t, ts, "/invoices/new", invForm)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /invoices/new: got %d want 303 body: %s", resp.StatusCode, body2)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/invoices/") {
		t.Errorf("POST /invoices/new redirect = %q want prefix /invoices/", loc)
	}

	// POST /recurring/new -> 303 to /recurring
	// Need a draft template already exists (seedInvoice created one)
	// Get draft invoice id for template
	invoices, _ := repos.Invoices.List(ctx)
	var templateID string
	for _, inv := range invoices {
		if inv.Status == "draft" {
			templateID = inv.ID
			break
		}
	}
	recForm := url.Values{
		"client_id":           {clientID},
		"invoice_template_id": {templateID},
		"frequency":           {"monthly"},
		"delivery_method":     {"email"},
		"next_send_date":      {"2026-09-01"},
	}
	resp, _ = postForm(t, ts, "/recurring/new", recForm)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /recurring/new: got %d want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/recurring" {
		t.Errorf("POST /recurring/new redirect = %q want /recurring", loc)
	}

	// Settings POST
	resp, _ = postForm(t, ts, "/settings", url.Values{"business_name": {"Test Biz"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /settings: got %d want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/settings?saved=1" {
		t.Errorf("POST /settings redirect = %q want /settings?saved=1", loc)
	}

	// Recurring toggle/delete and invoice actions need existing records but check 303 to English.
	// Create a recurring schedule via repo
	sched := &model.RecurringSchedule{
		ClientID:          clientID,
		InvoiceTemplateID: templateID,
		Frequency:         "monthly",
		NextSendDate:      dateUTC(2026, 9, 1),
		DeliveryMethod:    "email",
	}
	if err := repos.Recurring.Create(ctx, sched); err != nil {
		t.Fatalf("create recurring: %v", err)
	}
	resp, _ = postForm(t, ts, "/recurring/"+sched.ID+"/toggle", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /recurring/{id}/toggle: got %d want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/recurring" {
		t.Errorf("toggle redirect = %q want /recurring", loc)
	}
	resp, _ = postForm(t, ts, "/recurring/"+sched.ID+"/delete", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /recurring/{id}/delete: got %d want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/recurring" {
		t.Errorf("delete redirect = %q want /recurring", loc)
	}

	// Invoice mark-paid etc; need draft invoice
	draftInv := &model.Invoice{
		ClientID:  clientID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 8, 20),
		Items:     []*model.InvoiceItem{{Description: "Svc", Quantity: 1, UnitPrice: 1000}},
	}
	if err := repos.Invoices.Create(ctx, draftInv); err != nil {
		t.Fatalf("create draft invoice: %v", err)
	}
	resp, _ = postForm(t, ts, "/invoices/"+draftInv.ID+"/mark-paid", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /invoices/{id}/mark-paid: got %d want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/invoices/"+draftInv.ID {
		t.Errorf("mark-paid redirect = %q want /invoices/%s", loc, draftInv.ID)
	}
	// Send action is POST fragment returning 200, not redirect
	resp, _ = postForm(t, ts, "/invoices/"+draftInv.ID+"/send", url.Values{"method": {"email"}})
	// After marking paid, send should be 409, but we test with a new draft for send
	sendInv := &model.Invoice{
		ClientID:  clientID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 8, 20),
		Items:     []*model.InvoiceItem{{Description: "Svc", Quantity: 1, UnitPrice: 1000}},
	}
	if err := repos.Invoices.Create(ctx, sendInv); err != nil {
		t.Fatalf("create send invoice: %v", err)
	}
	resp, _ = postForm(t, ts, "/invoices/"+sendInv.ID+"/send", url.Values{"method": {"email"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /invoices/{id}/send: got %d want 200", resp.StatusCode)
	}
	// Cancel
	cancelInv := &model.Invoice{
		ClientID:  clientID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 8, 20),
		Items:     []*model.InvoiceItem{{Description: "Svc", Quantity: 1, UnitPrice: 1000}},
	}
	if err := repos.Invoices.Create(ctx, cancelInv); err != nil {
		t.Fatalf("create cancel invoice: %v", err)
	}
	resp, _ = postForm(t, ts, "/invoices/"+cancelInv.ID+"/cancel", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /invoices/{id}/cancel: got %d want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/invoices/"+cancelInv.ID {
		t.Errorf("cancel redirect = %q want /invoices/%s", loc, cancelInv.ID)
	}
}
