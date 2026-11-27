package mongodb

import (
	"context"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
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

// Keep the first revocation marker until the original TTL deadline. This lets
// a retry finish the revision fence after a crash or failed game save.
func (r *GameRepository) RevokePlayerSession(ctx context.Context, hash string, now time.Time) error {
	_, err := r.sessions.UpdateOne(ctx, bson.M{"_id": hash, "revokedAt": nil}, bson.M{"$set": bson.M{"revokedAt": now}})
	return mapError(err)
}

func (r *GameRepository) BindAccessSession(ctx context.Context, accessHash, sessionHash string) error {
	result, err := r.access.UpdateOne(ctx, bson.M{"_id": accessHash, "sessionHash": bson.M{"$in": bson.A{nil, ""}}}, bson.M{"$set": bson.M{"sessionHash": sessionHash}})
	if err != nil {
		return mapError(err)
	}
	if result.MatchedCount == 0 {
		return services.ErrUnauthorized
	}
	return nil
}
