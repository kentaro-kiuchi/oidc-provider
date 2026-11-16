package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kentaro-kiuchi/oidc-provider/provider/internal/database"
	"github.com/kentaro-kiuchi/oidc-provider/provider/internal/migration"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "version") {
		return errors.New("usage: oidc-provider-migrate <up|version>")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	db, err := database.Open(ctx, database.ConfigFromEnv())
	if err != nil {
		return err
	}
	defer db.Close()
	// The session advisory lock and migration statements share one connection.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	switch os.Args[1] {
	case "up":
		results, err := migration.Up(ctx, db, migration.Files)
		if err != nil {
			return err
		}
		if len(results) == 0 {
			log.Print("database schema is already up to date")
			return nil
		}
		for _, result := range results {
			log.Printf("applied migration %d (%s) in %s", result.Source.Version, result.Source.Path, result.Duration)
		}
	case "version":
		version, err := migration.CurrentVersion(ctx, db, migration.Files)
		if err != nil {
			return err
		}
		fmt.Println(version)
	}
	return nil
}
