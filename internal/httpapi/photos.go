package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"pictide.local/studio/internal/media"
	"pictide.local/studio/internal/mongostore"
	"pictide.local/studio/internal/security"
)

func (s *Server) uploadPhotos(w http.ResponseWriter, r *http.Request, user *account, galleryID string) {
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	gallery, err := store.Get(r.Context(), galleryID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			http.NotFound(w, r)
		} else {
			fail(w, 500, "Unable to load gallery.")
		}
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
	opt := media.Options{Watermark: stringValue(gallery["watermark_text"]), Style: stringValue(gallery["watermark_style"]), Color: stringValue(gallery["watermark_color"]), BrandPosition: stringValue(gallery["brand_position"])}
	opacity, _ := number(gallery["watermark_opacity"])
	size, _ := number(gallery["watermark_size"])
	opt.WatermarkOpacity = int(opacity)
	opt.WatermarkSize = int(size)
	added := 0
	failures := []string{}
	bucket := s.db.GridFSBucket()
	for _, header := range files {
		f, e := header.Open()
		if e != nil {
			failures = append(failures, header.Filename+": could not read")
			continue
		}
		data, e := io.ReadAll(io.LimitReader(f, (30<<20)+1))
		f.Close()
		if e != nil || len(data) > 30<<20 {
			failures = append(failures, header.Filename+": file too large")
			continue
		}
		hash := sha256.Sum256(data)
		hs := hex.EncodeToString(hash[:])
		assets, e := media.Process(data, opt)
		if e != nil {
			failures = append(failures, header.Filename+": invalid image")
			continue
		}
		id, e := security.Token()
		if e != nil {
			failures = append(failures, header.Filename+": could not save")
			continue
		}
		doc := bson.M{"id": id, "studio_id": user.ID, "gallery_id": galleryID, "name": filepath.Base(header.Filename), "hash": hs, "created": timestamp(), "archived": 0, "price": nil, "ready": false}
		if _, e = s.db.Collection("photos").InsertOne(r.Context(), doc); e != nil {
			if mongo.IsDuplicateKeyError(e) {
				failures = append(failures, header.Filename+": already added")
			} else {
				failures = append(failures, header.Filename+": could not save")
			}
			continue
		}
		payloads := map[string][]byte{"original": data, "preview": assets.Preview, "thumb": assets.Thumb, "clean": assets.CleanPreview, "clean_thumb": assets.CleanThumb, "master": assets.Master}
		saved := []string{}
		for suffix, body := range payloads {
			fileID := id + ":" + suffix
			if e = bucket.UploadFromStreamWithID(r.Context(), fileID, fileID, bytes.NewReader(body)); e != nil {
				break
			}
			saved = append(saved, fileID)
		}
		if e == nil {
			_, e = s.db.Collection("photos").UpdateOne(r.Context(), bson.M{"id": id, "studio_id": user.ID}, bson.M{"$set": bson.M{"ready": true}})
		}
		if e != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			for _, fileID := range saved {
				_ = bucket.Delete(cleanup, fileID)
			}
			_, _ = s.db.Collection("photos").DeleteOne(cleanup, bson.M{"id": id, "studio_id": user.ID})
			cancel()
			failures = append(failures, header.Filename+": could not save")
			continue
		}
		added++
	}
	reply(w, 200, bson.M{"added": added, "errors": failures})
}

func (s *Server) servePreview(w http.ResponseWriter, r *http.Request, user *account, id string) {
	if id == "" || strings.ContainsAny(id, "/\\") {
		http.NotFound(w, r)
		return
	}
	var photo struct {
		StudioID  string `bson:"studio_id"`
		GalleryID string `bson:"gallery_id"`
		Archived  int    `bson:"archived"`
	}
	err := s.db.Collection("photos").FindOne(r.Context(), bson.M{"id": id, "ready": true}).Decode(&photo)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var gallery struct {
		Status   string `bson:"status"`
		Archived int    `bson:"archived"`
	}
	err = s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": photo.GalleryID, "studio_id": photo.StudioID}).Decode(&gallery)
	if err != nil || (user == nil || user.ID != photo.StudioID) && (gallery.Status != "Published" || gallery.Archived != 0 || photo.Archived != 0) {
		http.NotFound(w, r)
		return
	}
	suffix := "preview"
	if r.URL.Query().Get("size") == "thumb" {
		suffix = "thumb"
	}
	var b bytes.Buffer
	if _, err = s.db.GridFSBucket().DownloadToStream(r.Context(), id+":"+suffix, &b); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b.Bytes())
}

