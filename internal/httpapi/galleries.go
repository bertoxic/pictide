package httpapi

import (
	"database/sql"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case string:
		f, e := strconv.ParseFloat(n, 64)
		return f, e == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return 0, false
}
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func (s *Server) listGalleries(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("limit") {
		s.pagedGalleries(w, r)
		return
	}
	g, err := s.query(`SELECT g.*, (SELECT count(*) FROM photos p WHERE p.gallery_id=g.id AND p.archived=0) photo_count, COALESCE(NULLIF(g.cover_photo,''),(SELECT p.id FROM photos p WHERE p.gallery_id=g.id AND p.archived=0 ORDER BY p.created LIMIT 1)) cover FROM galleries g WHERE g.archived=0 ORDER BY created DESC`)
	if err != nil {
		fail(w, 500, "Unable to load galleries.")
		return
	}
	respond(w, 200, g)
}
func (s *Server) getGallery(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	g, err := s.gallery(id)
	if err != nil || (!s.owner(r) && g["status"] != "Published") {
		http.NotFound(w, r)
		return
	}
	photos, err := s.query("SELECT id,name,created,price,archived FROM photos WHERE gallery_id=? ORDER BY created DESC", id)
	if err != nil {
		fail(w, 500, "Unable to load photographs.")
		return
	}
	g["photos"] = photos
	g["studio_name"] = s.setting("studio_name", "Pictide Studio")
	g["contact_email"] = s.setting("contact_email", "")
	if !s.owner(r) {
		delete(g, "card_design")
	}
	respond(w, 200, g)
}
func (s *Server) createGallery(w http.ResponseWriter, r *http.Request) {
	var b map[string]any
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	name := strings.TrimSpace(str(b["name"]))
	p, ok := number(b["price"])
	if _, exists := b["price"]; !exists {
		p, ok = number(s.setting("default_price", "5000"))
	}
	if name == "" || len(name) > 200 || !ok || p < 0 || p > 10000000 {
		fail(w, 400, "A gallery name and valid price are required.")
		return
	}
	wm := str(b["watermark_text"])
	if wm == "" {
		wm = "PICTIDE · PROOF"
	}
	style := str(b["watermark_style"])
	if !validStyle(style) {
		style = "diagonal"
	}
	id := uuid.NewString()
	_, err := s.DB.Exec("INSERT INTO galleries(id,name,client,location,event_date,price,created,watermark_text,watermark_style) VALUES(?,?,?,?,?,?,?,?,?)", id, name, str(b["client"]), str(b["location"]), str(b["event_date"]), int64(math.Round(p*100)), now(), wm, style)
	if err != nil {
		fail(w, 500, "Unable to create gallery.")
		return
	}
	g, _ := s.gallery(id)
	respond(w, 201, g)
}
func validStyle(s string) bool {
	return s == "diagonal" || s == "center" || s == "tile" || s == "corner"
}
func (s *Server) updateGallery(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	g, err := s.gallery(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var b map[string]any
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	for _, key := range []string{"name", "client", "location", "event_date"} {
		if v, ok := b[key]; ok {
			g[key] = str(v)
		}
	}
	name := strings.TrimSpace(str(g["name"]))
	p, _ := number(g["price"])
	if v, ok := b["price"]; ok {
		price, valid := number(v)
		if !valid || price < 0 || price > 10000000 {
			fail(w, 400, "Enter a valid price.")
			return
		}
		p = math.Round(price * 100)
	}
	if name == "" {
		fail(w, 400, "Enter a gallery name.")
		return
	}
	status := str(g["status"])
	if v := str(b["status"]); v == "Draft" || v == "Published" {
		status = v
	}
	wm := str(g["watermark_text"])
	if v, ok := b["watermark_text"]; ok {
		wm = str(v)
	}
	style := str(g["watermark_style"])
	if v := str(b["watermark_style"]); validStyle(v) {
		style = v
	}
	_, err = s.DB.Exec("UPDATE galleries SET name=?,client=?,location=?,event_date=?,price=?,status=?,watermark_text=?,watermark_style=? WHERE id=?", name, str(g["client"]), str(g["location"]), str(g["event_date"]), int64(p), status, wm, style, id)
	if err != nil {
		fail(w, 500, "Unable to update gallery.")
		return
	}
	g, _ = s.gallery(id)
	respond(w, 200, g)
}
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	price, _ := strconv.ParseFloat(s.setting("default_price", "5000"), 64)
	_, err := os.Stat(filepath.Join(s.Config.MediaDir, "studio_logo.png"))
	respond(w, 200, map[string]any{"studio_name": s.setting("studio_name", "Pictide Studio"), "default_price": price, "contact_email": s.setting("contact_email", ""), "studio_logo": err == nil})
}
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var b map[string]any
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	name := strings.TrimSpace(str(b["studio_name"]))
	email := strings.TrimSpace(str(b["contact_email"]))
	p, ok := number(b["default_price"])
	if name == "" || len(name) > 80 || len(email) > 254 || (email != "" && (!strings.Contains(email, "@") || strings.ContainsAny(email, " \r\n"))) || !ok || p < 0 || p > 10000000 {
		fail(w, 400, "Enter a studio name and valid defaults.")
		return
	}
	tx, err := s.DB.Begin()
	if err == nil {
		for k, v := range map[string]string{"studio_name": name, "contact_email": email, "default_price": strconv.FormatFloat(p, 'f', -1, 64)} {
			_, err = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v)
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
		fail(w, 500, "Unable to save settings.")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

var _ = sql.ErrNoRows
var _ = json.Valid
