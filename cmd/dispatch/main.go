package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shakcho/tacit/internal/platform/process"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print build version")
	listenAddress := flag.String("listen", "127.0.0.1:8082", "HTTP listen address")
	drainTimeout := flag.Duration("drain-timeout", 10*time.Second, "shutdown drain bound")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := process.Run(ctx, process.Config{
		Role: "dispatch", Listen: *listenAddress, DrainTimeout: *drainTimeout, Logger: logger,
	}, func(process.SocketTracker) http.Handler { return http.NotFoundHandler() }); err != nil {
		logger.Error("process failed", "role", "dispatch")
		os.Exit(1)
	}
}
