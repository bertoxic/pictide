package mongostore

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

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

// GalleryPage describes a page of non-archived galleries for one studio.
type GalleryPage struct {
	Items  []bson.M
	Total  int64
	Limit  int64
	Offset int64
}

// List retrieves a bounded page, ordered by creation time, with optional
// literal case-insensitive search and status filtering.
func (g *Galleries) List(ctx context.Context, search, status string, limit, offset int64) (GalleryPage, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return GalleryPage{}, errors.New("invalid gallery pagination")
	}
	if status != "" && status != "Draft" && status != "Published" {
		return GalleryPage{}, errors.New("invalid gallery status")
	}
	search = strings.TrimSpace(search)
	if len(search) > 200 {
		return GalleryPage{}, errors.New("gallery search is too long")
	}
	filter := bson.M{"studio_id": g.studioID, "archived": bson.M{"$ne": 1}}
	if status != "" {
		filter["status"] = status
	}
	if search != "" {
		pattern := bson.Regex{Pattern: regexp.QuoteMeta(search), Options: "i"}
		filter["$or"] = bson.A{bson.M{"name": pattern}, bson.M{"client": pattern}, bson.M{"location": pattern}}
	}
	total, err := g.collection.CountDocuments(ctx, filter)
	if err != nil {
		return GalleryPage{}, err
	}
	cursor, err := g.collection.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "created", Value: -1}, {Key: "id", Value: -1}}).SetLimit(limit).SetSkip(offset))
	if err != nil {
		return GalleryPage{}, err
	}
	defer cursor.Close(ctx)
	items := []bson.M{}
	if err := cursor.All(ctx, &items); err != nil {
		return GalleryPage{}, err
	}
	for _, doc := range items {
		delete(doc, "_id")
		delete(doc, "studio_id")
	}
	return GalleryPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// Archive removes a gallery from ordinary listings while keeping its record.
func (g *Galleries) Archive(ctx context.Context, id string) error {
	return g.Update(ctx, id, bson.M{"archived": 1})
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
