package httpapi

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"sort"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	_ "golang.org/x/image/webp"
	"pictide.local/studio/internal/facesearch"
)

const maxFaceSearchImageSize = 10 << 20

// indexFacePhoto stores embeddings only after a photo has been committed and
// made ready. It deliberately receives original bytes rather than a public
// preview, so watermarking cannot influence recognition quality.
func (s *Server) indexFacePhoto(ctx context.Context, studioID, galleryID, photoID string, data []byte) error {
	if s.faceDetector == nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	detections, err := s.faceDetector.Detect(img)
	if err != nil {
		return err
	}
	faces := make([]facesearch.FaceRecord, 0, len(detections))
	for _, detection := range detections {
		if len(detection.Embedding) == 0 {
			continue
		}
		r := detection.Rectangle
		faces = append(faces, facesearch.FaceRecord{Left: r.Min.X, Top: r.Min.Y, Right: r.Max.X, Bottom: r.Max.Y, Embedding: detection.Embedding})
	}
	return s.faceIndex.Put(ctx, facesearch.PhotoIndex{StudioID: studioID, GalleryID: galleryID, PhotoID: photoID, Model: s.cfg.FaceModel, Faces: faces})
}

func (s *Server) faceSearchEnabled(w http.ResponseWriter) bool {
	if s.faceDetector != nil {
		return true
	}
	fail(w, http.StatusConflict, "Face search is not enabled for this server.")
	return false
}

func faceImageFromRequest(w http.ResponseWriter, r *http.Request) (image.Image, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFaceSearchImageSize)
	if err := r.ParseMultipartForm(maxFaceSearchImageSize); err != nil {
		fail(w, http.StatusBadRequest, "Choose an image no larger than 10 MB.")
		return nil, false
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("selfie")
	if err != nil {
		file, _, err = r.FormFile("photo")
	}
	if err != nil {
		fail(w, http.StatusBadRequest, "Choose a selfie to search this gallery.")
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFaceSearchImageSize+1))
	if err != nil || len(data) > maxFaceSearchImageSize {
		fail(w, http.StatusBadRequest, "Choose an image no larger than 10 MB.")
		return nil, false
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		fail(w, http.StatusBadRequest, "The selfie must be a supported image.")
		return nil, false
	}
	return img, true
}

func (s *Server) faceSearch(w http.ResponseWriter, r *http.Request, user *account, galleryID string) {
	if !s.faceSearchEnabled(w) {
		return
	}
	var gallery struct {
		StudioID string `bson:"studio_id"`
		Status   string `bson:"status"`
		Archived int    `bson:"archived"`
	}
	if err := s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": galleryID, "archived": bson.M{"$ne": 1}}).Decode(&gallery); err != nil {
		http.NotFound(w, r)
		return
	}
	if (user == nil || user.ID != gallery.StudioID) && gallery.Status != "Published" {
		http.NotFound(w, r)
		return
	}
	img, ok := faceImageFromRequest(w, r)
	if !ok {
		return
	}
	queries, err := s.faceDetector.Detect(img)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "We could not read a face from that image.")
		return
	}
	records, err := s.faceIndex.Gallery(r.Context(), gallery.StudioID, galleryID, s.cfg.FaceModel)
	if err != nil {
		fail(w, http.StatusInternalServerError, "Unable to search this gallery.")
		return
	}
	best := map[string]float64{}
	for _, query := range queries {
		for _, record := range records {
			for _, candidate := range record.Faces {
				score, valid := facesearch.Similarity(query.Embedding, candidate.Embedding)
				if !valid || score < s.cfg.FaceThreshold {
					continue
				}
				if previous, exists := best[record.PhotoID]; !exists || score > previous {
					best[record.PhotoID] = score
				}
			}
		}
	}
	if len(best) == 0 {
		reply(w, http.StatusOK, bson.M{"matches": []string{}})
		return
	}
	ids := make([]string, 0, len(best))
	for id := range best {
		ids = append(ids, id)
	}
	// Do not return archived or interrupted uploads even if an old index exists.
	cur, err := s.db.Collection("photos").Find(r.Context(), bson.M{"id": bson.M{"$in": ids}, "studio_id": gallery.StudioID, "gallery_id": galleryID, "ready": true, "archived": bson.M{"$ne": 1}}, options.Find().SetProjection(bson.M{"id": 1}))
	if err != nil {
		fail(w, http.StatusInternalServerError, "Unable to search this gallery.")
		return
	}
	var photos []struct {
		ID string `bson:"id"`
	}
	err = cur.All(r.Context(), &photos)
	cur.Close(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, "Unable to search this gallery.")
		return
	}
	valid := make(map[string]bool, len(photos))
	for _, photo := range photos {
		valid[photo.ID] = true
	}
	ids = ids[:0]
	for id := range best {
		if valid[id] {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		return best[ids[i]] > best[ids[j]] || best[ids[i]] == best[ids[j]] && ids[i] < ids[j]
	})
	reply(w, http.StatusOK, bson.M{"matches": ids})
}

func (s *Server) storedOriginal(ctx context.Context, studioID, photoID string, r2Original bool) ([]byte, error) {
	if r2Original {
		if s.r2 == nil {
			return nil, errors.New("R2 original is unavailable")
		}
		stream, err := s.r2.open(ctx, r2PhotoKey(studioID, photoID, "original"))
		if err != nil {
			return nil, err
		}
		defer stream.Close()
		return io.ReadAll(io.LimitReader(stream, maxPhotoSize+1))
	}
	var buf bytes.Buffer
	_, err := s.db.GridFSBucket().DownloadToStream(ctx, photoID+":original", &buf)
	return buf.Bytes(), err
}

// backfillFaceIndex allows a studio to index historic ready photos without
// exposing originals or embeddings to a browser.
func (s *Server) backfillFaceIndex(w http.ResponseWriter, r *http.Request, user *account, galleryID string) {
	if !s.faceSearchEnabled(w) {
		return
	}
	if s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": galleryID, "studio_id": user.ID, "archived": bson.M{"$ne": 1}}).Err() != nil {
		http.NotFound(w, r)
		return
	}
	cur, err := s.db.Collection("photos").Find(r.Context(), bson.M{"studio_id": user.ID, "gallery_id": galleryID, "ready": true, "archived": bson.M{"$ne": 1}}, options.Find().SetProjection(bson.M{"id": 1, "r2_original": 1}))
	if err != nil {
		fail(w, http.StatusInternalServerError, "Unable to load gallery photos.")
		return
	}
	defer cur.Close(r.Context())
	var photos []struct {
		ID         string `bson:"id"`
		R2Original bool   `bson:"r2_original"`
	}
	if err := cur.All(r.Context(), &photos); err != nil {
		fail(w, http.StatusInternalServerError, "Unable to load gallery photos.")
		return
	}
	indexed, failures := 0, []string{}
	for _, photo := range photos {
		data, err := s.storedOriginal(r.Context(), user.ID, photo.ID, photo.R2Original)
		if err == nil {
			err = s.indexFacePhoto(r.Context(), user.ID, galleryID, photo.ID, data)
		}
		if err != nil {
			failures = append(failures, photo.ID)
			continue
		}
		indexed++
	}
	reply(w, http.StatusOK, bson.M{"indexed": indexed, "errors": failures})
}
