package database

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Connect verifies MongoDB is reachable before the API starts accepting requests.
func Connect(ctx context.Context, uri, name string) (*mongo.Database, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(5 * time.Second))
	if err != nil {
		return nil, fmt.Errorf("create MongoDB client: %w", err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(cleanupCtx)
		return nil, fmt.Errorf("ping MongoDB: %w", err)
	}
	return client.Database(name), nil
}
