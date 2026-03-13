package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/rigel-labs/rigel-build-engine/internal/app"
	"github.com/rigel-labs/rigel-build-engine/internal/config"
	"github.com/rigel-labs/rigel-build-engine/internal/repository/postgres"
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	repo, err := postgres.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("init repository: %v", err)
	}
	defer func() {
		if err := repo.Close(); err != nil {
			log.Printf("close repository: %v", err)
		}
	}()

	builder := buildservice.New(repo, time.Now)
	application := app.New(cfg, builder)
	server := &http.Server{Addr: ":" + cfg.HTTPPort, Handler: application.Handler(), ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown server: %v", err)
		}
	}()

	log.Printf("starting %s on :%s", cfg.ServiceName, cfg.HTTPPort)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server exited: %v", err)
	}
}
