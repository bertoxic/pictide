package app

import (
	"context"
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
	"pictide.local/studio/internal/httpapi"
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
	var handler *httpapi.Server
	if err == nil {
		handler, err = httpapi.New(ctx, client.Database(c.MongoDBDatabase), c)
	}
	cancel()
	if err != nil {
		return fmt.Errorf("application initialization failed: %w", err)
	}
	defer handler.Close()
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
	log.Printf("Pictide server listening on %s; some features are still being converted", server.Addr)
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
