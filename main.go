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

	"github.com/ajesus37/heavens-invoicing/internal/api"
	"github.com/ajesus37/heavens-invoicing/internal/auth"
	"github.com/ajesus37/heavens-invoicing/internal/db"
	"github.com/ajesus37/heavens-invoicing/internal/deliver"
	"github.com/ajesus37/heavens-invoicing/internal/repo"
	"github.com/ajesus37/heavens-invoicing/internal/server"
	"github.com/ajesus37/heavens-invoicing/internal/web"
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
	tgManager := NewTelegramManager(ctx, repos)

	pixFallback := settingOr(ctx, repos.Settings, repo.SettingDefaultPIXKey)
	senderInfo := setupSenderInfo(ctx, repos.Settings)
	router := deliver.NewRouter(repos.Invoices,
		tgManager,
		settingsLocale(repos.Settings),
		setupEmail(ctx, repos.Settings, pixFallback),
		setupWhatsAppDeliverer(waSession, pixFallback, senderInfo.Name),
		deliver.NewTelegramWithBusiness(tgManager, pixFallback, senderInfo.Name),
	)

	startScheduler(ctx, repos, router, tgManager, senderInfo)

	authManager := auth.New(repos.Sessions, repos.Settings)
	webHandlers, err := web.New(repos, router, waSession, senderInfo, authManager)
	if err != nil {
		log.Fatalf("web ui: %v", err)
	}
	webHandlers.SetTelegramReloader(tgManager.Reload)

	srv := server.New(server.Config{DataDir: dataDir, API: api.New(repos, router, senderInfo), Web: webHandlers.Mux(), Auth: authManager})
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
