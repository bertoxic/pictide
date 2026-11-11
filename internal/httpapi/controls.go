package httpapi

import (
	"errors"
	"image/color"
	"io"
	"net/http"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"pictide.local/studio/internal/mongostore"
	"pictide.local/studio/internal/qr"
	"pictide.local/studio/internal/security"
)

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, user *account) {
	var b struct{ Current, Password string }
	if decode(r, &b) != nil || len(b.Password) < 12 || len(b.Password) > 200 {
		fail(w, 400, "Use a password between 12 and 200 characters.")
		return
	}
	if !security.CheckPassword(user.PasswordHash, b.Current) {
		fail(w, 403, "Current password is incorrect.")
		return
	}
	hash, err := security.PasswordHash(b.Password)
	if err != nil {
		fail(w, 500, "Unable to update password.")
		return
	}
	version, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to update password.")
		return
	}
	result, err := s.db.Collection("accounts").UpdateOne(r.Context(), bson.M{"_id": user.ID, "password_hash": user.PasswordHash, "disabled": false}, bson.M{"$set": bson.M{"password_hash": hash, "session_version": version}})
	if err != nil || result.MatchedCount != 1 {
		fail(w, 500, "Unable to update password.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "lumen_platform", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secure(), SameSite: http.SameSiteStrictMode})
	reply(w, 200, bson.M{"ok": true})
}

func (s *Server) accountStatus(w http.ResponseWriter, r *http.Request, user *account) {
	if user.Role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	var b struct {
		ID       string
		Disabled bool
	}
	if decode(r, &b) != nil || b.ID == "" {
		fail(w, 400, "Invalid account.")
		return
	}
	version, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to update account.")
		return
	}
	result, err := s.db.Collection("accounts").UpdateOne(r.Context(), bson.M{"_id": b.ID, "role": "photographer"}, bson.M{"$set": bson.M{"disabled": b.Disabled, "session_version": version}})
	if err != nil {
		fail(w, 500, "Unable to update account.")
		return
	}
	if result.MatchedCount != 1 {
		http.NotFound(w, r)
		return
	}
	reply(w, 200, bson.M{"ok": true})
}

func (s *Server) cover(w http.ResponseWriter, r *http.Request, user *account, id string) {
	var b struct{ Photo string }
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	if b.Photo != "" {
		count, err := s.db.Collection("photos").CountDocuments(r.Context(), bson.M{"id": b.Photo, "gallery_id": id, "studio_id": user.ID, "ready": true, "archived": bson.M{"$ne": 1}})
		if err != nil {
			fail(w, 500, "Unable to update cover.")
			return
		}
		if count != 1 {
			fail(w, 400, "Photo not found in gallery.")
			return
		}
	}
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	if err := store.Update(r.Context(), id, bson.M{"cover_photo": b.Photo}); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			http.NotFound(w, r)
		} else {
			fail(w, 500, "Unable to update cover.")
		}
		return
	}
	reply(w, 200, bson.M{"ok": true})
}

func (s *Server) customize(w http.ResponseWriter, r *http.Request, user *account, id string) {
	var input map[string]any
	if decode(r, &input) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	fields := bson.M{}
	for k, v := range input {
		switch k {
		case "watermark_text", "share_title", "share_message":
			str, ok := v.(string)
			if !ok || len(str) > 240 {
				fail(w, 400, "Invalid text.")
				return
			}
			fields[k] = str
		case "watermark_style":
			style, ok := v.(string)
			if !ok || (style != "diagonal" && style != "center" && style != "tile" && style != "corner") {
				fail(w, 400, "Invalid watermark style.")
				return
			}
			fields[k] = style
		case "watermark_color", "share_color", "share_qr_background":
			str, ok := v.(string)
			if !ok || !cardColor.MatchString(str) {
				fail(w, 400, "Invalid color.")
				return
			}
			fields[k] = str
		case "brand_position":
			position, ok := v.(string)
			if !ok || (position != "top-left" && position != "top-right" && position != "bottom-left" && position != "bottom-right") {
				fail(w, 400, "Invalid brand position.")
				return
			}
			fields[k] = position
		case "watermark_opacity", "watermark_size", "brand_size", "brand_opacity", "allow_guest_upload":
			n, ok := number(v)
			if !ok || n < 0 || n > 100 || n != float64(int(n)) || (k == "allow_guest_upload" && n > 1) {
				fail(w, 400, "Invalid customization value.")
				return
			}
			fields[k] = int(n)
		default:
			fail(w, 400, "Unknown customization field.")
			return
		}
	}
	if len(fields) == 0 {
		fail(w, 400, "No changes provided.")
		return
	}
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	if err := store.Update(r.Context(), id, fields); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			http.NotFound(w, r)
		} else {
			fail(w, 500, "Unable to update gallery.")
		}
		return
	}
	reply(w, 200, bson.M{"ok": true})
}

func (s *Server) cardDesign(w http.ResponseWriter, r *http.Request, user *account, id string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1_200_001))
	if err != nil || len(body) > 1_200_000 || !validCard(body) {
		fail(w, 400, "Invalid card design.")
		return
	}
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	if err = store.Update(r.Context(), id, bson.M{"card_design": string(body)}); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			http.NotFound(w, r)
		} else {
			fail(w, 500, "Unable to save card.")
		}
		return
	}
	reply(w, 200, bson.M{"ok": true})
}

func (s *Server) galleryQR(w http.ResponseWriter, r *http.Request, user *account, id string) {
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	gallery, err := store.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fg := r.URL.Query().Get("qr_color")
	if fg == "" {
		fg = stringValue(gallery["share_color"])
	}
	bg := r.URL.Query().Get("qr_background")
	if bg == "" {
		bg = stringValue(gallery["share_qr_background"])
	}
	if !cardColor.MatchString(fg) || !cardColor.MatchString(bg) {
		fail(w, 400, "Invalid QR color.")
		return
	}
	base := s.cfg.PublicURL
	if base == "" {
		base = "http://" + r.Host
	}
	link := base + "/l/" + id
	if r.URL.Query().Get("format") == "png" {
		code, e := qrcode.New(link, qrcode.Medium)
		if e != nil {
			fail(w, 500, "Unable to create QR code.")
			return
		}
		code.ForegroundColor = qrColor(fg)
		code.BackgroundColor = qrColor(bg)
		data, e := code.PNG(1200)
		if e != nil {
			fail(w, 500, "Unable to create QR code.")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
		return
	}
	data, e := qr.SVGWithBackground(link, fg, bg)
	if e != nil {
		fail(w, 500, "Unable to create QR code.")
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = w.Write(data)
}
func qrColor(s string) color.RGBA {
	v, _ := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
}
