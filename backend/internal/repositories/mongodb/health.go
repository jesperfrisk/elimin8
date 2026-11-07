package mongodb

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type HealthRepository struct {
	db *mongo.Database
}

func NewHealthRepository(db *mongo.Database) *HealthRepository {
	return &HealthRepository{db: db}
}

func (r *HealthRepository) Ping(ctx context.Context) error {
	return r.db.Client().Ping(ctx, readpref.Primary())
}
