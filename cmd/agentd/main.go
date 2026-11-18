// Command agentd is the Charter core service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/db"
	"github.com/charterhq/charter/internal/run"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := runMain(log, os.Args[1:]); err != nil {
		log.Error("agentd failed", "err", err)
		os.Exit(1)
	}
}

func runMain(log *slog.Logger, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: agentd migrate | serve")
	}
	url := os.Getenv("CHARTER_DATABASE_URL")
	if url == "" {
		return errors.New("CHARTER_DATABASE_URL is not set")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch args[0] {
	case "migrate":
		return db.Migrate(ctx, pool)
	case "serve":
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
		// Stage 1 skeleton: stub drivers until the real ones land.
		drv, _, _, _ := stub.Set(&stub.Model{})
		eng, err := run.New(pool, drv, run.Config{Logger: log})
		if err != nil {
			return err
		}
		if err := eng.Start(ctx); err != nil {
			return err
		}
		log.Info("agentd started")
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return eng.Stop(shutdown)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
