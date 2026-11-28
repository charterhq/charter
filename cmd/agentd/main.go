// Command agentd is the Charter core service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // CRON_TZ and CHARTER_SCHEDULE_TZ work in images without a zone database

	"github.com/charterhq/charter/internal/api"
	"github.com/charterhq/charter/internal/config"
	"github.com/charterhq/charter/internal/db"
	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/pkg"
	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/telemetry"
)

var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := runMain(log, os.Args[1:]); err != nil {
		log.Error("agentd failed", "err", err)
		os.Exit(1)
	}
}

func runMain(log *slog.Logger, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: agentd migrate | serve | apply <package-dir> [git-sha]")
	}
	url := config.Env("CHARTER_DATABASE_URL")
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
	case "apply":
		if len(args) < 2 {
			return errors.New("usage: agentd apply <package-dir> [git-sha]")
		}
		sha := "local"
		if len(args) > 2 {
			sha = args[2]
		}
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
		fsys := os.DirFS(args[1])
		p, problems := pkg.Load(fsys)
		if problems != nil {
			return fmt.Errorf("package has problems:\n%w", problems)
		}
		stored, err := pkg.Compile(p)
		if err != nil {
			return err
		}
		src, err := pkg.SourceDigest(fsys)
		if err != nil {
			return err
		}
		m := p.Agent.Metadata
		id, err := registry.ApplySource(ctx, pool, registry.Agent{Name: m.Name, Team: m.Team, Owner: m.Owner}, m.Version, sha, src, stored)
		if err != nil {
			return err
		}
		log.Info("package applied", "agent", m.Name, "version", m.Version, "version_id", id)
		return nil
	case "serve":
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
		drv, journal, err := config.Drivers(log)
		if err != nil {
			return err
		}
		// Where the platform owns the journal schema (migrations elsewhere), set CHARTER_CH_ENSURE_SCHEMA=false.
		if journal != nil && config.Env("CHARTER_CH_ENSURE_SCHEMA") != "false" {
			if err := journal.EnsureSchema(ctx); err != nil {
				return err
			}
		}
		tel, err := telemetry.Setup(ctx, "charter-agentd", version)
		if err != nil {
			return err
		}
		defer func() {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = tel.Shutdown(sctx)
		}()
		loc := time.UTC
		if tz := config.Env("CHARTER_SCHEDULE_TZ"); tz != "" {
			if loc, err = time.LoadLocation(tz); err != nil {
				return fmt.Errorf("CHARTER_SCHEDULE_TZ: %w", err)
			}
		}
		eng, err := run.New(pool, drv, run.Config{Logger: log, Telemetry: tel, ScheduleLocation: loc})
		if err != nil {
			return err
		}
		if err := eng.Start(ctx); err != nil {
			return err
		}
		// Channel drivers that receive messages listen as the agents' bots; drivers with callbacks
		// (chat buttons) get a path under /v1/drivers/.
		var callbacks []driver.Callbacks
		for sys, ch := range drv.Channels {
			if rcv, ok := ch.(driver.Receiver); ok {
				go func() {
					if err := rcv.Receive(ctx, eng); err != nil {
						log.Error("channel driver stopped receiving", "system", sys, "err", err)
					}
				}()
			}
			if cb, ok := ch.(driver.Callbacks); ok {
				callbacks = append(callbacks, cb)
			}
		}
		tokens, err := config.Tokens()
		if err != nil {
			return err
		}
		listen := config.Env("CHARTER_LISTEN")
		if listen == "" {
			listen = ":8080"
		}
		apiSrv := &api.Server{Pool: pool, Engine: eng, Tokens: tokens,
			Publishers: config.Publishers(), Platform: config.Platform(), Formats: drv.Formats, Callbacks: callbacks,
			JudgeClass: config.Env("CHARTER_EVAL_JUDGE_CLASS"), PlatformGroup: config.Env("CHARTER_PLATFORM_GROUP"), Log: log}
		if issuer := config.Env("CHARTER_OIDC_ISSUER"); issuer != "" {
			apiSrv.OIDC = &api.OIDC{Issuer: issuer, ClientID: config.Env("CHARTER_OIDC_CLIENT_ID"),
				ClientSecret: config.Env("CHARTER_OIDC_CLIENT_SECRET"), RedirectURL: config.Env("CHARTER_OIDC_REDIRECT_URL"),
				CookiePath: config.Env("CHARTER_OIDC_COOKIE_PATH")}
			if err := apiSrv.OIDC.Validate(); err != nil {
				return err
			}
			log.Info("people log in through OIDC", "issuer", issuer, "redirect", apiSrv.OIDC.RedirectURL)
		}
		srv := &http.Server{
			Addr:              listen,
			Handler:           apiSrv.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		srv.RegisterOnShutdown(apiSrv.StopStreams) // event streams would hold Shutdown for its whole timeout
		errc := make(chan error, 1)
		go func() { errc <- srv.ListenAndServe() }()
		log.Info("agentd started", "listen", listen)
		select {
		case <-ctx.Done():
		case err := <-errc:
			return err
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		return eng.Stop(shutdown)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
