package server

import (
	"net/http"
)

type Config struct {
	DataDir string
	// API, when non-nil, is mounted at /api/ (the JSON API mux built by
	// the api package; its routes carry their own full /api/... paths).
	API http.Handler
	// Web, when non-nil, is mounted at / and serves the html/template UI.
	Web http.Handler
}

type Server struct {
	cfg Config
}

func New(cfg Config) *Server { return &Server{cfg: cfg} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	if s.cfg.API != nil {
		mux.Handle("/api/", s.cfg.API)
	}
	if s.cfg.Web != nil {
		mux.Handle("/", s.cfg.Web)
	}
	return mux
}
