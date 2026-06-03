package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/app"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/config"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/httpserver"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runtimeApp, err := app.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = runtimeApp.Close(context.Background())
	}()

	server := &http.Server{
		Addr:    cfg.Address(),
		Handler: httpserver.New(runtimeApp).Handler(),
	}

	go func() {
		log.Printf("service listening on http://%s", cfg.Address())
		log.Printf("auth token: %s", cfg.AuthToken)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
