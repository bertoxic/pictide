package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// AdminAuditEvent represents an administrative action or access event.
type AdminAuditEvent struct {
	ID        string `bson:"id" json:"id"`
	Actor     string `bson:"actor,omitempty" json:"actor,omitempty"`
	Action    string `bson:"action" json:"action"`
	Target    string `bson:"target,omitempty" json:"target,omitempty"`
	Details   string `bson:"details,omitempty" json:"details,omitempty"`
	CreatedAt int64  `bson:"created_at" json:"created_at"`
	Created   string `bson:"created,omitempty" json:"created,omitempty"`
}

type AdminAudit struct {
	collection *mongo.Collection
}

func NewAdminAudit(db *mongo.Database) (*AdminAudit, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &AdminAudit{collection: db.Collection("admin_audit")}, nil
}

func (a *AdminAudit) EnsureIndexes(ctx context.Context) error {
	_, err := a.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "created_at", Value: -1}}},
	})
	return err
}

func (a *AdminAudit) Record(ctx context.Context, event *AdminAuditEvent) error {
	if event == nil || event.ID == "" || event.Action == "" {
		return errors.New("invalid audit event record")
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = time.Now().Unix()
	}
	if event.Created == "" {
		event.Created = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := a.collection.InsertOne(ctx, event)
	return err
}

func (a *AdminAudit) ListRecent(ctx context.Context, limit int64) ([]AdminAuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cur, err := a.collection.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 0}).SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []AdminAuditEvent
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}
