package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/mongostore"
	"pictide.local/studio/internal/security"
)

type e2eEnv struct {
	server  *Server
	db      *mongo.Database
	client  *mongo.Client
	cfg     config.Config
	ctx     context.Context
	cancel  context.CancelFunc
	dbName  string
	signing string
}

func skipIfNoMongoDB(t *testing.T) {
	t.Helper()
	if os.Getenv("PICTIDE_TEST_MONGODB") != "1" {
		t.Skip("skipping E2E test; set PICTIDE_TEST_MONGODB=1 to run tests against isolated MongoDB test database")
	}
}

func newE2EEnv(t *testing.T, prefix string) *e2eEnv {
	t.Helper()
	skipIfNoMongoDB(t)
	_ = godotenv.Load("../../.env")

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		t.Fatal("MONGODB_URI environment variable is required when PICTIDE_TEST_MONGODB=1")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	client, err := mongostore.Connect(ctx, mongoURI)
	if err != nil {
		cancel()
		t.Fatalf("mongostore.Connect: %v", err)
	}

	randToken, err := security.Token()
	if err != nil {
		cancel()
		_ = client.Disconnect(context.Background())
		t.Fatalf("security.Token: %v", err)
	}

	dbName := fmt.Sprintf("pictide_test_%s_%s", prefix, randToken[:12])
	db := client.Database(dbName)

	signingKey := strings.Repeat("s", 32)
	adminPass := "demo-admin-password-123"
	cfg := config.Config{
		SigningKey:     signingKey,
		AdminPassword:  adminPass,
		PaymentMode:    "paystack",
		PaystackSecret: "sk_test_mock_paystack_secret_key_1234567890",
		FeeBPS:         500, // 5%
		PublicURL:      "http://example.com",
		SMTPHost:       "smtp.example.com",
		SMTPPort:       "587",
		SMTPUser:       "noreply@example.com",
		SMTPPassword:   "mock-smtp-password",
		SMTPFrom:       "Pictide <noreply@example.com>",
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
		cancel()
		t.Fatalf("httpapi.New: %v", err)
	}

	env := &e2eEnv{
		server:  server,
		db:      db,
		client:  client,
		cfg:     cfg,
		ctx:     ctx,
		cancel:  cancel,
		dbName:  dbName,
		signing: signingKey,
	}

	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_ = db.Drop(dropCtx)
		_ = client.Disconnect(dropCtx)
		cancel()
	})

	return env
}

func (env *e2eEnv) request(method, path string, body []byte, contentType string, session *http.Cookie) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if session != nil {
		r.AddCookie(session)
	}
	w := httptest.NewRecorder()
	env.server.ServeHTTP(w, r)
	return w
}

func (env *e2eEnv) requestWithHeader(method, path string, body []byte, headers map[string]string, session *http.Cookie) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if session != nil {
		r.AddCookie(session)
	}
	w := httptest.NewRecorder()
	env.server.ServeHTTP(w, r)
	return w
}

type testFile struct {
	filename string
	data     []byte
}

func (env *e2eEnv) multipartRequest(method, path string, files map[string][]testFile, fields map[string]string, session *http.Cookie) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for fieldName, fileList := range files {
		for _, file := range fileList {
			part, err := writer.CreateFormFile(fieldName, file.filename)
			if err != nil {
				panic(err)
			}
			_, _ = part.Write(file.data)
		}
	}
	for k, v := range fields {
		_ = writer.WriteField(k, v)
	}
	_ = writer.Close()

	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if session != nil {
		req.AddCookie(session)
	}
	w := httptest.NewRecorder()
	env.server.ServeHTTP(w, req)
	return w
}

func (env *e2eEnv) login(t *testing.T, username, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	w := env.request("POST", "/api/login", body, "application/json", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed for %s: status %d body %s", username, w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "lumen_platform" {
			return c
		}
	}
	t.Fatalf("login succeeded but lumen_platform cookie was not set")
	return nil
}

func (env *e2eEnv) adminLogin(t *testing.T) *http.Cookie {
	t.Helper()
	return env.login(t, "admin", env.cfg.AdminPassword)
}

func (env *e2eEnv) createPhotographer(t *testing.T, username, email, password string, verified bool) (string, *http.Cookie) {
	t.Helper()
	hash, err := security.PasswordHash(password)
	if err != nil {
		t.Fatalf("PasswordHash: %v", err)
	}
	version, err := security.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	accountID, err := security.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}

	doc := bson.M{
		"_id":             accountID,
		"username":        strings.ToLower(username),
		"email":           strings.ToLower(email),
		"email_verified":  verified,
		"password_hash":   hash,
		"role":            "photographer",
		"session_version": version,
		"disabled":        false,
		"created":         time.Now().UTC().Format(time.RFC3339Nano),
	}
	_, err = env.db.Collection("accounts").InsertOne(env.ctx, doc)
	if err != nil {
		t.Fatalf("insert photographer account: %v", err)
	}

	// Login and return cookie
	cookie := env.login(t, username, password)
	return accountID, cookie
}

func (env *e2eEnv) createGallery(t *testing.T, session *http.Cookie, name string, price int) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "price": price})
	w := env.request("POST", "/api/galleries", body, "application/json", session)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("createGallery failed: status %d body %s", w.Code, w.Body.String())
	}
	var res struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.ID == "" {
		t.Fatalf("createGallery returned empty ID: %s", w.Body.String())
	}
	return res.ID
}

func createTestPNGBytes(width, height int, col color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, col)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func computePaystackSignature(secret string, body []byte) string {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
