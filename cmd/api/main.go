package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print build version")
	listenAddress := flag.String("listen", defaultListenAddress(), "HTTP listen address")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := &http.Server{
		Addr:              *listenAddress,
		Handler:           newHandler(),
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("Tacit API listening", "address", server.Addr, "version", version)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("Tacit API stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	case <-shutdownContext.Done():
		logger.Info("Tacit API shutdown requested")
	}

	drainContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(drainContext); err != nil {
		logger.Error("Tacit API shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("Tacit API stopped")
}

func defaultListenAddress() string {
	if address := os.Getenv("TACIT_API_ADDRESS"); address != "" {
		return address
	}
	return "127.0.0.1:8080"
}

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", writeHealthy)
	mux.HandleFunc("GET /readyz", writeHealthy)
	mux.HandleFunc("POST /graphql", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusNotImplemented, map[string]string{
			"code":    "GRAPHQL_NOT_IMPLEMENTED",
			"message": "The GraphQL control plane is implemented by TAC-004.",
		})
	})
	return mux
}

func writeHealthy(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}
