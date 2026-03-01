package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/legamerdc/knowledge-hub/internal/server/handlers"
	"github.com/legamerdc/knowledge-hub/internal/server/service"
	"github.com/legamerdc/knowledge-hub/pkg/corestore"
)

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "data.db"
	}
	return filepath.Join(home, ".knowledge-hub", "data.db")
}

func main() {
	addr := flag.String("addr", envOr("KH_ADDR", ":19820"), "listen address")
	dbPath := flag.String("db", envOr("KH_DB", defaultDBPath()), "SQLite database path")
	flag.Parse()

	// Ensure data directory exists
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	store, err := corestore.New(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	svc := service.New(store)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	handlers.HandlerFromMux(svc, r)

	srv := &http.Server{
		Addr:    *addr,
		Handler: r,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		fmt.Printf("Knowledge Hub API Server listening on %s (db: %s)\n", *addr, *dbPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-quit
	fmt.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	fmt.Println("Server stopped.")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
