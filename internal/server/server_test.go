package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jesus/invoice-app/internal/server"
)

func TestHealthEndpoint(t *testing.T) {
	srv := server.New(server.Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
	if string(body) != "ok" {
		t.Fatalf("got %q want %q", body, "ok")
	}
}

func TestAPIMount(t *testing.T) {
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	srv := server.New(server.Config{API: apiMux})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
}

func TestNoAPIMountWhenNil(t *testing.T) {
	srv := server.New(server.Config{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/anything")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got %d want 404", resp.StatusCode)
	}
}
