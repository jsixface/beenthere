package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sixface/beenthere/internal/api"
	"github.com/sixface/beenthere/internal/config"
	"github.com/sixface/beenthere/internal/geocode"
	"github.com/sixface/beenthere/internal/jobs"
	"github.com/sixface/beenthere/internal/migrate"
	"github.com/sixface/beenthere/internal/store"
	"github.com/sixface/beenthere/internal/web"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if len(os.Args) > 1 && os.Args[1] == "adduser" {
		addUser(log)
		return
	}
	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL, cfg.PoolMax)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	if created, err := migrate.Apply(ctx, st.Pool); err != nil {
		log.Error("schema", "err", err)
		os.Exit(1)
	} else if created {
		log.Info("created database schema", "version", migrate.SchemaVersion)
	}

	mgr := jobs.New(ctx, st, cfg.Workers, log)
	if gc, ok := geocode.FromEnv(); ok {
		go geocode.Run(ctx, st, gc, log)
	}
	mux := http.NewServeMux()
	(&api.Server{S: st, Jobs: mgr, Log: log, MaxUploadBytes: cfg.MaxUploadMB << 20}).Routes(mux)
	web.New(st, mgr, log, cfg.SecretKey).Routes(mux)

	srv := &http.Server{Addr: cfg.Addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Info("listening", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "err", err)
		os.Exit(1)
	}
	mgr.Wait()
}

// addUser: beenthere adduser EMAIL [--admin]; password from BT_PASSWORD or generated and printed once.
func addUser(log *slog.Logger) {
	var email string
	admin := false
	for _, a := range os.Args[2:] {
		if a == "--admin" {
			admin = true
		} else {
			email = a
		}
	}
	if email == "" {
		fmt.Fprintln(os.Stderr, "usage: beenthere adduser EMAIL [--admin]   (password from BT_PASSWORD, else generated)")
		os.Exit(2)
	}
	cfg, err := config.LoadDB()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DatabaseURL, 2)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer st.Close()
	if _, err := migrate.Apply(ctx, st.Pool); err != nil {
		log.Error("schema", "err", err)
		os.Exit(1)
	}
	pw, generated := os.Getenv("BT_PASSWORD"), false
	if pw == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		pw, generated = hex.EncodeToString(b), true
	}
	key, err := st.CreateUser(ctx, email, pw, admin)
	if err != nil {
		log.Error("create user", "err", err)
		os.Exit(1)
	}
	fmt.Printf("created %s\napi key: %s\n", email, key)
	if generated {
		fmt.Printf("password: %s\n", pw)
	}
}