func (s *Server) photoPrice(w http.ResponseWriter, r *http.Request, user *account, id string) {
	var b map[string]any
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	v, exists := b["price"]
	if !exists {
		fail(w, 400, "Price is required.")
		return
	}
	var price any
	if v != nil {
		p, ok := number(v)
		if !ok || p < 0 || p > 10000000 {
			fail(w, 400, "Enter a valid photo price.")
			return
		}
		price = int64(math.Round(p * 100))
	}
	result, err := s.db.Collection("photos").UpdateOne(r.Context(), bson.M{"id": id, "studio_id": user.ID, "ready": true}, bson.M{"$set": bson.M{"price": price}})
	if err != nil {
		fail(w, 500, "Unable to update photo.")
		return
	}
	if result.MatchedCount == 0 {
		http.NotFound(w, r)
		return
	}
	reply(w, 200, bson.M{"ok": true})
}
func (s *Server) archivePhoto(w http.ResponseWriter, r *http.Request, user *account, id string) {
	result, err := s.db.Collection("photos").UpdateOne(r.Context(), bson.M{"id": id, "studio_id": user.ID, "ready": true}, bson.M{"$set": bson.M{"archived": 1}})
	if err != nil {
		fail(w, 500, "Unable to remove photo.")
		return
	}
	if result.MatchedCount == 0 {
		http.NotFound(w, r)
		return
	}
	_, err = s.db.Collection("galleries").UpdateMany(r.Context(), bson.M{"studio_id": user.ID, "cover_photo": id}, bson.M{"$set": bson.M{"cover_photo": ""}})
	if err != nil {
		fail(w, 500, "Unable to update gallery cover.")
		return
	}
	reply(w, 200, bson.M{"ok": true})
}

func (s *Server) publicGallery(w http.ResponseWriter, r *http.Request, id string) {
	var gallery bson.M
	if err := s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": id, "status": "Published", "archived": bson.M{"$ne": 1}}).Decode(&gallery); err != nil {
		http.NotFound(w, r)
		return
	}
	studio, ok := gallery["studio_id"].(string)
	if !ok {
		http.NotFound(w, r)
		return
	}
	cur, err := s.db.Collection("photos").Find(r.Context(), bson.M{"studio_id": studio, "gallery_id": id, "ready": true, "archived": bson.M{"$ne": 1}}, options.Find().SetProjection(bson.M{"_id": 0, "studio_id": 0, "hash": 0, "ready": 0}).SetSort(bson.D{{Key: "created", Value: -1}}))
	if err != nil {
		fail(w, 500, "Unable to load photographs.")
		return
	}
	defer cur.Close(r.Context())
	photos := []bson.M{}
	if cur.All(r.Context(), &photos) != nil {
		fail(w, 500, "Unable to load photographs.")
		return
	}
	var setting struct {
		StudioName   string `bson:"studio_name"`
		ContactEmail string `bson:"contact_email"`
	}
	if s.db.Collection("settings").FindOne(r.Context(), bson.M{"_id": studio}).Decode(&setting) != nil {
		setting.StudioName = "Pictide Studio"
	}
	delete(gallery, "_id")
	delete(gallery, "studio_id")
	delete(gallery, "card_design")
	gallery["photos"] = photos
	gallery["photo_count"] = len(photos)
	gallery["studio_name"] = setting.StudioName
	gallery["contact_email"] = setting.ContactEmail
	reply(w, 200, gallery)
}
