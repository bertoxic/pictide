package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"pictide.local/studio/internal/media"
	"pictide.local/studio/internal/mongostore"
	"pictide.local/studio/internal/security"
)

type directUpload struct {
	ID        string `bson:"_id"`
	StudioID  string `bson:"studio_id"`
	GalleryID string `bson:"gallery_id"`
	Name      string `bson:"name"`
	Size      int64  `bson:"size"`
	ObjectKey string `bson:"object_key"`
	Status    string `bson:"status"`
}

func (s *Server) startDirectUpload(w http.ResponseWriter, r *http.Request, user *account, galleryID string) {
	if s.r2 == nil {
		fail(w, 503, "Direct uploads are not configured.")
		return
	}
	var input struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if decode(r, &input) != nil || input.Size <= 0 || input.Size > maxPhotoSize {
		fail(w, 400, "Choose an image no larger than 30 MB.")
		return
	}
	input.Name = path.Base(strings.ReplaceAll(input.Name, "\\", "/"))
	if input.Name == "" || input.Name == "." || input.Name == "/" || len(input.Name) > 255 {
		fail(w, 400, "Invalid photo name.")
		return
	}
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	gallery, err := store.Get(r.Context(), galleryID)
	if err != nil || gallery["archived"] == int32(1) || gallery["archived"] == int64(1) {
		http.NotFound(w, r)
		return
	}
	id, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to start upload.")
		return
	}
	key := r2TemporaryKey(user.ID, id)
	signedURL, err := s.r2.putURL(r.Context(), key)
	if err != nil {
		fail(w, 500, "Unable to start upload.")
		return
	}
	doc := bson.M{"_id": id, "studio_id": user.ID, "gallery_id": galleryID, "name": input.Name, "size": input.Size, "object_key": key, "status": "pending", "expires_at": time.Now().Add(2 * time.Hour)}
	if _, err = s.db.Collection("direct_uploads").InsertOne(r.Context(), doc); err != nil {
		fail(w, 500, "Unable to start upload.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	reply(w, 200, bson.M{"upload_id": id, "url": signedURL, "content_type": "application/octet-stream"})
}

func (s *Server) completeDirectUpload(w http.ResponseWriter, r *http.Request, user *account, id string) {
	if s.r2 == nil {
		fail(w, 503, "Direct uploads are not configured.")
		return
	}
	var existing directUpload
	err := s.db.Collection("direct_uploads").FindOne(r.Context(), bson.M{"_id": id, "studio_id": user.ID}).Decode(&existing)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if existing.Status == "complete" {
		reply(w, 200, bson.M{"ok": true, "id": id})
		return
	}
	var ready struct {
		Ready bool `bson:"ready"`
	}
	if s.db.Collection("photos").FindOne(r.Context(), bson.M{"id": id, "studio_id": user.ID, "gallery_id": existing.GalleryID, "ready": true}).Decode(&ready) == nil {
		_, _ = s.db.Collection("direct_uploads").UpdateOne(r.Context(), bson.M{"_id": id, "studio_id": user.ID}, bson.M{"$set": bson.M{"status": "complete"}})
		reply(w, 200, bson.M{"ok": true, "id": id})
		return
	}
	lease, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to finish upload.")
		return
	}
	now := time.Now()
	filter := bson.M{"_id": id, "studio_id": user.ID, "expires_at": bson.M{"$gt": now}, "$or": bson.A{bson.M{"status": "pending"}, bson.M{"status": "processing", "lease_until": bson.M{"$lt": now}}}}
	update := bson.M{"$set": bson.M{"status": "processing", "lease": lease, "lease_until": now.Add(10 * time.Minute)}}
	var job directUpload
	if err = s.db.Collection("direct_uploads").FindOneAndUpdate(r.Context(), filter, update).Decode(&job); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			fail(w, 409, "Upload is expired or being processed.")
		} else {
			fail(w, 500, "Unable to finish upload.")
		}
		return
	}
	finished := false
	defer func() {
		if !finished {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = s.db.Collection("direct_uploads").UpdateOne(ctx, bson.M{"_id": id, "studio_id": user.ID, "lease": lease}, bson.M{"$set": bson.M{"status": "pending"}, "$unset": bson.M{"lease": "", "lease_until": ""}})
		}
	}()
	data, err := s.r2.get(r.Context(), job.ObjectKey, maxPhotoSize)
	if err != nil || int64(len(data)) != job.Size {
		fail(w, 400, "The uploaded file is missing or has the wrong size.")
		return
	}
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	gallery, err := store.Get(r.Context(), job.GalleryID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	archived, _ := number(gallery["archived"])
	if archived == 1 {
		http.NotFound(w, r)
		return
	}
	opt := media.Options{Watermark: stringValue(gallery["watermark_text"]), Style: stringValue(gallery["watermark_style"]), Color: stringValue(gallery["watermark_color"]), BrandPosition: stringValue(gallery["brand_position"])}
	opacity, _ := number(gallery["watermark_opacity"])
	size, _ := number(gallery["watermark_size"])
	opt.WatermarkOpacity = int(opacity)
	opt.WatermarkSize = int(size)
	assets, err := media.Process(data, opt)
	if err != nil {
		fail(w, 400, "The uploaded file is not a supported image.")
		return
	}
	hash := sha256.Sum256(data)
	hashString := hex.EncodeToString(hash[:])
	bucket := s.db.GridFSBucket()
	// A previously interrupted completion may have left a non-ready record.
	if s.db.Collection("photos").FindOne(r.Context(), bson.M{"id": id, "studio_id": user.ID, "ready": false}).Err() == nil {
		_, _ = s.db.Collection("photos").DeleteOne(r.Context(), bson.M{"id": id, "studio_id": user.ID, "ready": false})
		for _, variant := range []string{"preview", "thumb", "clean", "clean_thumb"} {
			_ = bucket.Delete(r.Context(), id+":"+variant)
		}
	}
	doc := bson.M{"id": id, "studio_id": user.ID, "gallery_id": job.GalleryID, "name": job.Name, "hash": hashString, "created": timestamp(), "archived": 0, "price": nil, "ready": false, "r2_master": true, "r2_original": true}
	if _, err = s.db.Collection("photos").InsertOne(r.Context(), doc); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			fail(w, 409, "Photo already added to this gallery.")
		} else {
			fail(w, 500, "Unable to save photo.")
		}
		return
	}
	savedGrid := []string{}
	savedR2 := []string{}
	success := false
	defer func() {
		if !success {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			for _, key := range savedGrid {
				_ = bucket.Delete(ctx, key)
			}
			for _, key := range savedR2 {
				_ = s.r2.delete(ctx, key)
			}
			_, _ = s.db.Collection("photos").DeleteOne(ctx, bson.M{"id": id, "studio_id": user.ID, "ready": false})
		}
	}()
	for variant, body := range map[string][]byte{"original": data, "master": assets.Master} {
		key := r2PhotoKey(user.ID, id, variant)
		contentType := "application/octet-stream"
		if variant == "master" {
			contentType = "image/png"
		}
		if err = s.r2.put(r.Context(), key, contentType, body); err != nil {
			fail(w, 500, "Unable to save photo.")
			return
		}
		savedR2 = append(savedR2, key)
	}
	for variant, body := range map[string][]byte{"preview": assets.Preview, "thumb": assets.Thumb, "clean": assets.CleanPreview, "clean_thumb": assets.CleanThumb} {
		key := id + ":" + variant
		if err = bucket.UploadFromStreamWithID(r.Context(), key, key, bytes.NewReader(body)); err != nil {
			fail(w, 500, "Unable to save photo.")
			return
		}
		savedGrid = append(savedGrid, key)
	}
	result, err := s.db.Collection("photos").UpdateOne(r.Context(), bson.M{"id": id, "studio_id": user.ID, "ready": false}, bson.M{"$set": bson.M{"ready": true}})
	if err != nil || result.MatchedCount != 1 {
		fail(w, 500, "Unable to finish photo.")
		return
	}
	success = true
	_, _ = s.db.Collection("direct_uploads").UpdateOne(r.Context(), bson.M{"_id": id, "studio_id": user.ID, "lease": lease}, bson.M{"$set": bson.M{"status": "complete"}, "$unset": bson.M{"lease": "", "lease_until": ""}})
	finished = true
	_ = s.r2.delete(r.Context(), job.ObjectKey)
	reply(w, 200, bson.M{"ok": true, "id": id})
}
