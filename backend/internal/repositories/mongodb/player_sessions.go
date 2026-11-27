package mongodb

import (
	"context"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (r *GameRepository) ensurePlayerSessionIndexes(ctx context.Context) error {
	_, err := r.sessions.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)})
	return err
}

func (r *GameRepository) PutPlayerSession(ctx context.Context, session models.PlayerSession) error {
	_, err := r.sessions.InsertOne(ctx, session)
	return mapError(err)
}

func (r *GameRepository) GetPlayerSession(ctx context.Context, hash string) (models.PlayerSession, error) {
	var session models.PlayerSession
	err := r.sessions.FindOne(ctx, bson.M{"_id": hash}).Decode(&session)
	return session, mapError(err)
}

func (r *GameRepository) DeletePlayerSession(ctx context.Context, hash string) error {
	_, err := r.sessions.DeleteOne(ctx, bson.M{"_id": hash})
	return mapError(err)
}
