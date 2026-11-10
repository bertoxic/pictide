package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/mongostore"
)

// Run starts the application for both local and hosted entrypoints.
func Run() error {
	if err := useProjectRootFromCmd(); err != nil {
		return err
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	client, err := mongostore.Connect(ctx, c.MongoDBURI)
	if err != nil {
		cancel()
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Disconnect(ctx)
	}()
	galleries, err := mongostore.NewGalleries(client.Database(c.MongoDBDatabase), "legacy")
	if err == nil {
		err = galleries.EnsureIndexes(ctx)
	}
	cancel()
	if err != nil {
		return errors.New("MongoDB index initialization failed")
	}

	// The former application handlers have not yet been replaced. Return an
	// explicit unavailable response instead of presenting a working application.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Application features are unavailable during the MongoDB conversion."})
	})
	host := c.Host
	if os.Getenv("VERCEL") == "1" {
		host = "0.0.0.0"
	}
	server := &http.Server{Addr: fmt.Sprintf("%s:%s", host, c.Port), Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 3 * time.Minute, WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Pictide conversion service listening on %s; application features unavailable", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Running from cmd uses the same project-relative settings as running from root.
func useProjectRootFromCmd() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	if filepath.Base(cwd) != "cmd" {
		return nil
	}
	root := filepath.Dir(cwd)
	for _, marker := range []string{"go.mod", filepath.Join("cmd", "pictide", "main.go"), filepath.Join("public", "index.html")} {
		if info, err := os.Stat(filepath.Join(root, marker)); err != nil || info.IsDir() {
			return nil
		}
	}
	return os.Chdir(root)
}
