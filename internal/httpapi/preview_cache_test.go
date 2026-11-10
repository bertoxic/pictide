package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakePreviewStore struct {
	mu                 sync.Mutex
	data               []byte
	uploads, downloads int
	fail               bool
}

func (f *fakePreviewStore) Upload(_ context.Context, b []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads++
	if f.fail {
		return "", errors.New("offline")
	}
	f.data = append([]byte{}, b...)
	return "https://i.ibb.co/example/image.jpg", nil
}
func (f *fakePreviewStore) Download(_ context.Context, _ string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads++
	if f.fail {
		return nil, errors.New("offline")
	}
	return append([]byte{}, f.data...), nil
}
func (f *fakePreviewStore) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uploads, f.downloads
}
func TestPublishedPreviewImgBBFallbackAndPrivacy(t *testing.T) {
	s := makeOrderFixture(t, 0)
	store := &fakePreviewStore{}
	s.previewStore = store
	id := "de1e4b13-5047-4a83-956d-420383c5e61d"
	_, e := s.DB.Exec("INSERT INTO photos(id,gallery_id,name,hash,created) VALUES(?,'g1','photo','hash','2026-10-02T12:00:00Z')", id)
	if e != nil {
		t.Fatal(e)
	}
	local := []byte{0xff, 0xd8, 0xff, 0xd9}
	if e = os.WriteFile(filepath.Join(s.Config.MediaDir, id+".jpg"), local, 0600); e != nil {
		t.Fatal(e)
	}
	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/media/"+id, nil)
		s.Router().ServeHTTP(w, r)
		return w
	}
	first := get()
	if first.Code != 200 || !bytes.Equal(first.Body.Bytes(), local) {
		t.Fatal("local first response failed")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var n int
		if s.DB.QueryRow("SELECT count(*) FROM imgbb_previews WHERE photo_id=? AND url<>''", id).Scan(&n) == nil && n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("preview not cached")
		}
		time.Sleep(10 * time.Millisecond)
	}
	second := get()
	_, downloads := store.counts()
	if second.Code != 200 || !bytes.Equal(second.Body.Bytes(), local) || downloads != 1 {
		t.Fatal("cached preview not fetched")
	}
	store.mu.Lock()
	store.fail = true
	store.mu.Unlock()
	if w := get(); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), local) {
		t.Fatal("local fallback failed")
	}
	_, e = s.DB.Exec("UPDATE galleries SET status='Draft' WHERE id='g1'")
	if e != nil {
		t.Fatal(e)
	}
	before, _ := store.counts()
	ownerRequest := httptest.NewRequest("GET", "/media/"+id, nil)
	ownerRequest.RemoteAddr = "127.0.0.1:12345"
	ownerResponse := httptest.NewRecorder()
	s.Router().ServeHTTP(ownerResponse, ownerRequest)
	if ownerResponse.Code != 200 || !bytes.Equal(ownerResponse.Body.Bytes(), local) {
		t.Fatal("owner should see draft locally")
	}
	if w := get(); w.Code != 404 {
		t.Fatal("draft proof exposed")
	}
	time.Sleep(30 * time.Millisecond)
	after, _ := store.counts()
	if after != before {
		t.Fatal("draft preview uploaded")
	}
}

type blockingPreviewStore struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingPreviewStore) Upload(_ context.Context, _ []byte) (string, error) {
	b.entered <- struct{}{}
	<-b.release
	return "https://i.ibb.co/example/image.jpg", nil
}
func (b *blockingPreviewStore) Download(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("unused")
}
func TestPublishedPreviewsQueueBeyondWorkerLimit(t *testing.T) {
	s := makeOrderFixture(t, 0)
	store := &blockingPreviewStore{entered: make(chan struct{}, 3), release: make(chan struct{})}
	s.previewStore = store
	ids := []string{"f52e909d-d8fb-4377-8302-5911f35a3b11", "9b328aa1-e695-475e-97a3-c6b853a7ac64", "75633c22-ce8c-43ee-9a39-a752f0c5a35f"}
	for _, id := range ids {
		_, e := s.DB.Exec("INSERT INTO photos(id,gallery_id,name,hash,created) VALUES(?,'g1','photo',?,'2026-10-02T12:00:00Z')", id, id)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(s.Config.MediaDir, id+".jpg"), []byte{0xff, 0xd8, 0xff, 0xd9}, 0600); e != nil {
			t.Fatal(e)
		}
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, httptest.NewRequest("GET", "/media/"+id, nil))
		if w.Code != 200 {
			t.Fatal("local preview failed")
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-store.entered:
		case <-time.After(2 * time.Second):
			close(store.release)
			t.Fatal("upload worker did not start")
		}
	}
	select {
	case <-store.entered:
		close(store.release)
		t.Fatal("worker limit exceeded")
	default:
	}
	close(store.release)
	select {
	case <-store.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("queued upload did not start")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var n int
		s.DB.QueryRow("SELECT count(*) FROM imgbb_previews WHERE url<>''").Scan(&n)
		if n == 3 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d previews saved", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
