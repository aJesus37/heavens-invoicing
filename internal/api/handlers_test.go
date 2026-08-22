package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jesus/invoice-app/internal/api"
	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
)

// countingChannel is a fake deliverer recording calls.
type countingChannel struct {
	name          string
	err           error
	invoiceCalls  int
	reminderCalls int
}

func (c *countingChannel) Name() string { return c.name }

func (c *countingChannel) SendInvoice(_ context.Context, _ model.Client, _ model.Invoice, _ []byte) error {
	c.invoiceCalls++
	return c.err
}

func (c *countingChannel) SendReminder(_ context.Context, _ model.Client, _ model.Invoice) error {
	c.reminderCalls++
	return c.err
}

type recordingNotifier struct {
	texts []string
}

func (n *recordingNotifier) Notify(_ context.Context, text string) error {
	n.texts = append(n.texts, text)
	return nil
}

type testEnv struct {
	handler  http.Handler
	repos    *repo.Repos
	email    *countingChannel
	wa       *countingChannel
	tg       *countingChannel
	notifier *recordingNotifier
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	env := &testEnv{
		repos:    repo.New(conn),
		email:    &countingChannel{name: "email"},
		wa:       &countingChannel{name: "whatsapp"},
		tg:       &countingChannel{name: "telegram"},
		notifier: &recordingNotifier{},
	}
	router := deliver.NewRouter(env.repos.Invoices, env.notifier, env.email, env.wa, env.tg)
	env.handler = api.New(env.repos, router, pdf.SenderInfo{Name: "Teste Ltda", Address: "Rua Um, 1", PIXKey: "pix@teste"})
	return env
}

// do performs a JSON request against the handler; body may be nil.
func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// doRaw sends a raw string body (for malformed-JSON cases).
func doRaw(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, context string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: status = %d, want %d (body: %s)", context, rec.Code, want, rec.Body.String())
	}
}

func strP(s string) *string { return &s }
