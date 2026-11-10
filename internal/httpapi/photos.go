package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/disintegration/imaging"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"image"
	"image/png"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"pictide.local/studio/internal/media"
	"regexp"
	"strings"
)

func (s *Server) opts(g map[string]any) media.Options {
	num := func(k string) int { v, _ := number(g[k]); return int(v) }
	opt := media.Options{Watermark: str(g["watermark_text"]), Style: str(g["watermark_style"]), Color: str(g["watermark_color"]), WatermarkOpacity: num("watermark_opacity"), WatermarkSize: num("watermark_size"), BrandPosition: str(g["brand_position"]), BrandOpacity: num("brand_opacity"), BrandSize: num("brand_size")}
	file := filepath.Join(s.Config.MediaDir, str(g["id"])+"_logo.png")
	if str(g["watermark_logo"]) == "" {
		file = filepath.Join(s.Config.MediaDir, "studio_logo.png")
	}
	if data, e := s.readMedia(filepath.Base(file)); e == nil {
		opt.Brand, _, _ = image.Decode(bytes.NewReader(data))
	}
	return opt
}
func (s *Server) writeAssets(id string, a media.Assets) error {
	for suffix, data := range map[string][]byte{".jpg": a.Preview, "_thumb.jpg": a.Thumb, "_clean.jpg": a.CleanPreview, "_thumb_clean.jpg": a.CleanThumb, ".png": a.Master} {
		if err := s.writeMedia(id+suffix, data); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) removeAssets(id string) {
	_, _ = s.DB.Exec("DELETE FROM imgbb_previews WHERE photo_id=?", id)
	for _, suffix := range []string{".jpg", "_thumb.jpg", "_clean.jpg", "_thumb_clean.jpg", ".png", ".original"} {
		_ = s.removeMedia(id + suffix)
	}
}
func (s *Server) processPhoto(id string, data []byte, g map[string]any) error {
	a, err := media.Process(data, s.opts(g))
	if err != nil {
		return err
	}
	if err = s.writeMedia(id+".original", data); err != nil {
		return err
	}
	if err = s.writeAssets(id, a); err != nil {
		s.removeAssets(id)
		return err
	}
	return nil
}
func (s *Server) uploadPhotos(w http.ResponseWriter, r *http.Request) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	g, err := s.gallery(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if err = r.ParseMultipartForm(16 << 20); err != nil {
		fail(w, 400, "Upload is too large or invalid.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["photos"]
	if len(files) > 50 {
		fail(w, 400, "Upload at most 50 photos.")
		return
	}
	added := 0
	errs := []string{}
	for _, header := range files {
		f, e := header.Open()
		if e != nil {
			errs = append(errs, header.Filename+": could not read")
			continue
		}
		data, e := io.ReadAll(io.LimitReader(f, 30<<20+1))
		f.Close()
		if e != nil || len(data) > 30<<20 {
			errs = append(errs, header.Filename+": file too large")
			continue
		}
		hash := sha256.Sum256(data)
		hs := hex.EncodeToString(hash[:])
		var existing int
		if s.DB.QueryRow("SELECT 1 FROM photos WHERE gallery_id=? AND hash=?", g["id"], hs).Scan(&existing) == nil {
			errs = append(errs, header.Filename+": already added")
			continue
		}
		id := uuid.NewString()
		if e = s.processPhoto(id, data, g); e != nil {
			errs = append(errs, header.Filename+": invalid image")
			continue
		}
		_, e = s.DB.Exec("INSERT INTO photos(id,gallery_id,name,hash,created) VALUES(?,?,?,?,?)", id, g["id"], filepath.Base(header.Filename), hs, now())
		if e != nil {
			s.removeAssets(id)
			errs = append(errs, header.Filename+": could not save")
			continue
		}
		added++
	}
	respond(w, 200, map[string]any{"added": added, "errors": errs})
}
func (s *Server) photoPrice(w http.ResponseWriter, r *http.Request) {
	var b map[string]any
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	v, ok := b["price"]
	if !ok {
		fail(w, 400, "Price is required.")
		return
	}
	var value any
	if v != nil {
		p, valid := number(v)
		if !valid || p < 0 || p > 10000000 {
			fail(w, 400, "Enter a valid photo price.")
			return
		}
		value = int64(math.Round(p * 100))
	}
	result, err := s.DB.Exec("UPDATE photos SET price=? WHERE id=?", value, chi.URLParam(r, "id"))
	if err != nil {
		fail(w, 500, "Unable to update photo.")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		http.NotFound(w, r)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) deletePhoto(w http.ResponseWriter, r *http.Request) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return
	}
	var n int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM order_photos WHERE photo_id=?", id).Scan(&n); err != nil {
		fail(w, 500, "Unable to check photo orders.")
		return
	}
	if n > 0 {
		if _, err := s.DB.Exec("UPDATE photos SET archived=1 WHERE id=?", id); err != nil {
			fail(w, 500, "Unable to archive photo.")
			return
		}
		_, _ = s.DB.Exec("UPDATE galleries SET cover_photo='' WHERE cover_photo=?", id)
		w.WriteHeader(204)
		return
	}
	tx, err := s.DB.Begin()
	if err == nil {
		_, err = tx.Exec("UPDATE galleries SET cover_photo='' WHERE cover_photo=?", id)
		if err == nil {
			_, err = tx.Exec("DELETE FROM photos WHERE id=?", id)
		}
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	if err != nil {
		fail(w, 500, "Unable to delete photo.")
		return
	}
	s.removeAssets(id)
	w.WriteHeader(204)
}
func (s *Server) cover(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct{ Photo string }
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	if b.Photo != "" {
		var n int
		if s.DB.QueryRow("SELECT 1 FROM photos WHERE id=? AND gallery_id=?", b.Photo, id).Scan(&n) != nil {
			fail(w, 400, "Choose a photo from this gallery.")
			return
		}
	}
	result, err := s.DB.Exec("UPDATE galleries SET cover_photo=? WHERE id=?", b.Photo, id)
	if err != nil {
		fail(w, 500, "Unable to set cover.")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		http.NotFound(w, r)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) customize(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, e := s.gallery(id); e != nil {
		http.NotFound(w, r)
		return
	}
	var b map[string]any
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	hexColor := regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	sets := map[string]func(any) bool{"watermark_text": func(v any) bool { x, ok := v.(string); return ok && len(x) <= 240 }, "watermark_style": func(v any) bool { return validStyle(str(v)) }, "watermark_color": func(v any) bool { return hexColor.MatchString(str(v)) }, "brand_position": func(v any) bool {
		return map[string]bool{"bottom-right": true, "bottom-left": true, "top-right": true, "top-left": true}[str(v)]
	}, "share_title": func(v any) bool { x, ok := v.(string); return ok && len(x) <= 400 }, "share_message": func(v any) bool { x, ok := v.(string); return ok && len(x) <= 960 }, "share_color": func(v any) bool { return hexColor.MatchString(str(v)) }, "share_qr_background": func(v any) bool { return hexColor.MatchString(str(v)) }}
	bounds := map[string][2]float64{"watermark_opacity": {0, 100}, "watermark_size": {1, 15}, "brand_size": {5, 30}, "brand_opacity": {0, 100}, "allow_guest_upload": {0, 1}}
	for k, v := range b {
		if fn, ok := sets[k]; ok {
			if !fn(v) {
				fail(w, 400, "Invalid customization: "+k)
				return
			}
		} else if bound, ok := bounds[k]; ok {
			n, valid := number(v)
			if !valid || math.Trunc(n) != n || n < bound[0] || n > bound[1] {
				fail(w, 400, "Invalid customization: "+k)
				return
			}
		} else {
			fail(w, 400, "Invalid customization: "+k)
			return
		}
	}
	tx, err := s.DB.Begin()
	if err == nil {
		for k, v := range b {
			_, err = tx.Exec("UPDATE galleries SET "+k+"=? WHERE id=?", v, id)
			if err != nil {
				break
			}
		}
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	if err != nil {
		fail(w, 500, "Unable to save customization.")
		return
	}
	g, _ := s.gallery(id)
	respond(w, 200, g)
}
func (s *Server) cardDesign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, e := s.gallery(id); e != nil {
		http.NotFound(w, r)
		return
	}
	data, e := io.ReadAll(io.LimitReader(r.Body, 1200001))
	if e != nil || len(data) > 1200000 || !json.Valid(data) {
		fail(w, 400, "Invalid card design.")
		return
	}
	if !validCard(data) {
		fail(w, 400, "Invalid card design.")
		return
	}
	_, e = s.DB.Exec("UPDATE galleries SET card_design=? WHERE id=?", string(data), id)
	if e != nil {
		fail(w, 500, "Could not save design.")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) regenGallery(id string) (int, []string) {
	g, err := s.gallery(id)
	if err != nil {
		return 0, []string{id}
	}
	rows, err := s.query("SELECT id FROM photos WHERE gallery_id=?", id)
	if err != nil {
		return 0, []string{id}
	}
	updated := 0
	errs := []string{}
	for _, p := range rows {
		photo := str(p["id"])
		data, e := s.readMedia(photo + ".original")
		if e == nil {
			var a media.Assets
			a, e = media.Process(data, s.opts(g))
			if e == nil {
				e = s.writeAssets(photo, a)
			}
		}
		if e != nil {
			errs = append(errs, photo)
		} else {
			updated++
		}
	}
	return updated, errs
}
func (s *Server) regenerate(w http.ResponseWriter, r *http.Request) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	id := chi.URLParam(r, "id")
	if _, e := s.gallery(id); e != nil {
		http.NotFound(w, r)
		return
	}
	n, errs := s.regenGallery(id)
	respond(w, 200, map[string]any{"updated": n, "errors": errs})
}
func (s *Server) saveLogo(w http.ResponseWriter, r *http.Request) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	id := chi.URLParam(r, "id")
	if id != "" {
		if _, e := s.gallery(id); e != nil {
			http.NotFound(w, r)
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	if e := r.ParseMultipartForm(3 << 20); e != nil {
		fail(w, 400, "Invalid image.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, _, e := r.FormFile("logo")
	if e != nil {
		fail(w, 400, "Choose a logo.")
		return
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, 2<<20+1))
	if e != nil || len(data) > 2<<20 {
		fail(w, 400, "Logo is too large.")
		return
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || cfg.Width*cfg.Height > 4000000 || !map[string]bool{"png": true, "jpeg": true, "webp": true}[format] {
		fail(w, 400, "Choose a valid PNG, JPEG or WebP logo.")
		return
	}
	img, e := imaging.Decode(bytes.NewReader(data))
	if e != nil {
		fail(w, 400, "Invalid image.")
		return
	}
	filename := "studio_logo.png"
	if id != "" {
		filename = id + "_logo.png"
	}
	var buf bytes.Buffer
	e = png.Encode(&buf, imaging.Fit(img, 320, 160, imaging.Linear))
	if e == nil {
		e = s.writeMedia(filename, buf.Bytes())
	}
	if e != nil {
		fail(w, 500, "Could not save logo.")
		return
	}
	if id != "" {
		_, e = s.DB.Exec("UPDATE galleries SET watermark_logo='set' WHERE id=?", id)
	}
	if e != nil {
		fail(w, 500, "Could not save logo.")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) deleteLogo(w http.ResponseWriter, r *http.Request) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	id := chi.URLParam(r, "id")
	filename := "studio_logo.png"
	if id != "" {
		if _, e := s.gallery(id); e != nil {
			http.NotFound(w, r)
			return
		}
		filename = id + "_logo.png"
		if _, e := s.DB.Exec("UPDATE galleries SET watermark_logo='' WHERE id=?", id); e != nil {
			fail(w, 500, "Could not remove logo.")
			return
		}
	}
	_ = s.removeMedia(filename)
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) applyBrand(w http.ResponseWriter, r *http.Request) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	rows, e := s.query("SELECT id FROM galleries")
	if e != nil {
		fail(w, 500, "Could not load galleries.")
		return
	}
	n := 0
	errs := []string{}
	for _, g := range rows {
		x, es := s.regenGallery(str(g["id"]))
		n += x
		errs = append(errs, es...)
	}
	respond(w, 200, map[string]any{"updated": n, "errors": errs})
}

var _ = errors.New
var _ = fmt.Sprint
var _ = strings.TrimSpace
