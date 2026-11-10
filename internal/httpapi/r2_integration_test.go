package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/joho/godotenv"
	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/mongostore"
	"pictide.local/studio/internal/security"
)

// The opt-in integration test creates a synthetic image and only deletes its
// own randomly named MongoDB database and R2 objects.
func TestR2DirectUpload(t *testing.T) {
	if os.Getenv("PICTIDE_TEST_R2") != "1" {
		t.Skip("set PICTIDE_TEST_R2=1 to run the R2 integration test")
	}
	_ = godotenv.Load("../../.env")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client, err := mongostore.Connect(ctx, os.Getenv("MONGODB_URI"))
	if err != nil {
		t.Fatal("MongoDB connection failed")
	}
	defer client.Disconnect(context.Background())
	dbID, err := security.Token()
	if err != nil {
		t.Fatal("unable to create test ID")
	}
	db := client.Database("pictide_r2_test_" + dbID[:16])
	defer db.Drop(context.Background())
	cfg := config.Config{SigningKey: strings.Repeat("k", 32), AdminPassword: "demo-password-123", R2AccountID: os.Getenv("R2_ACCOUNT_ID"), R2AccessKeyID: os.Getenv("R2_ACCESS_KEY_ID"), R2SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"), R2BucketName: os.Getenv("R2_BUCKET_NAME")}
	server, err := New(ctx, db, cfg)
	if err != nil || server.r2 == nil {
		t.Fatal("unable to initialize R2 integration")
	}
	var cookie *http.Cookie
	request := func(method, url string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, bytes.NewReader(body))
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	w := request("POST", "/api/login", []byte(`{"username":"admin","password":"demo-password-123"}`))
	if w.Code != 200 {
		t.Fatalf("login failed: %d", w.Code)
	}
	cookie = w.Result().Cookies()[0]
	w = request("POST", "/api/galleries", []byte(`{"name":"R2 demo","price":0}`))
	if w.Code != 201 {
		t.Fatalf("gallery creation failed: %d", w.Code)
	}
	var gallery struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(w.Body.Bytes(), &gallery) != nil || gallery.ID == "" {
		t.Fatal("gallery response invalid")
	}
	img := image.NewRGBA(image.Rect(0, 0, 1400, 1400))
	var random uint32 = 1
	for i := 0; i < len(img.Pix); i += 4 {
		random ^= random << 13
		random ^= random >> 17
		random ^= random << 5
		img.Pix[i] = byte(random)
		img.Pix[i+1] = byte(random >> 8)
		img.Pix[i+2] = byte(random >> 16)
		img.Pix[i+3] = 255
	}
	var body bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if encoder.Encode(&body, img) != nil || body.Len() <= 4_500_000 {
		t.Fatalf("synthetic image should exceed Vercel's limit, got %d bytes", body.Len())
	}
	metadata, _ := json.Marshal(map[string]any{"name": "synthetic.png", "size": body.Len()})
	w = request("POST", "/api/galleries/"+gallery.ID+"/direct-upload", metadata)
	if w.Code != 200 {
		t.Fatalf("start direct upload failed: %d %s", w.Code, w.Body.String())
	}
	var upload struct {
		ID          string `json:"upload_id"`
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
	}
	if json.Unmarshal(w.Body.Bytes(), &upload) != nil || upload.ID == "" || upload.URL == "" {
		t.Fatal("invalid direct-upload response")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = server.r2.delete(cleanup, r2TemporaryKey("legacy", upload.ID))
		_ = server.r2.delete(cleanup, r2PhotoKey("legacy", upload.ID, "original"))
		_ = server.r2.delete(cleanup, r2PhotoKey("legacy", upload.ID, "master"))
	}()
	put, err := http.NewRequestWithContext(ctx, "PUT", upload.URL, bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatal("could not construct direct upload")
	}
	put.Header.Set("Content-Type", upload.ContentType)
	response, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatal("direct PUT to R2 failed")
	}
	io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("R2 PUT failed with status %d", response.StatusCode)
	}
	w = request("POST", "/api/direct-uploads/"+upload.ID+"/complete", nil)
	if w.Code != 200 {
		t.Fatalf("completion failed: %d %s", w.Code, w.Body.String())
	}
	w = request("PATCH", "/api/galleries/"+gallery.ID, []byte(`{"status":"Published"}`))
	if w.Code != 200 {
		t.Fatalf("publication failed: %d", w.Code)
	}
	guest := httptest.NewRecorder()
	server.ServeHTTP(guest, httptest.NewRequest("GET", "/api/photos/"+upload.ID+"/free", nil))
	if guest.Code != 302 {
		t.Fatalf("free download did not redirect: %d", guest.Code)
	}
	link := guest.Header().Get("Location")
	get, err := http.NewRequestWithContext(ctx, "GET", link, nil)
	if err != nil {
		t.Fatal("invalid download URL")
	}
	response, err = http.DefaultClient.Do(get)
	if err != nil {
		t.Fatal("direct GET from R2 failed")
	}
	received, err := io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || received <= 4_500_000 {
		t.Fatalf("direct download failed or was too small: status %d size %d", response.StatusCode, received)
	}
	archiveResponse := httptest.NewRecorder()
	server.ServeHTTP(archiveResponse, httptest.NewRequest("GET", "/api/galleries/"+gallery.ID+"/free-download", nil))
	if archiveResponse.Code != 302 {
		t.Fatalf("archive did not redirect: %d", archiveResponse.Code)
	}
	archiveURL, err := url.Parse(archiveResponse.Header().Get("Location"))
	if err != nil {
		t.Fatal("invalid archive URL")
	}
	marker := "/pictide/temporary/archives/"
	position := strings.Index(archiveURL.Path, marker)
	if position < 0 {
		t.Fatal("unexpected archive location")
	}
	archiveKey := strings.TrimPrefix(archiveURL.Path[position:], "/")
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = server.r2.delete(cleanup, archiveKey)
	}()
	get, err = http.NewRequestWithContext(ctx, "GET", archiveResponse.Header().Get("Location"), nil)
	if err != nil {
		t.Fatal("invalid archive download")
	}
	response, err = http.DefaultClient.Do(get)
	if err != nil {
		t.Fatal("archive GET failed")
	}
	archiveBytes, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || len(archiveBytes) <= 4_500_000 {
		t.Fatalf("archive download failed or was too small: status %d size %d", response.StatusCode, len(archiveBytes))
	}
	archive, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil || len(archive.File) != 1 {
		t.Fatal("archive contents invalid")
	}
	// Existing GridFS masters are copied to R2 on their first download.
	var small bytes.Buffer
	if png.Encode(&small, image.NewRGBA(image.Rect(0, 0, 20, 20))) != nil {
		t.Fatal("unable to create legacy image")
	}
	var multipartBody bytes.Buffer
	form := multipart.NewWriter(&multipartBody)
	part, err := form.CreateFormFile("photos", "old.png")
	if err != nil {
		t.Fatal("unable to create legacy upload")
	}
	_, _ = part.Write(small.Bytes())
	_ = form.Close()
	oldRequest := httptest.NewRequest("POST", "/api/galleries/"+gallery.ID+"/photos", &multipartBody)
	oldRequest.Header.Set("Content-Type", form.FormDataContentType())
	oldRequest.AddCookie(cookie)
	oldResponse := httptest.NewRecorder()
	server.ServeHTTP(oldResponse, oldRequest)
	if oldResponse.Code != 200 || !strings.Contains(oldResponse.Body.String(), `"added":1`) {
		t.Fatalf("legacy upload failed: %d", oldResponse.Code)
	}
	w = request("GET", "/api/galleries/"+gallery.ID, nil)
	var detail struct {
		Photos []struct{ ID, Name string } `json:"photos"`
	}
	if json.Unmarshal(w.Body.Bytes(), &detail) != nil {
		t.Fatal("invalid gallery detail")
	}
	oldID := ""
	for _, p := range detail.Photos {
		if p.Name == "old.png" {
			oldID = p.ID
		}
	}
	if oldID == "" {
		t.Fatal("legacy photo not found")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = server.r2.delete(cleanup, r2PhotoKey("legacy", oldID, "master"))
	}()
	legacyDownload := httptest.NewRecorder()
	server.ServeHTTP(legacyDownload, httptest.NewRequest("GET", "/api/photos/"+oldID+"/free", nil))
	if legacyDownload.Code != 302 {
		t.Fatalf("legacy download did not redirect: %d", legacyDownload.Code)
	}
	preflight, err := http.NewRequestWithContext(ctx, "OPTIONS", upload.URL, nil)
	if err != nil {
		t.Fatal("invalid preflight URL")
	}
	preflight.Header.Set("Origin", "http://127.0.0.1:8080")
	preflight.Header.Set("Access-Control-Request-Method", "PUT")
	preflight.Header.Set("Access-Control-Request-Headers", "content-type")
	response, err = http.DefaultClient.Do(preflight)
	if err == nil {
		io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 || response.Header.Get("Access-Control-Allow-Origin") == "" {
			t.Log("R2 bucket does not permit browser PUT from http://127.0.0.1:8080; configure CORS for the actual app origin")
		} else {
			t.Log("R2 bucket permits browser PUT from http://127.0.0.1:8080")
		}
	}
	cors, err := server.r2.client.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(server.r2.bucket)})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchCORSConfiguration" {
			t.Log("R2 bucket has no CORS policy")
		} else {
			t.Log("R2 CORS policy could not be inspected")
		}
	} else {
		vercelOrigin := false
		for _, rule := range cors.CORSRules {
			for _, origin := range rule.AllowedOrigins {
				if strings.HasSuffix(origin, ".vercel.app") || origin == "*" {
					vercelOrigin = true
				}
			}
		}
		t.Logf("R2 CORS policy has %d rules; an allowed Vercel origin or wildcard is present: %t", len(cors.CORSRules), vercelOrigin)
	}
}

func TestR2CORSStatus(t *testing.T) {
	if os.Getenv("PICTIDE_TEST_R2") != "1" {
		t.Skip("set PICTIDE_TEST_R2=1 to inspect R2 CORS")
	}
	_ = godotenv.Load("../../.env")
	store, err := newR2Storage(config.Config{R2AccountID: os.Getenv("R2_ACCOUNT_ID"), R2AccessKeyID: os.Getenv("R2_ACCESS_KEY_ID"), R2SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"), R2BucketName: os.Getenv("R2_BUCKET_NAME")})
	if err != nil || store == nil {
		t.Fatal("unable to initialize R2")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err = store.client.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(store.bucket)})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			t.Logf("R2 CORS status: %s", apiErr.ErrorCode())
		} else {
			t.Log("R2 CORS status could not be inspected")
		}
		return
	}
	t.Log("R2 CORS policy is readable")
}
