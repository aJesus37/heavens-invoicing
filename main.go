package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/server"
)

func main() {
	if err := os.MkdirAll("./data", 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	conn, err := db.Open("./data/app.db")
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	srv := server.New(server.Config{DataDir: "./data"})
	httpSrv := &http.Server{
		Addr:              ":8080",
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Println("listening on :8080")
	log.Fatal(httpSrv.ListenAndServe())
}
