package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jesus/invoice-app/internal/api"
	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/repo"
	"github.com/jesus/invoice-app/internal/server"
	"github.com/jesus/invoice-app/internal/web"
)

const (
	dataDir = "./data"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	conn, err := db.Open(filepath.Join(dataDir, "app.db"))
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	repos := repo.New(conn)

	waSession := setupWhatsApp(ctx, conn)
	if waSession != nil {
		defer waSession.Close()
	}
	tgClient, adminChatID := setupTelegram(ctx, repos.Settings)
	startAdminBot(ctx, tgClient, adminChatID, repos)

	pixFallback := settingOr(ctx, repos.Settings, repo.SettingDefaultPIXKey)
	senderInfo := setupSenderInfo(ctx, repos.Settings)
	router := deliver.NewRouter(repos.Invoices,
		tgNotifier(tgClient, adminChatID),
		settingsLocale(repos.Settings),
		setupEmail(ctx, repos.Settings, pixFallback),
		setupWhatsAppDeliverer(waSession, pixFallback),
		setupTelegramDeliverer(tgClient, pixFallback),
	)

	startScheduler(ctx, repos, router, tgNotifier(tgClient, adminChatID), senderInfo)

	webHandlers, err := web.New(repos, router, waSession, senderInfo)
	if err != nil {
		log.Fatalf("web ui: %v", err)
	}

	srv := server.New(server.Config{DataDir: dataDir, API: api.New(repos, router, senderInfo), Web: webHandlers.Mux()})
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Println("listening on " + addr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen and serve: %v", err)
	}
}
