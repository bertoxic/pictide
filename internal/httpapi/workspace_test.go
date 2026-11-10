package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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
	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/mongostore"
	"pictide.local/studio/internal/security"
)

// This opt-in test uses a randomly named temporary database, never the
// configured application database.
func TestMongoWorkspace(t *testing.T) {
	if os.Getenv("PICTIDE_TEST_MONGODB") != "1" {
		t.Skip("set PICTIDE_TEST_MONGODB=1 to run the MongoDB workspace test")
	}
	_ = godotenv.Load("../../.env")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongostore.Connect(ctx, os.Getenv("MONGODB_URI"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	id, err := security.Token()
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database("pictide_test_" + id[:16])
	defer db.Drop(context.Background())
	server, err := New(ctx, db, config.Config{SigningKey: strings.Repeat("k", 32), AdminPassword: "demo-password-123"})
	if err != nil {
		t.Fatal(err)
	}
	var session *http.Cookie
	request := func(method, path string, body []byte, contentType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		if session != nil {
			r.AddCookie(session)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	static := request("GET", "/", nil, "")
	if static.Code != 200 || !strings.Contains(static.Body.String(), "Pictide") {
		t.Fatalf("static application: %d", static.Code)
	}
	w := request("POST", "/api/login", []byte(`{"username":"admin","password":"demo-password-123"}`), "application/json")
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	session = w.Result().Cookies()[0]
	w = request("PUT", "/api/settings", []byte(`{"studio_name":"Demo","contact_email":"","default_price":"25"}`), "application/json")
	if w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body.String())
	}
	w = request("POST", "/api/galleries", []byte(`{"name":"Demo gallery"}`), "application/json")
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var gallery struct {
		ID    string `json:"id"`
		Price int    `json:"price"`
	}
	if json.Unmarshal(w.Body.Bytes(), &gallery) != nil || gallery.ID == "" || gallery.Price != 2500 {
		t.Fatalf("unexpected gallery: %s", w.Body.String())
	}
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var imageBytes bytes.Buffer
	if err = png.Encode(&imageBytes, img); err != nil {
		t.Fatal(err)
	}
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	part, err := writer.CreateFormFile("photos", "demo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(imageBytes.Bytes())
	_ = writer.Close()
	w = request("POST", "/api/galleries/"+gallery.ID+"/photos", form.Bytes(), writer.FormDataContentType())
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"added":1`) {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	w = request("GET", "/api/galleries/"+gallery.ID, nil, "")
	var detail struct {
		Photos []struct {
			ID string `json:"id"`
		} `json:"photos"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || len(detail.Photos) != 1 {
		t.Fatalf("gallery detail: %d %s", w.Code, w.Body.String())
	}
	w = request("PATCH", "/api/photos/"+detail.Photos[0].ID, []byte(`{"price":0}`), "application/json")
	if w.Code != 200 {
		t.Fatalf("set free photo: %d %s", w.Code, w.Body.String())
	}
	w = request("GET", "/media/"+detail.Photos[0].ID, nil, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("preview: %d", w.Code)
	}
	w = request("GET", "/api/galleries?limit=30", nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	w = request("PATCH", "/api/galleries/"+gallery.ID+"/customization", []byte(`{"share_title":"Our day","share_color":"#405020"}`), "application/json")
	if w.Code != 200 {
		t.Fatalf("customize: %d %s", w.Code, w.Body.String())
	}
	w = request("PATCH", "/api/galleries/"+gallery.ID, []byte(`{"status":"Published"}`), "application/json")
	if w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	public := httptest.NewRecorder()
	server.ServeHTTP(public, httptest.NewRequest("GET", "/api/galleries/"+gallery.ID, nil))
	if public.Code != 200 || !strings.Contains(public.Body.String(), `"share_title":"Our day"`) {
		t.Fatalf("public gallery: %d %s", public.Code, public.Body.String())
	}
	public = httptest.NewRecorder()
	server.ServeHTTP(public, httptest.NewRequest("GET", "/api/photos/"+detail.Photos[0].ID+"/clean?size=thumb", nil))
	if public.Code != 200 || public.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("free photo preview: %d", public.Code)
	}
	w = request("GET", "/api/galleries/"+gallery.ID+"/qr?format=png", nil, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("QR: %d", w.Code)
	}
}
