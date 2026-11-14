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

	graph "github.com/shakcho/tacit/internal/adapters/graphql"
	"github.com/shakcho/tacit/internal/platform/process"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print build version")
	listenAddress := flag.String("listen", defaultListenAddress(), "HTTP listen address")
	drainTimeout := flag.Duration("drain-timeout", 10*time.Second, "shutdown drain bound")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := process.Run(shutdownContext, process.Config{
		Role: "api", Listen: *listenAddress, DrainTimeout: *drainTimeout, Logger: logger,
	}, func(process.SocketTracker) http.Handler { return newHandler() }); err != nil {
		logger.Error("process failed", "role", "api")
		os.Exit(1)
	}
}

func defaultListenAddress() string {
	if address := os.Getenv("TACIT_API_ADDRESS"); address != "" {
		return address
	}
	return "127.0.0.1:8080"
}

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /graphql", graph.Boundary(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusNotImplemented, map[string]string{
			"code":    "GRAPHQL_NOT_IMPLEMENTED",
			"message": "The GraphQL control plane is implemented by TAC-004.",
		})
	})))
	return mux
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
