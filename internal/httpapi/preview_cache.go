package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"time"

	"net/http"
	"pictide.local/studio/internal/imgbb"
)

type previewStore interface {
	Upload(context.Context, []byte) (string, error)
	Download(context.Context, string) ([]byte, error)
}

func previewSignature(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ImgBB only receives public-facing preview JPEGs.
func (s *Server) serveImgBBPreview(w http.ResponseWriter, r *http.Request, id, variant, file string) bool {
	local, e := s.readMedia(id + variant)
	if e != nil || len(local) == 0 || len(local) > imgbb.MaxImageBytes {
		return false
	}
	sig := previewSignature(local)
	now := time.Now().Unix()
	var storedSig, remote string
	var expires, retry int64
	err := s.DB.QueryRow("SELECT signature,url,expires,retry_after FROM imgbb_previews WHERE photo_id=? AND variant=?", id, variant).Scan(&storedSig, &remote, &expires, &retry)
	if err == nil && storedSig == sig && remote != "" && expires > now {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		data, e := s.previewStore.Download(ctx, remote)
		cancel()
		if e == nil && previewSignature(data) == sig {
			http.ServeContent(w, r, filepath.Base(file), time.Time{}, bytes.NewReader(data))
			return true
		}
		_, _ = s.DB.Exec("UPDATE imgbb_previews SET url='',expires=0,retry_after=? WHERE photo_id=? AND variant=? AND signature=? AND url=?", now+300, id, variant, sig, remote)
		return false
	}
	if err != nil || storedSig != sig || retry <= now {
		s.schedulePreviewUpload(id, variant, sig, file)
	}
	return false
}
func (s *Server) schedulePreviewUpload(id, variant, sig, file string) {
	key := id + variant
	s.previewMu.Lock()
	if s.previewPending[key] || len(s.previewPending) >= 128 {
		s.previewMu.Unlock()
		return
	}
	s.previewPending[key] = true
	s.previewMu.Unlock()
	go func() {
		defer func() { s.previewMu.Lock(); delete(s.previewPending, key); s.previewMu.Unlock() }()
		s.previewWorkers <- struct{}{}
		defer func() { <-s.previewWorkers }()
		var allowed int
		if s.DB.QueryRow("SELECT 1 FROM photos p JOIN galleries g ON g.id=p.gallery_id WHERE p.id=? AND p.archived=0 AND g.archived=0 AND g.status='Published'", id).Scan(&allowed) != nil {
			return
		}
		data, e := s.readMedia(id + variant)
		if e != nil || len(data) == 0 || len(data) > imgbb.MaxImageBytes || previewSignature(data) != sig {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		remote, e := s.previewStore.Upload(ctx, data)
		cancel()
		if e != nil {
			_, _ = s.DB.Exec(`INSERT INTO imgbb_previews(photo_id,variant,signature,url,expires,retry_after) VALUES(?,?,?,'',0,?) ON CONFLICT(photo_id,variant) DO UPDATE SET signature=excluded.signature,url='',expires=0,retry_after=excluded.retry_after`, id, variant, sig, time.Now().Add(5*time.Minute).Unix())
			return
		}
		current, e := s.readMedia(id + variant)
		if e != nil || previewSignature(current) != sig {
			return
		}
		if s.DB.QueryRow("SELECT 1 FROM photos p JOIN galleries g ON g.id=p.gallery_id WHERE p.id=? AND p.archived=0 AND g.archived=0 AND g.status='Published'", id).Scan(&allowed) != nil {
			return
		}
		// Renew well before ImgBB's 24-hour expiry.
		_, _ = s.DB.Exec(`INSERT INTO imgbb_previews(photo_id,variant,signature,url,expires,retry_after) VALUES(?,?,?,?,?,0) ON CONFLICT(photo_id,variant) DO UPDATE SET signature=excluded.signature,url=excluded.url,expires=excluded.expires,retry_after=0`, id, variant, sig, remote, time.Now().Add(23*time.Hour).Unix())
	}()
}
