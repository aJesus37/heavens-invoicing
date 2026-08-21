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
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
	if string(body) != "ok" {
		t.Fatalf("got %q want %q", body, "ok")
	}
}
