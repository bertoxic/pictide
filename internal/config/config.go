package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Host, Port, DataDir, MediaDir, SigningKey, AdminPassword, PublicURL, PaymentMode, PaystackSecret string
	FeeBPS                                                                                           int
	SMTPHost, SMTPPort, SMTPUser, SMTPPassword, SMTPFrom                                             string
	R2AccountID, R2AccessKeyID, R2SecretAccessKey, R2BucketName, R2Prefix                            string
	ImgBBKey                                                                                         string
	ImgBBPreviews                                                                                    bool
	MongoDBURI, MongoDBDatabase                                                                      string
}

func Load() (Config, error) {
	envFile := os.Getenv("LUMEN_ENV_FILE")
	if envFile == "" {
		envFile = ".env"
	}
	if err := godotenv.Load(envFile); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("load environment: %w", err)
	}
	get := func(key, fallback string) string {
		if value := os.Getenv(key); value != "" {
			return value
		}
		return fallback
	}
	c := Config{ImgBBKey: os.Getenv("IMGBB_API_KEY"), SMTPHost: os.Getenv("LUMEN_SMTP_HOST"), SMTPPort: get("LUMEN_SMTP_PORT", "587"), SMTPUser: os.Getenv("LUMEN_SMTP_USER"), SMTPPassword: os.Getenv("LUMEN_SMTP_PASSWORD"), SMTPFrom: os.Getenv("LUMEN_SMTP_FROM"), Host: get("LUMEN_HOST", "127.0.0.1"), Port: get("PORT", "3000"), DataDir: get("LUMEN_DATA_DIR", "data"), SigningKey: os.Getenv("LUMEN_SIGNING_KEY"), AdminPassword: os.Getenv("LUMEN_ADMIN_PASSWORD"), PublicURL: strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"), PaymentMode: get("LUMEN_PAYMENT_MODE", "paystack"), PaystackSecret: os.Getenv("PAYSTACK_SECRET_KEY")}
	c.MongoDBURI = os.Getenv("MONGODB_URI")
	c.MongoDBDatabase = get("MONGODB_DATABASE", "pictide")
	c.R2AccountID, c.R2AccessKeyID, c.R2SecretAccessKey, c.R2BucketName = os.Getenv("R2_ACCOUNT_ID"), os.Getenv("R2_ACCESS_KEY_ID"), os.Getenv("R2_SECRET_ACCESS_KEY"), os.Getenv("R2_BUCKET_NAME")
	if (c.R2AccountID != "" || c.R2AccessKeyID != "" || c.R2SecretAccessKey != "" || c.R2BucketName != "") && (c.R2AccountID == "" || c.R2AccessKeyID == "" || c.R2SecretAccessKey == "" || c.R2BucketName == "") {
		return Config{}, fmt.Errorf("all R2 credentials and bucket name are required")
	}
	var enabledErr error
	c.ImgBBPreviews, enabledErr = strconv.ParseBool(get("LUMEN_IMGBB_PREVIEWS", "false"))
	if enabledErr != nil {
		return Config{}, fmt.Errorf("LUMEN_IMGBB_PREVIEWS must be true or false")
	}
	if c.ImgBBPreviews && c.ImgBBKey == "" {
		return Config{}, fmt.Errorf("IMGBB_API_KEY is required when ImgBB previews are enabled")
	}
	c.MediaDir = get("LUMEN_MEDIA_DIR", filepath.Join(c.DataDir, "media"))
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return Config{}, fmt.Errorf("PUBLIC_URL must be an HTTPS origin")
		}
	}
	if c.PaymentMode != "paystack" {
		return Config{}, fmt.Errorf("invalid LUMEN_PAYMENT_MODE")
	}
	if c.PaymentMode == "paystack" && c.PaystackSecret == "" {
		return Config{}, fmt.Errorf("PAYSTACK_SECRET_KEY is required in paystack mode")
	}
	fee, err := strconv.Atoi(get("LUMEN_PLATFORM_FEE_BPS", "0"))
	if err != nil || fee < 0 || fee > 10000 {
		return Config{}, fmt.Errorf("invalid LUMEN_PLATFORM_FEE_BPS")
	}
	c.FeeBPS = fee
	if os.Getenv("NODE_ENV") == "production" && (c.AdminPassword == "" || c.SigningKey == "" || !strings.HasPrefix(c.PublicURL, "https://")) {
		return Config{}, fmt.Errorf("production requires LUMEN_ADMIN_PASSWORD, LUMEN_SIGNING_KEY and an HTTPS PUBLIC_URL")
	}
	if c.Host != "127.0.0.1" && c.Host != "localhost" && (c.AdminPassword == "" || len(c.SigningKey) < 32 || !strings.HasPrefix(c.PublicURL, "https://")) {
		return Config{}, fmt.Errorf("non-local listening requires LUMEN_ADMIN_PASSWORD, a signing key of at least 32 characters, and an HTTPS PUBLIC_URL")
	}
	return c, nil
}
