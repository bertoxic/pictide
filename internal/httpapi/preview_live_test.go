package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/imgbb"
)

type countedLiveStore struct {
	client    *imgbb.Client
	mu        sync.Mutex
	downloads int
	last      error
}

func (s *countedLiveStore) Upload(ctx context.Context, b []byte) (string, error) {
	return s.client.Upload(ctx, b)
}
func (s *countedLiveStore) Download(ctx context.Context, u string) ([]byte, error) {
	b, e := s.client.Download(ctx, u)
	s.mu.Lock()
	s.downloads++
	s.last = e
	s.mu.Unlock()
	return b, e
}
func TestLiveImgBBPublishedPreview(t *testing.T) {
	if os.Getenv("LUMEN_TEST_IMGBB") != "1" {
		t.Skip("live ImgBB test is opt-in")
	}
	cfg, e := config.Load()
	if e != nil {
		t.Fatal(e)
	}
	if cfg.ImgBBKey == "" {
		t.Fatal("ImgBB key missing")
	}
	s := makeOrderFixture(t, 0)
	client := imgbb.New(cfg.ImgBBKey)
	client.Expiration = time.Minute
	store := &countedLiveStore{client: client}
	s.previewStore = store
	id := "2184d0a3-6956-4856-84d4-96d327d07213"
	_, e = s.DB.Exec("INSERT INTO photos(id,gallery_id,name,hash,created) VALUES(?,'g1','synthetic','hash','2026-01-01T00:00:00Z')", id)
	if e != nil {
		t.Fatal(e)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 24, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			picture.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 80, A: 255})
		}
	}
	var imageData bytes.Buffer
	if e = jpeg.Encode(&imageData, picture, nil); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(s.Config.MediaDir, id+".jpg"), imageData.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, httptest.NewRequest("GET", "/media/"+id, nil))
		return w
	}
	if w := get(); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), imageData.Bytes()) {
		t.Fatal("local preview failed")
	}
	deadline := time.Now().Add(35 * time.Second)
	for {
		var n int
		if s.DB.QueryRow("SELECT count(*) FROM imgbb_previews WHERE photo_id=? AND url<>''", id).Scan(&n) == nil && n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ImgBB synthetic preview was not uploaded")
		}
		time.Sleep(100 * time.Millisecond)
	}
	w := get()
	store.mu.Lock()
	count, last := store.downloads, store.last
	store.mu.Unlock()
	if w.Code != 200 || count != 1 || last != nil || !bytes.Equal(w.Body.Bytes(), imageData.Bytes()) {
		t.Fatalf("ImgBB preview retrieval failed: status=%d downloads=%d err=%v", w.Code, count, last)
	}
}
