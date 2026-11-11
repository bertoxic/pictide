package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// GuestUpload represents a photo contributed by a gallery guest.
type GuestUpload struct {
	ID        string        `bson:"id" json:"id"`
	StudioID  string        `bson:"studio_id" json:"studio_id"`
	GalleryID string        `bson:"gallery_id" json:"gallery_id"`
	GuestName string        `bson:"guest_name" json:"guest_name"`
	Filename  string        `bson:"filename" json:"filename"`
	Name      string        `bson:"name,omitempty" json:"name,omitempty"`
	Size      int64         `bson:"size" json:"size"`
	Status    string        `bson:"status" json:"status"` // "pending", "approved", "rejected"
	FileID    bson.ObjectID `bson:"file_id,omitempty" json:"file_id,omitempty"`
	CreatedAt int64         `bson:"created_at" json:"created_at"`
	Created   string        `bson:"created,omitempty" json:"created,omitempty"`
}

type GuestUploads struct {
	collection *mongo.Collection
}

func NewGuestUploads(db *mongo.Database) (*GuestUploads, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &GuestUploads{collection: db.Collection("guest_uploads")}, nil
}

func (g *GuestUploads) EnsureIndexes(ctx context.Context) error {
	_, err := g.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "gallery_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "studio_id", Value: 1}}},
	})
	return err
}

func (g *GuestUploads) Create(ctx context.Context, item *GuestUpload) error {
	if item == nil || item.ID == "" || item.StudioID == "" || item.GalleryID == "" {
		return errors.New("invalid guest upload record")
	}
	if item.CreatedAt == 0 {
		item.CreatedAt = time.Now().Unix()
	}
	if item.Created == "" {
		item.Created = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if item.Name == "" {
		item.Name = item.Filename
	}
	if item.Status == "" {
		item.Status = "pending"
	}
	_, err := g.collection.InsertOne(ctx, item)
	return err
}

func (g *GuestUploads) GetByID(ctx context.Context, id string) (*GuestUpload, error) {
	if id == "" {
		return nil, errors.New("upload id required")
	}
	var out GuestUpload
	err := g.collection.FindOne(ctx, bson.M{"id": id}).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *GuestUploads) ListByGallery(ctx context.Context, galleryID string) ([]GuestUpload, error) {
	if galleryID == "" {
		return nil, errors.New("gallery id required")
	}
	cur, err := g.collection.Find(ctx, bson.M{"gallery_id": galleryID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []GuestUpload
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (g *GuestUploads) ListByStudio(ctx context.Context, studioID string) ([]GuestUpload, error) {
	if studioID == "" {
		return nil, errors.New("studio id required")
	}
	cur, err := g.collection.Find(ctx, bson.M{"studio_id": studioID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []GuestUpload
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (g *GuestUploads) ListByIDs(ctx context.Context, ids []string) ([]GuestUpload, error) {
	if len(ids) == 0 {
		return []GuestUpload{}, nil
	}
	cur, err := g.collection.Find(ctx, bson.M{"id": bson.M{"$in": ids}}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []GuestUpload
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (g *GuestUploads) UpdateStatus(ctx context.Context, id string, status string) error {
	if id == "" || status == "" {
		return errors.New("id and status required")
	}
	res, err := g.collection.UpdateOne(ctx, bson.M{"id": id}, bson.M{"$set": bson.M{"status": status}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (g *GuestUploads) CountPending(ctx context.Context) (int64, error) {
	return g.collection.CountDocuments(ctx, bson.M{"status": "pending"})
}
