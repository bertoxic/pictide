package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvAndRejectUnsafeMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte("LUMEN_PAYMENT_MODE=paystack\nPAYSTACK_SECRET_KEY=test-key\nLUMEN_HOST=127.0.0.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUMEN_ENV_FILE", p)
	t.Setenv("LUMEN_PAYMENT_MODE", "")
	os.Unsetenv("LUMEN_PAYMENT_MODE")
	t.Setenv("PAYSTACK_SECRET_KEY", "")
	os.Unsetenv("PAYSTACK_SECRET_KEY")
	c, err := Load()
	if err != nil || c.PaymentMode != "paystack" {
		t.Fatalf("config: %+v %v", c, err)
	}
	t.Setenv("LUMEN_PAYMENT_MODE", "paystack")
	t.Setenv("PAYSTACK_SECRET_KEY", "")
	if _, err = Load(); err == nil {
		t.Fatal("paystack mode should require a secret")
	}
}
func TestImgBBPreviewConfiguration(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.env")
	if err := os.WriteFile(p, []byte("# empty\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUMEN_ENV_FILE", p)
	t.Setenv("LUMEN_PAYMENT_MODE", "paystack")
	t.Setenv("PAYSTACK_SECRET_KEY", "test")
	t.Setenv("LUMEN_HOST", "127.0.0.1")
	t.Setenv("LUMEN_IMGBB_PREVIEWS", "true")
	t.Setenv("IMGBB_API_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("preview mode must require an ImgBB key")
	}
	t.Setenv("IMGBB_API_KEY", "synthetic")
	c, err := Load()
	if err != nil || !c.ImgBBPreviews || c.ImgBBKey != "synthetic" {
		t.Fatal("ImgBB preview config not loaded", err)
	}
}

func TestMongoDBConfiguration(t *testing.T) {
	t.Setenv("LUMEN_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	t.Setenv("LUMEN_PAYMENT_MODE", "paystack")
	t.Setenv("PAYSTACK_SECRET_KEY", "test")
	t.Setenv("LUMEN_HOST", "127.0.0.1")
	t.Setenv("MONGODB_URI", "mongodb://localhost:27017")
	t.Setenv("MONGODB_DATABASE", "")
	c, err := Load()
	if err != nil || c.MongoDBURI != "mongodb://localhost:27017" || c.MongoDBDatabase != "pictide" {
		t.Fatal("default MongoDB configuration was not loaded", err)
	}
	t.Setenv("MONGODB_DATABASE", "studio_test")
	c, err = Load()
	if err != nil || c.MongoDBDatabase != "studio_test" {
		t.Fatal("MongoDB database override was not loaded", err)
	}
}
