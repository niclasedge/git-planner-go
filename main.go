// Command git-planner serves a GitHub issue planner, an Actions tracker and a
// config-driven dashboard from one binary.
//
// The design goal is that polling GitHub often should be nearly free: every
// request is conditional, ETags survive restarts in SQLite, and a 304 costs no
// rate limit. Watch /api/status to see it — cache_hits_304 climbs while
// remaining stays put.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/niclasedge/git-planner-go/internal/config"
	"github.com/niclasedge/git-planner-go/internal/gh"
	"github.com/niclasedge/git-planner-go/internal/hub"
	"github.com/niclasedge/git-planner-go/internal/panel"
	"github.com/niclasedge/git-planner-go/internal/store"
	"github.com/niclasedge/git-planner-go/internal/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "git-planner:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath = flag.String("config", "config.yaml", "path to config.yaml")
		envPath    = flag.String("env", ".env", "path to the .env file holding the tokens")
		verbose    = flag.Bool("v", false, "debug logging")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	shadowed, err := config.LoadDotEnv(*envPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", *envPath, err)
	}
	for _, key := range shadowed {
		// The environment wins by convention, but a token left over in a shell
		// profile shadowing the file you just edited only ever shows up as a 401.
		log.Warn("environment overrides "+*envPath,
			"key", key,
			"hint", "the value from the environment is used; run `unset "+key+"` to use the file")
	}

	cfg, warnings, err := config.Load(*configPath)
	for _, w := range warnings {
		log.Warn(w)
	}
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.Server.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	if entries, bytes, err := st.Stats(); err == nil && entries > 0 {
		// A warm cache means the first refresh is a series of 304s.
		log.Info("cache loaded", "entries", entries, "kb", bytes/1024)
	}

	api := gh.New(cfg.Tokens, st)

	panels, panelWarnings, err := panel.Build(cfg.Pages)
	for _, w := range panelWarnings {
		log.Warn(w)
	}
	if err != nil {
		return err
	}

	h := hub.New(cfg, api, log)

	srv, err := web.New(cfg, h, panels, api, st, log)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go h.Start(ctx)
	go panels.Start(ctx)

	httpSrv := &http.Server{
		Addr:              cfg.Server.Bind,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening",
			"addr", "http://"+cfg.Server.Bind,
			"tokens", len(cfg.Tokens),
			"pages", len(cfg.Pages))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}
