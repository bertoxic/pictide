// Package mongostore contains the MongoDB connection for the planned data-store migration.
package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Connect validates connectivity before returning a client. Callers own the client
// and must disconnect it when finished. Do not include the URI in error messages.
func Connect(ctx context.Context, uri string) (*mongo.Client, error) {
	if uri == "" {
		return nil, errors.New("MONGODB_URI is required")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(10 * time.Second))
	if err != nil {
		return nil, errors.New("invalid MongoDB configuration")
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, errors.New("MongoDB connection failed")
	}
	return client, nil
}
