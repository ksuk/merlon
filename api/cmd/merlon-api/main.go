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
