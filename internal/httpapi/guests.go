package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"image"
	"io"
	"pictide.local/studio/internal/media"
	"pictide.local/studio/internal/security"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (s *Server) guestUpload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	g, e := s.gallery(id)
	if e != nil || g["status"] != "Published" || g["archived"] == int64(1) || g["allow_guest_upload"] != int64(1) {
		fail(w, 403, "Guest uploads are not enabled for this gallery.")
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.mu.Lock()
	a := s.uploads[host]
	if time.Now().After(a.until) {
		a = attempt{until: time.Now().Add(time.Hour)}
	}
	if a.count >= 30 {
		s.mu.Unlock()
		fail(w, 429, "Upload limit reached.")
		return
	}
	a.count += 5
	s.uploads[host] = a
	s.mu.Unlock()
	r.Body = http.MaxBytesReader(w, r.Body, 51<<20)
	if r.ParseMultipartForm(12<<20) != nil {
		fail(w, 400, "Invalid upload.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["photos"]
	if len(files) == 0 || len(files) > 5 {
		fail(w, 400, "Choose up to five photos.")
		return
	}
	token, e := security.Token()
	if e != nil {
		fail(w, 500, "Unable to start upload.")
		return
	}
	added := []map[string]string{}
	errs := []string{}
	for _, h := range files {
		f, err := h.Open()
		if err != nil {
			errs = append(errs, h.Filename+": could not read")
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, 10<<20+1))
		f.Close()
		if err != nil || len(data) > 10<<20 {
			errs = append(errs, h.Filename+": file too large")
			continue
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
			errs = append(errs, h.Filename+": invalid image")
			continue
		}
		hash := sha256.Sum256(data)
		hs := hex.EncodeToString(hash[:])
		var n int
		if s.DB.QueryRow("SELECT 1 FROM photos WHERE gallery_id=? AND hash=? UNION ALL SELECT 1 FROM guest_uploads WHERE gallery_id=? AND hash=? AND status='pending' LIMIT 1", id, hs, id, hs).Scan(&n) == nil {
			errs = append(errs, h.Filename+": already uploaded")
			continue
		}
		assets, err := media.Process(data, media.Options{})
		if err != nil {
			errs = append(errs, h.Filename+": invalid image")
			continue
		}
		photo := uuid.NewString()
		err = os.WriteFile(filepath.Join(s.Config.MediaDir, photo+"_pending.png"), assets.Master, 0600)
		if err == nil {
			_, err = s.DB.Exec("INSERT INTO guest_uploads(id,gallery_id,name,hash,token_hash,status,created) VALUES(?,?,?,?,?,'pending',?)", photo, id, filepath.Base(h.Filename), hs, security.HashToken(token), now())
		}
		if err != nil {
			s.removeAssets(photo)
			_ = os.Remove(filepath.Join(s.Config.MediaDir, photo+"_pending.png"))
			errs = append(errs, h.Filename+": could not save")
			continue
		}
		added = append(added, map[string]string{"id": photo, "name": filepath.Base(h.Filename), "status": "pending"})
	}
	respond(w, 200, map[string]any{"token": token, "added": added, "errors": errs})
}
func (s *Server) guestStatus(w http.ResponseWriter, r *http.Request) {
	var b struct{ Tokens []string }
	if decode(r, &b) != nil || len(b.Tokens) > 50 {
		fail(w, 400, "Invalid request.")
		return
	}
	out := []map[string]any{}
	for _, token := range b.Tokens {
		if len(token) != 64 {
			continue
		}
		rows, e := s.query("SELECT id,gallery_id,name,status,created FROM guest_uploads WHERE token_hash=?", security.HashToken(token))
		if e != nil {
			fail(w, 500, "Could not load uploads.")
			return
		}
		out = append(out, rows...)
	}
	respond(w, 200, out)
}
func (s *Server) guestUploads(w http.ResponseWriter, r *http.Request) {
	out, e := s.query("SELECT id,name,status,created FROM guest_uploads WHERE gallery_id=? ORDER BY created DESC", chi.URLParam(r, "id"))
	if e != nil {
		fail(w, 500, "Could not load uploads.")
		return
	}
	respond(w, 200, out)
}
func (s *Server) guestPreview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, e := uuid.Parse(id); e != nil {
		http.NotFound(w, r)
		return
	}
	var n int
	if s.DB.QueryRow("SELECT 1 FROM guest_uploads WHERE id=? AND status='pending'", id).Scan(&n) != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.Config.MediaDir, id+"_pending.png"))
}
func (s *Server) reviewGuest(w http.ResponseWriter, r *http.Request) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	id := chi.URLParam(r, "id")
	if _, e := uuid.Parse(id); e != nil {
		http.NotFound(w, r)
		return
	}
	var b struct{ Action string }
	if decode(r, &b) != nil || (b.Action != "approve" && b.Action != "reject") {
		fail(w, 400, "Invalid review action.")
		return
	}
	p, e := s.one("SELECT * FROM guest_uploads WHERE id=? AND status='pending'", id)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	if b.Action == "approve" {
		g, e := s.gallery(str(p["gallery_id"]))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		data, e := os.ReadFile(filepath.Join(s.Config.MediaDir, id+"_pending.png"))
		if e != nil || s.processPhoto(id, data, g) != nil {
			fail(w, 500, "Could not process photo.")
			return
		}
		tx, e := s.DB.Begin()
		if e == nil {
			_, e = tx.Exec("INSERT INTO photos(id,gallery_id,name,hash,created,price) VALUES(?,?,?,?,?,0)", id, p["gallery_id"], p["name"], p["hash"], now())
			if e == nil {
				_, e = tx.Exec("UPDATE guest_uploads SET status='approved' WHERE id=? AND status='pending'", id)
			}
			if e == nil {
				e = tx.Commit()
			} else {
				tx.Rollback()
			}
		}
		if e != nil {
			for _, suffix := range []string{".jpg", "_thumb.jpg", "_clean.jpg", "_thumb_clean.jpg", ".png", ".original"} {
				_ = os.Remove(filepath.Join(s.Config.MediaDir, id+suffix))
			}
			fail(w, 409, "Photo may already be in the gallery.")
			return
		}
	} else {
		if _, e = s.DB.Exec("UPDATE guest_uploads SET status='rejected' WHERE id=? AND status='pending'", id); e != nil {
			fail(w, 500, "Could not update upload.")
			return
		}
	}
	_ = os.Remove(filepath.Join(s.Config.MediaDir, id+"_pending.png"))
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) deleteGallery(w http.ResponseWriter, r *http.Request) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	id := chi.URLParam(r, "id")
	if _, e := uuid.Parse(id); e != nil {
		http.NotFound(w, r)
		return
	}
	var n int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM orders WHERE gallery_id=?", id).Scan(&n); err != nil {
		fail(w, 500, "Could not check gallery orders.")
		return
	}
	if n > 0 {
		if _, err := s.DB.Exec("UPDATE galleries SET archived=1 WHERE id=?", id); err != nil {
			fail(w, 500, "Could not archive gallery.")
			return
		}
		w.WriteHeader(204)
		return
	}
	photos, e := s.query("SELECT id FROM photos WHERE gallery_id=? UNION SELECT id FROM guest_uploads WHERE gallery_id=?", id, id)
	if e != nil {
		fail(w, 500, "Could not delete gallery.")
		return
	}
	tx, e := s.DB.Begin()
	if e == nil {
		for _, stmt := range []string{"DELETE FROM entitlements WHERE order_id IN (SELECT id FROM orders WHERE gallery_id=?)", "DELETE FROM order_photos WHERE order_id IN (SELECT id FROM orders WHERE gallery_id=?)", "DELETE FROM orders WHERE gallery_id=?", "DELETE FROM galleries WHERE id=?"} {
			_, e = tx.Exec(stmt, id)
			if e != nil {
				break
			}
		}
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	if e != nil {
		fail(w, 500, "Could not delete gallery.")
		return
	}
	for _, p := range photos {
		s.removeAssets(str(p["id"]))
		_ = os.Remove(filepath.Join(s.Config.MediaDir, str(p["id"])+"_pending.png"))
	}
	_ = os.Remove(filepath.Join(s.Config.MediaDir, id+"_logo.png"))
	w.WriteHeader(204)
}

var _ = fmt.Sprint
