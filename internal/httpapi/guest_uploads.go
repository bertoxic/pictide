package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"pictide.local/studio/internal/media"
	"pictide.local/studio/internal/security"
)

const (
	maxGuestUploadSize  = 10 << 20
	maxGuestUploadFiles = 5
)

// submitGuestUploads accepts a small batch of images for a published gallery.
// The originals remain private until the studio owner approves each submission.
func (s *Server) submitGuestUploads(w http.ResponseWriter, r *http.Request, galleryID string) {
	var gallery struct {
		StudioID string `bson:"studio_id"`
		Status   string `bson:"status"`
		Archived int    `bson:"archived"`
		Allowed  int    `bson:"allow_guest_upload"`
	}
	err := s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": galleryID}).Decode(&gallery)
	if errors.Is(err, mongo.ErrNoDocuments) || gallery.Archived != 0 {
		fail(w, http.StatusNotFound, "Gallery not found.")
		return
	}
	if err != nil {
		fail(w, 500, "Unable to load gallery.")
		return
	}
	if gallery.Status != "Published" || gallery.Allowed != 1 {
		fail(w, http.StatusForbidden, "Guest uploads are not enabled for this gallery.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, int64(maxGuestUploadFiles*maxGuestUploadSize)+(1<<20))
	if err = r.ParseMultipartForm(1 << 20); err != nil {
		fail(w, 400, "Upload is too large or invalid.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["photos"]
	if len(files) == 0 || len(files) > maxGuestUploadFiles {
		fail(w, 400, "Upload between one and five images.")
		return
	}
	token, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to start upload.")
		return
	}
	bucket := s.db.GridFSBucket()
	added := []bson.M{}
	failures := []string{}
	for _, header := range files {
		name := filepath.Base(header.Filename)
		file, openErr := header.Open()
		if openErr != nil {
			failures = append(failures, name+": could not read")
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxGuestUploadSize+1))
		file.Close()
		if readErr != nil || len(data) == 0 || len(data) > maxGuestUploadSize {
			failures = append(failures, name+": file must be an image under 10 MB")
			continue
		}
		assets, processErr := media.Process(data, media.Options{})
		if processErr != nil {
			failures = append(failures, name+": invalid image")
			continue
		}
		id, tokenErr := security.Token()
		if tokenErr != nil {
			failures = append(failures, name+": could not save")
			continue
		}
		keys := []string{id + ":guest-original", id + ":guest-preview"}
		if err = bucket.UploadFromStreamWithID(r.Context(), keys[0], keys[0], bytes.NewReader(data)); err == nil {
			err = bucket.UploadFromStreamWithID(r.Context(), keys[1], keys[1], bytes.NewReader(assets.Preview))
		}
		if err != nil {
			for _, key := range keys {
				_ = bucket.Delete(r.Context(), key)
			}
			failures = append(failures, name+": could not save")
			continue
		}
		doc := bson.M{"id": id, "studio_id": gallery.StudioID, "gallery_id": galleryID, "filename": name, "name": name, "size": len(data), "status": "pending", "token": token, "created": timestamp(), "created_at": time.Now().Unix()}
		if _, err = s.db.Collection("guest_uploads").InsertOne(r.Context(), doc); err != nil {
			for _, key := range keys {
				_ = bucket.Delete(r.Context(), key)
			}
			failures = append(failures, name+": could not save")
			continue
		}
		added = append(added, bson.M{"id": id, "name": name, "status": "pending"})
	}
	if len(added) == 0 {
		fail(w, 400, "No supported image files were uploaded.")
		return
	}
	reply(w, 200, bson.M{"token": token, "added": added, "errors": failures})
}

func (s *Server) listGuestUploads(w http.ResponseWriter, r *http.Request, user *account, galleryID string) {
	owned, err := s.db.Collection("galleries").CountDocuments(r.Context(), bson.M{"id": galleryID, "studio_id": user.ID, "archived": bson.M{"$ne": 1}})
	if err != nil {
		fail(w, 500, "Unable to load submissions.")
		return
	}
	if owned == 0 {
		http.NotFound(w, r)
		return
	}
	cur, err := s.db.Collection("guest_uploads").Find(r.Context(), bson.M{"gallery_id": galleryID, "studio_id": user.ID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		fail(w, 500, "Unable to load submissions.")
		return
	}
	defer cur.Close(r.Context())
	rows := []bson.M{}
	if err = cur.All(r.Context(), &rows); err != nil {
		fail(w, 500, "Unable to load submissions.")
		return
	}
	for _, row := range rows {
		delete(row, "_id")
		delete(row, "token")
		delete(row, "file_id")
	}
	reply(w, 200, rows)
}

func (s *Server) guestUploadPreview(w http.ResponseWriter, r *http.Request, id string) {
	var upload struct {
		ID string `bson:"id"`
	}
	if s.db.Collection("guest_uploads").FindOne(r.Context(), bson.M{"id": id}).Decode(&upload) != nil {
		http.NotFound(w, r)
		return
	}
	var data bytes.Buffer
	if _, err := s.db.GridFSBucket().DownloadToStream(r.Context(), id+":guest-preview", &data); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(data.Bytes())
}

func (s *Server) guestUploadStatus(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Tokens []string `json:"tokens"`
	}
	if decode(r, &input) != nil || len(input.Tokens) > 20 {
		fail(w, 400, "Invalid upload status request.")
		return
	}
	if len(input.Tokens) == 0 {
		reply(w, 200, []bson.M{})
		return
	}
	cur, err := s.db.Collection("guest_uploads").Find(r.Context(), bson.M{"token": bson.M{"$in": input.Tokens}}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		fail(w, 500, "Unable to load upload status.")
		return
	}
	defer cur.Close(r.Context())
	rows := []bson.M{}
	if err = cur.All(r.Context(), &rows); err != nil {
		fail(w, 500, "Unable to load upload status.")
		return
	}
	for _, row := range rows {
		for _, key := range []string{"_id", "token", "studio_id", "gallery_id", "file_id"} {
			delete(row, key)
		}
	}
	reply(w, 200, rows)
}

func (s *Server) reviewGuestUpload(w http.ResponseWriter, r *http.Request, user *account, id string) {
	var input struct {
		Action string `json:"action"`
	}
	if decode(r, &input) != nil || (input.Action != "approve" && input.Action != "reject") {
		fail(w, 400, "Choose approve or reject.")
		return
	}
	var upload struct {
		ID        string `bson:"id"`
		StudioID  string `bson:"studio_id"`
		GalleryID string `bson:"gallery_id"`
		Name      string `bson:"name"`
		Status    string `bson:"status"`
	}
	if err := s.db.Collection("guest_uploads").FindOne(r.Context(), bson.M{"id": id, "studio_id": user.ID}).Decode(&upload); err != nil {
		http.NotFound(w, r)
		return
	}
	if upload.Status != "pending" {
		fail(w, 409, "This submission has already been reviewed.")
		return
	}
	if input.Action == "reject" {
		result, err := s.db.Collection("guest_uploads").UpdateOne(r.Context(), bson.M{"id": id, "studio_id": user.ID, "status": "pending"}, bson.M{"$set": bson.M{"status": "rejected"}})
		if err != nil {
			fail(w, 500, "Unable to review submission.")
			return
		}
		if result.MatchedCount != 1 {
			fail(w, 409, "This submission has already been reviewed.")
			return
		}
		reply(w, 200, bson.M{"ok": true, "status": "rejected"})
		return
	}
	var gallery bson.M
	if err := s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": upload.GalleryID, "studio_id": user.ID, "archived": bson.M{"$ne": 1}}).Decode(&gallery); err != nil {
		http.NotFound(w, r)
		return
	}
	var original bytes.Buffer
	if _, err := s.db.GridFSBucket().DownloadToStream(r.Context(), id+":guest-original", &original); err != nil {
		fail(w, 500, "Unable to read submission.")
		return
	}
	assets, err := media.Process(original.Bytes(), media.Options{})
	if err != nil {
		fail(w, 400, "The submitted image is invalid.")
		return
	}
	bucket := s.db.GridFSBucket()
	hash := sha256.Sum256(original.Bytes())
	photo := bson.M{"id": id, "studio_id": user.ID, "gallery_id": upload.GalleryID, "name": upload.Name, "hash": hex.EncodeToString(hash[:]), "created": timestamp(), "archived": 0, "price": int64(0), "ready": false}
	if _, err = s.db.Collection("photos").InsertOne(r.Context(), photo); err != nil {
		fail(w, 500, "Unable to approve submission.")
		return
	}
	saved := []string{}
	success := false
	defer func() {
		if !success {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for _, key := range saved {
				_ = bucket.Delete(ctx, key)
			}
			_, _ = s.db.Collection("photos").DeleteOne(ctx, bson.M{"id": id, "ready": false})
		}
	}()
	for suffix, body := range map[string][]byte{"original": original.Bytes(), "preview": assets.Preview, "thumb": assets.Thumb, "clean": assets.CleanPreview, "clean_thumb": assets.CleanThumb, "master": assets.Master} {
		key := id + ":" + suffix
		if err = bucket.UploadFromStreamWithID(r.Context(), key, key, bytes.NewReader(body)); err != nil {
			fail(w, 500, "Unable to approve submission.")
			return
		}
		saved = append(saved, key)
	}
	result, err := s.db.Collection("guest_uploads").UpdateOne(r.Context(), bson.M{"id": id, "studio_id": user.ID, "status": "pending"}, bson.M{"$set": bson.M{"status": "approved"}})
	if err != nil || result.MatchedCount != 1 {
		fail(w, 409, "This submission has already been reviewed.")
		return
	}
	if _, err = s.db.Collection("photos").UpdateOne(r.Context(), bson.M{"id": id, "ready": false}, bson.M{"$set": bson.M{"ready": true}}); err != nil {
		fail(w, 500, "Unable to approve submission.")
		return
	}
	success = true
	_ = s.indexFacePhoto(r.Context(), user.ID, upload.GalleryID, id, original.Bytes())
	reply(w, 200, bson.M{"ok": true, "status": "approved", "photo": id})
}
