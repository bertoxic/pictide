package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/database"
	"pictide.local/studio/internal/httpapi"
	"pictide.local/studio/internal/platform"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	if err := useProjectRootFromCmd(); err != nil {
		log.Fatal(err)
	}
	c, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if c.PaymentMode != "paystack" || c.PaystackSecret == "" {
		log.Fatal("Go server requires LUMEN_PAYMENT_MODE=paystack and PAYSTACK_SECRET_KEY")
	}
	db, err := database.Open(c.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	api, err := httpapi.New(db, c)
	if err != nil {
		log.Fatal(err)
	}
	if !api.HasPassword() {
		log.Fatal("Set LUMEN_ADMIN_PASSWORD before starting the studio")
	}
	if len(c.SigningKey) < 32 {
		log.Fatal("Set LUMEN_SIGNING_KEY to at least 32 characters")
	}
	multi, err := platform.New(c, db, api)
	if err != nil {
		log.Fatal(err)
	}
	defer multi.Close()
	server := &http.Server{Addr: fmt.Sprintf("%s:%s", c.Host, c.Port), Handler: multi, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 3 * time.Minute, WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Pictide Go server listening on %s", server.Addr)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// Running `go run ./pictide` from cmd should use the same files as running
// `go run ./cmd/pictide` from the project root.
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
	if err := os.Chdir(root); err != nil {
		return fmt.Errorf("change to project directory: %w", err)
	}
	return nil
}
