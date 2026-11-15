package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/shakcho/tacit/internal/platform/postgres"
)

var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	if len(os.Args) != 2 || os.Args[1] != "migrate" {
		fmt.Fprintln(os.Stderr, "usage: tacit-db migrate | tacit-db --version")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := postgres.Migrate(ctx, os.Getenv("TACIT_DATABASE_DIRECT_URL")); err != nil {
		fmt.Fprintln(os.Stderr, "database migration failed")
		os.Exit(1)
	}
	fmt.Printf("database migrated to schema version %d\n", postgres.CurrentSchemaVersion)
}
