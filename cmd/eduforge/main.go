package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"eduforge/internal/app"
	"eduforge/internal/artifacts"
	"eduforge/internal/httpapi"
	"eduforge/internal/mathtex"
	"eduforge/internal/store"
)

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: eduforge serve | healthcheck")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "healthcheck":
		flags := flag.NewFlagSet("healthcheck", flag.ExitOnError)
		url := flags.String("url", "http://127.0.0.1:8080/api/health", "Health endpoint")
		_ = flags.Parse(os.Args[2:])
		client := http.Client{Timeout: 4 * time.Second}
		response, err := client.Get(*url)
		if err != nil {
			os.Exit(1)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "Unknown command")
		os.Exit(2)
	}
}
func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:8080", "Listen address")
	data := flags.String("data", env("EDUFORGE_DATA", ".eduforge"), "Data directory")
	secrets := flags.String("secrets", env("EDUFORGE_SECRETS", ".eduforge-secrets"), "Separate credential directory")
	web := flags.String("web", env("EDUFORGE_WEB", "frontend/dist"), "Built Vue application")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := os.MkdirAll(*data, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(*data, "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another_server_owns_data_directory")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	s, err := store.Open(*data)
	if err != nil {
		return err
	}
	defer s.Close()
	application, err := app.New(s, *secrets)
	if err != nil {
		return err
	}
	// Formulas render with MathJax under Node.js; without it they stay TeX.
	artifacts.Math = mathtex.New(env("EDUFORGE_MATH", "/app/math/render.mjs"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	application.Start(ctx)
	defer application.Stop()
	server := httpapi.HTTPServer(httpapi.New(application, *web, httpapi.DefaultOrigins(*listen)), *listen)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("EduForge listening on http://%s\n", *listen)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
