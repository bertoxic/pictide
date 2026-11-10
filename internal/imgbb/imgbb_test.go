package imgbb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestUploadAndDownload(t *testing.T) {
	image := []byte{0xff, 0xd8, 0xff, 0xd9}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/upload" {
			r.ParseForm()
			if r.Form.Get("key") != "secret" || r.Form.Get("expiration") != "86400" {
				t.Error("wrong credentials or expiry")
			}
			d, e := base64.StdEncoding.DecodeString(r.Form.Get("image"))
			if e != nil || string(d) != string(image) {
				t.Error("image corrupted")
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"url": server.URL + "/image"}})
			return
		}
		w.Write(image)
	}))
	defer server.Close()
	c := New("secret")
	c.endpoint = server.URL + "/upload"
	c.allowedHost = func(u *url.URL) bool { return u != nil && strings.HasPrefix(u.String(), server.URL+"/") }
	raw, e := c.Upload(context.Background(), image)
	if e != nil {
		t.Fatal(e)
	}
	got, e := c.Download(context.Background(), raw)
	if e != nil || string(got) != string(image) {
		t.Fatalf("download failed: %v", e)
	}
	if _, e = c.Download(context.Background(), "http://127.0.0.1/secret"); e == nil {
		t.Fatal("untrusted URL accepted")
	}
}
func TestRejectsUntrustedUploadURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"url": "http://127.0.0.1/private"}})
	}))
	defer server.Close()
	c := New("secret")
	c.endpoint = server.URL
	if _, e := c.Upload(context.Background(), []byte{0xff, 0xd8, 0xff}); e == nil {
		t.Fatal("untrusted URL accepted")
	}
}

func TestNonExpiringPNG(t *testing.T) {
	image := []byte("\x89PNG\r\n\x1a\nexample")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/upload" {
			_ = r.ParseForm()
			if r.Form.Has("expiration") {
				t.Error("unexpected expiration")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"url": server.URL + "/image"}})
			return
		}
		_, _ = w.Write(image)
	}))
	defer server.Close()
	c := New("secret")
	c.Expiration = 0
	c.endpoint = server.URL + "/upload"
	c.allowedHost = func(u *url.URL) bool { return u != nil && strings.HasPrefix(u.String(), server.URL+"/") }
	raw, err := c.Upload(context.Background(), image)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Download(context.Background(), raw)
	if err != nil || string(got) != string(image) {
		t.Fatalf("download failed: %v", err)
	}
}
