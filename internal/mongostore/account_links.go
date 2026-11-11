package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// AccountLink represents an ephemeral verification or password reset token.
type AccountLink struct {
	Token     string    `bson:"token" json:"token"`
	AccountID string    `bson:"account_id" json:"account_id"`
	Purpose   string    `bson:"purpose" json:"purpose"` // "verify_email", "password_reset"
	ExpiresAt time.Time `bson:"expires_at" json:"expires_at"`
	CreatedAt time.Time `bson:"created_at,omitempty" json:"created_at,omitempty"`
}

type AccountLinks struct {
	collection *mongo.Collection
}

func NewAccountLinks(db *mongo.Database) (*AccountLinks, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &AccountLinks{collection: db.Collection("account_links")}, nil
}

func (a *AccountLinks) EnsureIndexes(ctx context.Context) error {
	_, err := a.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "token", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
	})
	return err
}

func (a *AccountLinks) Create(ctx context.Context, item *AccountLink) error {
	if item == nil || item.Token == "" || item.AccountID == "" || item.Purpose == "" || item.ExpiresAt.IsZero() {
		return errors.New("invalid account link record")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	_, err := a.collection.InsertOne(ctx, item)
	return err
}

func (a *AccountLinks) GetByToken(ctx context.Context, token string) (*AccountLink, error) {
	if token == "" {
		return nil, errors.New("token required")
	}
	var out AccountLink
	err := a.collection.FindOne(ctx, bson.M{"token": token}).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *AccountLinks) GetValid(ctx context.Context, token string, purpose string) (*AccountLink, error) {
	if token == "" || purpose == "" {
		return nil, errors.New("token and purpose required")
	}
	var out AccountLink
	filter := bson.M{
		"token":      token,
		"purpose":    purpose,
		"expires_at": bson.M{"$gt": time.Now()},
	}
	err := a.collection.FindOne(ctx, filter).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *AccountLinks) DeleteByToken(ctx context.Context, token string) error {
	if token == "" {
		return errors.New("token required")
	}
	_, err := a.collection.DeleteOne(ctx, bson.M{"token": token})
	return err
}

func (a *AccountLinks) DeleteByAccountAndPurpose(ctx context.Context, accountID string, purpose string) error {
	if accountID == "" || purpose == "" {
		return errors.New("account id and purpose required")
	}
	_, err := a.collection.DeleteMany(ctx, bson.M{"account_id": accountID, "purpose": purpose})
	return err
}
