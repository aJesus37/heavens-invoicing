package telegram_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jesus/invoice-app/internal/telegram"
)

const testToken = "123456:ABC-TOKEN"

var errDial = errors.New("dial tcp: connection refused")

type recordedRequest struct {
	path        string
	contentType string
	fields      map[string]string
	fileName    string
	fileContent []byte
}

// readMultipart parses a multipart/form-data request into its field values
// and, when present, the "document" file part.
func readMultipart(t *testing.T, r *http.Request) recordedRequest {
	t.Helper()
	rec := recordedRequest{
		path:        r.URL.Path,
		contentType: r.Header.Get("Content-Type"),
		fields:      map[string]string{},
	}
	mr, err := r.MultipartReader()
	if err != nil {
		t.Errorf("multipart body expected: %v", err)
		return rec
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Errorf("reading multipart: %v", err)
			break
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Errorf("reading part %q: %v", part.FormName(), err)
			continue
		}
		if part.FileName() == "" {
			rec.fields[part.FormName()] = string(data)
			continue
		}
		if part.FormName() != "document" {
			t.Errorf("unexpected file part %q", part.FormName())
			continue
		}
		rec.fileName = part.FileName()
		rec.fileContent = data
	}
	return rec
}

func okHandler(t *testing.T, wantPath string, check func(t *testing.T, rec recordedRequest)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		rec := readMultipart(t, r)
		if rec.path != wantPath {
			t.Errorf("path: want %q, got %q", wantPath, rec.path)
		}
		check(t, rec)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
	}
}

func TestSendMessageSuccess(t *testing.T) {
	srv := httptest.NewServer(okHandler(t, "/bot"+testToken+"/sendMessage", func(t *testing.T, rec recordedRequest) {
		if !strings.HasPrefix(rec.contentType, "multipart/form-data") {
			t.Errorf("content type: want multipart/form-data, got %q", rec.contentType)
		}
		for field, want := range map[string]string{"chat_id": "42", "text": "Olá"} {
			if got := rec.fields[field]; got != want {
				t.Errorf("%s: want %q, got %q", field, want, got)
			}
		}
	}))
	defer srv.Close()

	client := telegram.NewClient(srv.Client(), srv.URL, testToken)
	if err := client.SendMessage(context.Background(), "42", "Olá"); err != nil {
		t.Fatal(err)
	}
}

func TestSendDocumentSuccess(t *testing.T) {
	pdf := []byte("%PDF-fake-bytes")
	srv := httptest.NewServer(okHandler(t, "/bot"+testToken+"/sendDocument", func(t *testing.T, rec recordedRequest) {
		for field, want := range map[string]string{"chat_id": "42", "caption": "Fatura #000001"} {
			if got := rec.fields[field]; got != want {
				t.Errorf("%s: want %q, got %q", field, want, got)
			}
		}
		if rec.fileName != "fatura-000001.pdf" {
			t.Errorf("document filename: want %q, got %q", "fatura-000001.pdf", rec.fileName)
		}
		if string(rec.fileContent) != string(pdf) {
			t.Error("document content differs")
		}
	}))
	defer srv.Close()

	client := telegram.NewClient(srv.Client(), srv.URL, testToken)
	err := client.SendDocument(context.Background(), "42", "fatura-000001.pdf", pdf, "Fatura #000001")
	if err != nil {
		t.Fatal(err)
	}
}

func TestAPIErrorDescriptionFromJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
	}))
	defer srv.Close()

	client := telegram.NewClient(srv.Client(), srv.URL, testToken)

	err := client.SendMessage(context.Background(), "42", "hi")
	if err == nil || !strings.Contains(err.Error(), "Bad Request: chat not found") {
		t.Fatalf("want error with description, got %v", err)
	}
}

func TestHTTPErrorRawBodyFallback(t *testing.T) {
	garbage := "<html>gateway exploded</html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, garbage)
	}))
	defer srv.Close()

	client := telegram.NewClient(srv.Client(), srv.URL, testToken)

	err := client.SendDocument(context.Background(), "42", "x.pdf", []byte("pdf"), "cap")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	for _, want := range []string{"500", garbage} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestOKFalseOnHTTP200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":false,"description":"weird but possible"}`)
	}))
	defer srv.Close()

	client := telegram.NewClient(srv.Client(), srv.URL, testToken)

	if err := client.SendMessage(context.Background(), "42", "hi"); err == nil ||
		!strings.Contains(err.Error(), "weird but possible") {
		t.Fatalf("want error with description, got %v", err)
	}
}

// urlCapturingTransport records request URLs without touching the network,
// which is how the api.telegram.org default can be asserted offline.
type urlCapturingTransport struct{ lastURL string }

func (tr *urlCapturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.lastURL = req.URL.String()
	rec := httptest.NewRecorder()
	rec.WriteString(`{"ok":true}`)
	return rec.Result(), nil
}

func TestEmptyBaseURLDefaultsToTelegramAPI(t *testing.T) {
	tr := &urlCapturingTransport{}
	client := telegram.NewClient(&http.Client{Transport: tr}, "", testToken)

	if err := client.SendMessage(context.Background(), "42", "hi"); err != nil {
		t.Fatal(err)
	}
	want := "https://api.telegram.org/bot" + testToken + "/sendMessage"
	if tr.lastURL != want {
		t.Fatalf("url: want %q, got %q", want, tr.lastURL)
	}
}

// failingTransport simulates a transport-level failure whose message quotes
// the full request URL, like *url.Error does.
type failingTransport struct{ cause error }

func (tr failingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, &url.Error{Op: "Post", URL: req.URL.String(), Err: tr.cause}
}

func TestTransportErrorRedactsToken(t *testing.T) {
	client := telegram.NewClient(&http.Client{Transport: failingTransport{cause: errDial}}, "", testToken)

	err := client.SendMessage(context.Background(), "42", "hi")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("token leaked in error: %v", err)
	}
	for _, want := range []string{"***", "/bot", "sendMessage"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
	if !errors.Is(err, errDial) {
		t.Fatalf("errors.Is should reach the wrapped cause, got %v", err)
	}
}

var _ interface {
	SendMessage(ctx context.Context, chatID, text string) error
	SendDocument(ctx context.Context, chatID, filename string, content []byte, caption string) error
} = (*telegram.Client)(nil)
