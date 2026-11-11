package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Withdrawal represents a payout request or transaction record.
type Withdrawal struct {
	ID            string `bson:"id" json:"id"`
	StudioID      string `bson:"studio_id" json:"studio_id"`
	Amount        int64  `bson:"amount" json:"amount"`
	Fee           int64  `bson:"fee" json:"fee"`
	Currency      string `bson:"currency" json:"currency"`
	RecipientCode string `bson:"recipient_code" json:"recipient_code"`
	Reference     string `bson:"reference" json:"reference"`
	Status        string `bson:"status" json:"status"` // "pending", "otp", "success", "failed", "reversed"
	BankCode      string `bson:"bank_code" json:"bank_code"`
	Bank          string `bson:"bank,omitempty" json:"bank,omitempty"`
	AccountNumber string `bson:"account_number" json:"account_number"`
	AccountName   string `bson:"account_name" json:"account_name"`
	AccountLast4  string `bson:"account_last4,omitempty" json:"account_last4,omitempty"`
	CreatedAt     int64  `bson:"created_at" json:"created_at"`
	UpdatedAt     int64  `bson:"updated_at" json:"updated_at"`
	Created       string `bson:"created,omitempty" json:"created,omitempty"`
}

type Withdrawals struct {
	collection *mongo.Collection
}

func NewWithdrawals(db *mongo.Database) (*Withdrawals, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Withdrawals{collection: db.Collection("withdrawals")}, nil
}

func (w *Withdrawals) EnsureIndexes(ctx context.Context) error {
	_, err := w.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "reference", Value: 1}}},
	})
	return err
}

func (w *Withdrawals) Create(ctx context.Context, item *Withdrawal) error {
	if item == nil || item.ID == "" || item.StudioID == "" || item.Reference == "" {
		return errors.New("invalid withdrawal record")
	}
	if item.CreatedAt == 0 {
		item.CreatedAt = time.Now().Unix()
	}
	if item.UpdatedAt == 0 {
		item.UpdatedAt = item.CreatedAt
	}
	if item.Created == "" {
		item.Created = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if item.AccountLast4 == "" && len(item.AccountNumber) >= 4 {
		item.AccountLast4 = item.AccountNumber[len(item.AccountNumber)-4:]
	}
	_, err := w.collection.InsertOne(ctx, item)
	return err
}

func (w *Withdrawals) GetByID(ctx context.Context, id string) (*Withdrawal, error) {
	if id == "" {
		return nil, errors.New("withdrawal id required")
	}
	var out Withdrawal
	err := w.collection.FindOne(ctx, bson.M{"id": id}).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (w *Withdrawals) GetByReference(ctx context.Context, reference string) (*Withdrawal, error) {
	if reference == "" {
		return nil, errors.New("withdrawal reference required")
	}
	var out Withdrawal
	err := w.collection.FindOne(ctx, bson.M{"reference": reference}).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (w *Withdrawals) ListByStudio(ctx context.Context, studioID string, limit int64) ([]Withdrawal, error) {
	if studioID == "" {
		return nil, errors.New("studio id required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	cur, err := w.collection.Find(ctx, bson.M{"studio_id": studioID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []Withdrawal
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (w *Withdrawals) ListAll(ctx context.Context, limit int64) ([]Withdrawal, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	cur, err := w.collection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []Withdrawal
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (w *Withdrawals) UpdateStatus(ctx context.Context, reference string, status string, updatedAt int64) error {
	if reference == "" || status == "" {
		return errors.New("reference and status required")
	}
	if updatedAt == 0 {
		updatedAt = time.Now().Unix()
	}
	res, err := w.collection.UpdateOne(ctx, bson.M{"reference": reference}, bson.M{"$set": bson.M{"status": status, "updated_at": updatedAt}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (w *Withdrawals) CountPendingByStudio(ctx context.Context, studioID string) (int64, error) {
	if studioID == "" {
		return 0, errors.New("studio id required")
	}
	return w.collection.CountDocuments(ctx, bson.M{"studio_id": studioID, "status": bson.M{"$in": bson.A{"pending", "otp"}}})
}
