package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/merlon-aml/merlon/api/internal/config"
	"github.com/merlon-aml/merlon/api/internal/engine"
	"github.com/merlon-aml/merlon/api/internal/server"
	"github.com/merlon-aml/merlon/api/internal/store"
)

func main() {
	cfg := config.Load()

	deps := server.Deps{
		Customers:    store.NewMemoryCustomerRepo(),
		Transactions: store.NewMemoryTransactionRepo(),
		Alerts:       store.NewMemoryAlertRepo(),
	}

	if cfg.EngineAddr != "" {
		client, err := engine.NewClient(cfg.EngineAddr)
		if err != nil {
			log.Fatalf("engine client: %v", err)
		}
		defer client.Close()
		deps.Scoring = client
		deps.Monitoring = client
		deps.Screening = client
		deps.Backtest = client
		log.Printf("engine connected: %s", cfg.EngineAddr)
	} else {
		log.Printf("warning: MERLON_ENGINE_ADDR not set, engine endpoints disabled")
	}

	srv := server.New(cfg.HTTPAddr, deps)

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: srv.Handler(),
	}

	go func() {
		log.Printf("merlon-api starting env=%s addr=%s", cfg.Env, cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Printf("merlon-api shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}

	log.Printf("merlon-api stopped")
}
