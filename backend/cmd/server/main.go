package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"eduforge/backend/internal/api"
	"eduforge/backend/internal/generate"
	"eduforge/backend/internal/job"
	"eduforge/backend/internal/llm"
	"eduforge/backend/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	path := os.Getenv("EDUFORGE_DB")
	if path == "" {
		path = "data/eduforge.sqlite"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	db, err := store.Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close SQLite: %w", closeErr))
		}
	}()
	if err := db.Migrate(ctx); err != nil {
		return err
	}
	if len(os.Args) == 2 && os.Args[1] == "migrate" {
		return nil
	}
	if err := db.RecoverInterrupted(ctx); err != nil {
		return err
	}
	limit := 30
	if value := os.Getenv("EDUFORGE_DAILY_GENERATIONS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return errors.New("EDUFORGE_DAILY_GENERATIONS must be a positive integer")
		}
		limit = parsed
	}
	provider, err := llm.NewConnections(
		os.Getenv("EDUFORGE_LLM_ENDPOINT"),
		os.Getenv("EDUFORGE_LLM_MODEL"),
		os.Getenv("EDUFORGE_LLM_API_KEY"),
		&llm.Codex{}, api.ModelPreferences(db),
	)
	if err != nil {
		return fmt.Errorf("configure LLM: %w", err)
	}
	// Reconnect to the ChatGPT sign-in Codex CLI kept in CODEX_HOME, without delaying startup.
	go func() {
		restoreCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := provider.Restore(restoreCtx); err != nil {
			slog.Warn("restore ChatGPT connection", "error", err)
		}
	}()
	connections := api.NewModelConnections(provider, db, limit)
	generation := generate.New(db, provider, limit)
	if err := db.RecoverGenerationJobs(ctx); err != nil {
		return fmt.Errorf("recover generation queue: %w", err)
	}
	queue := job.New(db, generation)
	queueCtx, stopQueue := context.WithCancel(ctx)
	queueDone := make(chan struct{})
	go func() {
		defer close(queueDone)
		queue.Run(queueCtx)
	}()
	defer func() {
		stopQueue()
		<-queueDone
	}()
	documents := api.NewDocuments(db, generation, queue)

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           api.NewHandler(documents, connections),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      70 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("starting HTTP server", "address", addr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			closeErr := server.Close()
			return fmt.Errorf("shut down HTTP server: %w", errors.Join(err, closeErr))
		}
		return nil
	}
}
