package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
	"github.com/jesus/invoice-app/internal/web"
)

// newTestEnv boots the real stack (sqlite repos + router + web handlers)
// against a throwaway database and returns a live test server along with
// its repos for direct seeding.
func newTestEnv(t *testing.T) (*httptest.Server, *repo.Repos) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	repos := repo.New(conn)

	router := deliver.NewRouter(repos.Invoices, nil, nil, nil, nil)
	handlers, err := web.New(repos, router, nil, pdf.SenderInfo{})
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	ts := httptest.NewServer(handlers.Mux())
	// Keep redirect responses visible so flows can assert 303 + Location.
	ts.Client().CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return ts, repos
}

func dateUTC(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

func get(t *testing.T, ts *httptest.Server, path string) (int, string) {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func postForm(t *testing.T, ts *httptest.Server, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	resp, err := ts.Client().PostForm(ts.URL+path, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestDashboardRenders(t *testing.T) {
	ts, _ := newTestEnv(t)
	status, body := get(t, ts, "/")
	if status != http.StatusOK {
		t.Fatalf("got %d want 200", status)
	}
	for _, marker := range []string{"Dashboard", "Faturas pendentes", "Recorrentes nos próximos 7 dias", "/clientes"} {
		if !strings.Contains(body, marker) {
			t.Errorf("dashboard missing marker %q", marker)
		}
	}
}

func TestClientCreateFlowEndToEnd(t *testing.T) {
	ts, _ := newTestEnv(t)

	form := url.Values{
		"name":    {"Acme Ltda"},
		"email":   {"contato@acme.com"},
		"phone":   {"+5511999999999"},
		"address": {"Rua das Flores, 123 - São Paulo/SP"},
	}
	resp, body := postForm(t, ts, "/clientes/novo", form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create: got %d want 303\nbody: %s", resp.StatusCode, body)
	}

	status, body := get(t, ts, resp.Header.Get("Location"))
	if status != http.StatusOK {
		t.Fatalf("detail: got %d want 200", status)
	}
	for _, marker := range []string{"Acme Ltda", "contato@acme.com", "5511999999999", "Rua das Flores"} {
		if !strings.Contains(body, marker) {
			t.Errorf("client detail missing %q", marker)
		}
	}

	status, body = get(t, ts, "/clientes")
	if status != http.StatusOK || !strings.Contains(body, "Acme Ltda") {
		t.Fatalf("client list should show created client (status=%d)", status)
	}

	// Blank name must be rejected without creating anything.
	resp2, _ := postForm(t, ts, "/clientes/novo", url.Values{"name": {" "}})
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank name: got %d want 400", resp2.StatusCode)
	}
}

func TestClientUpdateFlow(t *testing.T) {
	ts, repos := newTestEnv(t)

	form := url.Values{"name": {"Beta"}}
	resp, _ := postForm(t, ts, "/clientes/novo", form)
	id := strings.TrimPrefix(resp.Header.Get("Location"), "/clientes/")

	resp, body := postForm(t, ts, "/clientes/"+id, url.Values{
		"name":  {"Beta SA"},
		"notes": {"atualizado"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update: got %d want 303 (body: %s)", resp.StatusCode, body)
	}
	_, body = get(t, ts, "/clientes/"+id)
	if !strings.Contains(body, "Beta SA") || !strings.Contains(body, "atualizado") {
		t.Fatal("update did not persist")
	}

	// Updates trim the name just like creation does.
	respTrim, _ := postForm(t, ts, "/clientes/"+id, url.Values{"name": {"  Gamma  "}})
	if respTrim.StatusCode != http.StatusSeeOther {
		t.Fatalf("trim update: got %d want 303", respTrim.StatusCode)
	}
	got, err := repos.Clients.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Gamma" {
		t.Fatalf("update did not trim name, got %q", got.Name)
	}
}

func TestClientDetailNotFound(t *testing.T) {
	ts, _ := newTestEnv(t)
	status, _ := get(t, ts, "/clientes/naoexiste")
	if status != http.StatusNotFound {
		t.Fatalf("got %d want 404", status)
	}
}
