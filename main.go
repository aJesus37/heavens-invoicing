package main

import (
	"log"
	"net/http"

	"github.com/jesus/invoice-app/internal/server"
)

func main() {
	srv := server.New(server.Config{DataDir: "./data"})
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", srv.Handler()))
}
