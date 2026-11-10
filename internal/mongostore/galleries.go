package mongostore

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Galleries scopes every operation to one studio. Gallery IDs are globally
// unique in the existing application, but studio scoping is required for isolation.
type Galleries struct {
	collection *mongo.Collection
	studioID   string
}

func NewGalleries(db *mongo.Database, studioID string) (*Galleries, error) {
	if db == nil || studioID == "" {
		return nil, errors.New("database and studio ID are required")
	}
	return &Galleries{collection: db.Collection("galleries"), studioID: studioID}, nil
}

// EnsureIndexes must run during data-store initialization, before accepting writes.
func (g *Galleries) EnsureIndexes(ctx context.Context) error {
	_, err := g.collection.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)})
	return err
}

func (g *Galleries) Create(ctx context.Context, id string, fields bson.M) error {
	if id == "" {
		return errors.New("gallery ID is required")
	}
	doc := make(bson.M, len(fields)+2)
	for key, value := range fields {
		if key == "_id" || key == "id" || key == "studio_id" {
			return fmt.Errorf("reserved gallery field: %s", key)
		}
		doc[key] = value
	}
	doc["id"], doc["studio_id"] = id, g.studioID
	_, err := g.collection.InsertOne(ctx, doc)
	return err
}

func (g *Galleries) Get(ctx context.Context, id string) (bson.M, error) {
	var doc bson.M
	err := g.collection.FindOne(ctx, bson.M{"studio_id": g.studioID, "id": id}).Decode(&doc)
	if err != nil {
		return nil, err
	}
	delete(doc, "_id")
	delete(doc, "studio_id")
	return doc, nil
}

// Update changes only explicitly supplied fields and cannot move a gallery to another studio.
func (g *Galleries) Update(ctx context.Context, id string, fields bson.M) error {
	if id == "" || len(fields) == 0 {
		return errors.New("gallery ID and fields are required")
	}
	for key := range fields {
		if key == "_id" || key == "id" || key == "studio_id" || len(key) == 0 || key[0] == '$' {
			return fmt.Errorf("invalid gallery field: %s", key)
		}
	}
	result, err := g.collection.UpdateOne(ctx, bson.M{"studio_id": g.studioID, "id": id}, bson.M{"$set": fields})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}
