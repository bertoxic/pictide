package facesearch

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// FaceRecord stores a detected face in image coordinates with its embedding.
type FaceRecord struct {
	Left      int       `bson:"left" json:"left"`
	Top       int       `bson:"top" json:"top"`
	Right     int       `bson:"right" json:"right"`
	Bottom    int       `bson:"bottom" json:"bottom"`
	Embedding []float32 `bson:"embedding" json:"-"`
}

// PhotoIndex holds one model's face data for one photo.
type PhotoIndex struct {
	StudioID  string       `bson:"studio_id"`
	GalleryID string       `bson:"gallery_id"`
	PhotoID   string       `bson:"photo_id"`
	Model     string       `bson:"model"`
	Faces     []FaceRecord `bson:"faces"`
}

// Index stores face data separately from the public photo documents.
type Index struct{ collection *mongo.Collection }

func NewIndex(db *mongo.Database) *Index { return &Index{collection: db.Collection("face_indexes")} }

// EnsureIndexes prevents multiple records for the same photo and model.
func (i *Index) EnsureIndexes(ctx context.Context) error {
	_, err := i.collection.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "gallery_id", Value: 1}, {Key: "photo_id", Value: 1}, {Key: "model", Value: 1}}, Options: options.Index().SetUnique(true)})
	return err
}

// Put replaces the complete index for one photo and model.
func (i *Index) Put(ctx context.Context, photo PhotoIndex) error {
	if photo.StudioID == "" || photo.GalleryID == "" || photo.PhotoID == "" || photo.Model == "" {
		return errors.New("face index requires studio, gallery, photo and model")
	}
	filter := bson.M{"studio_id": photo.StudioID, "gallery_id": photo.GalleryID, "photo_id": photo.PhotoID, "model": photo.Model}
	_, err := i.collection.ReplaceOne(ctx, filter, photo, options.Replace().SetUpsert(true))
	return err
}

// Gallery retrieves records only for a specific studio, gallery and model.
func (i *Index) Gallery(ctx context.Context, studio, gallery, model string) ([]PhotoIndex, error) {
	if studio == "" || gallery == "" || model == "" {
		return nil, errors.New("face index requires studio, gallery and model")
	}
	cur, err := i.collection.Find(ctx, bson.M{"studio_id": studio, "gallery_id": gallery, "model": model})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var records []PhotoIndex
	if err := cur.All(ctx, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// DeleteGallery removes all biometric data for a studio's gallery.
func (i *Index) DeleteGallery(ctx context.Context, studio, gallery string) error {
	if studio == "" || gallery == "" {
		return errors.New("face index requires studio and gallery")
	}
	_, err := i.collection.DeleteMany(ctx, bson.M{"studio_id": studio, "gallery_id": gallery})
	return err
}

// DeletePhoto removes biometric data once the corresponding photo is removed
// from a gallery's ordinary results.
func (i *Index) DeletePhoto(ctx context.Context, studio, gallery, photo string) error {
	if studio == "" || gallery == "" || photo == "" {
		return errors.New("face index requires studio, gallery and photo")
	}
	_, err := i.collection.DeleteMany(ctx, bson.M{"studio_id": studio, "gallery_id": gallery, "photo_id": photo})
	return err
}
