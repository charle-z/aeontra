package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/publicsite"
)

const defaultListenAddress = ":8080"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	runtimeURL := strings.TrimSpace(os.Getenv("AEONTRA_PUBLIC_RUNTIME_URL"))
	handler, err := publicsite.New(publicsite.Options{RuntimeURL: runtimeURL})
	if err != nil {
		return err
	}
	address := strings.TrimSpace(os.Getenv("AEONTRA_SITE_ADDR"))
	if address == "" {
		address = defaultListenAddress
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() {
		log.Printf("aeontra site listening on %s", address)
		done <- server.ListenAndServe()
	}()
	select {
	case serveErr := <-done:
		if errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return serveErr
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		serveErr := <-done
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	}
}
