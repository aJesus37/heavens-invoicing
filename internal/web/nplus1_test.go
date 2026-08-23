package web_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ajesus37/heavens-invoicing/internal/model"
)

// TestListRecurringPreloads verifies that listRecurring preloads
// clients and invoices maps instead of per-row Get calls (N+1 fix).
func TestListRecurringPreloads(t *testing.T) {
	data, err := os.ReadFile("recurring.go")
	if err != nil {
		// fallback when running from repo root
		data, err = os.ReadFile("internal/web/recurring.go")
		if err != nil {
			t.Fatalf("read recurring.go: %v", err)
		}
	}
	src := string(data)

	// isolate listRecurring func body
	start := strings.Index(src, "func (h *Handlers) listRecurring")
	if start == -1 {
		t.Fatalf("listRecurring func not found")
	}
	end := strings.Index(src[start+1:], "\nfunc ")
	var body string
	if end == -1 {
		body = src[start:]
	} else {
		body = src[start : start+1+end]
	}

	if !strings.Contains(body, "Clients.List") {
		t.Errorf("listRecurring should preload clients via Clients.List once, missing Clients.List")
	}
	if !strings.Contains(body, "Invoices.List") {
		t.Errorf("listRecurring should preload invoices via Invoices.List once, missing Invoices.List")
	}
	if !strings.Contains(body, "map[string]") {
		t.Errorf("listRecurring should use map[string] for preloaded lookups")
	}
	// N+1 markers should be absent inside the loop after fix
	if strings.Contains(body, "h.clientName(ctx") {
		t.Errorf("listRecurring still uses per-row h.clientName (N+1) - should use preloaded map")
	}
	if strings.Contains(body, "h.repos.Invoices.Get(ctx") {
		t.Errorf("listRecurring still uses per-row Invoices.Get (N+1) - should use preloaded map")
	}
}

// TestDashboardUpcomingPreloads verifies that dashboard upcoming
// section preloads maps instead of per-row Gets.
func TestDashboardUpcomingPreloads(t *testing.T) {
	data, err := os.ReadFile("dashboard.go")
	if err != nil {
		data, err = os.ReadFile("internal/web/dashboard.go")
		if err != nil {
			t.Fatalf("read dashboard.go: %v", err)
		}
	}
	src := string(data)

	start := strings.Index(src, "func (h *Handlers) dashboard")
	if start == -1 {
		t.Fatalf("dashboard func not found")
	}
	end := strings.Index(src[start+1:], "\nfunc ")
	var body string
	if end == -1 {
		body = src[start:]
	} else {
		body = src[start : start+1+end]
	}

	// Find upcoming section (after horizon)
	horizonIdx := strings.Index(body, "horizon :=")
	if horizonIdx == -1 {
		t.Fatalf("horizon not found in dashboard body")
	}
	upcomingSection := body[horizonIdx:]

	if !strings.Contains(upcomingSection, "Clients.List") {
		t.Errorf("dashboard upcoming should preload clients via Clients.List, missing in upcoming section")
	}
	if !strings.Contains(upcomingSection, "Invoices.List") {
		t.Errorf("dashboard upcoming should preload invoices via Invoices.List, missing in upcoming section")
	}
	if !strings.Contains(upcomingSection, "map[string]") {
		t.Errorf("dashboard upcoming should use map[string] for preloaded lookups")
	}
	if strings.Contains(upcomingSection, "h.clientName(ctx") {
		t.Errorf("dashboard upcoming still uses per-row h.clientName (N+1)")
	}
	if strings.Contains(upcomingSection, "h.repos.Invoices.Get(ctx") {
		t.Errorf("dashboard upcoming still uses per-row Invoices.Get (N+1)")
	}
}

// TestRecurringAndDashboardCorrectness verifies functional correctness
// after preload: multiple schedules with different clients/templates
// still render correct names and numbers.
func TestRecurringAndDashboardCorrectness(t *testing.T) {
	ts, repos := newTestEnv(t)
	ctx := context.Background()

	// create 3 clients
	clientIDs := make([]string, 3)
	clientNames := []string{"Alice Co", "Bob Ltda", "Carol SA"}
	for i, name := range clientNames {
		c, err := repos.Clients.Create(ctx, &model.Client{Name: name})
		if err != nil {
			t.Fatalf("create client %q: %v", name, err)
		}
		clientIDs[i] = c.ID
	}

	// create 3 draft invoices as templates, each for distinct client
	tplIDs := make([]string, 3)
	var tplNumbers []int64
	for i, cid := range clientIDs {
		inv := &model.Invoice{
			ClientID:  cid,
			Status:    "draft",
			IssueDate: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
			DueDate:   time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
			Items:     []*model.InvoiceItem{{Description: "Serviço", UnitPrice: 1000, Quantity: 1}},
		}
		if err := repos.Invoices.Create(ctx, inv); err != nil {
			t.Fatalf("create template %d: %v", i, err)
		}
		tplIDs[i] = inv.ID
		tplNumbers = append(tplNumbers, inv.Number)
	}

	// create 3 recurring schedules, each linking client and its template
	for i := range clientIDs {
		s := &model.RecurringSchedule{
			ClientID:          clientIDs[i],
			InvoiceTemplateID: tplIDs[i],
			Frequency:         "monthly",
			NextSendDate:      time.Now().AddDate(0, 0, 1), // within dashboard horizon
			DeliveryMethod:    "email",
		}
		if err := repos.Recurring.Create(ctx, s); err != nil {
			t.Fatalf("create schedule %d: %v", i, err)
		}
	}

	// Verify /recurring renders all client names and template numbers
	status, body := get(t, ts, "/recurring")
	if status != 200 {
		t.Fatalf("GET /recurring: got %d want 200 body %s", status, body)
	}
	for _, name := range clientNames {
		if !strings.Contains(body, name) {
			t.Errorf("/recurring missing client name %q", name)
		}
	}
	for _, num := range tplNumbers {
		if !strings.Contains(body, formatNumber(num)) {
			t.Errorf("/recurring missing template number %d (expected %q)", num, formatNumber(num))
		}
	}

	// Verify dashboard upcoming also renders them
	status, body = get(t, ts, "/")
	if status != 200 {
		t.Fatalf("GET / dashboard: got %d want 200", status)
	}
	for _, name := range clientNames {
		if !strings.Contains(body, name) {
			t.Errorf("dashboard missing client name %q in upcoming", name)
		}
	}
	for _, num := range tplNumbers {
		if !strings.Contains(body, formatNumber(num)) {
			t.Errorf("dashboard missing template number %d (expected %q)", num, formatNumber(num))
		}
	}

	// Verify fallback not crash: map miss should degrade gracefully (code path tested via handler maps)
	// FK prevents inserting bogus schedule, so we verify the map fallback logic exists in source
	data, _ := os.ReadFile("recurring.go")
	if len(data) == 0 {
		data, _ = os.ReadFile("internal/web/recurring.go")
	}
	if !strings.Contains(string(data), `if name == ""`) {
		t.Errorf("fallback for missing client name not found in recurring.go")
	}
}

func formatNumber(n int64) string {
	// matches recurring template: # + 6 digits
	if n < 10 {
		return "#00000" + numToStr(n)
	}
	return "#" + pad6(n)
}

func numToStr(n int64) string {
	// simple itoa
	if n == 0 {
		return "0"
	}
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func pad6(n int64) string {
	s := numToStr(n)
	for len(s) < 6 {
		s = "0" + s
	}
	return s
}
