package mongodb

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func photoRetentionFilter(endedBefore time.Time, afterID string) bson.M {
	filter := bson.M{"status": "ended", "endedAt": bson.M{"$type": "date", "$lte": endedBefore}}
	if afterID != "" {
		filter["_id"] = bson.M{"$gt": afterID}
	}
	return filter
}

// EnsurePhotoRetentionIndex supports status filtering and ID-ordered paging.
// The cutoff is covered without loading game documents or credentials.
func (r *GameRepository) EnsurePhotoRetentionIndex(ctx context.Context) error {
	_, err := r.games.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "status", Value: 1}, {Key: "_id", Value: 1}, {Key: "endedAt", Value: 1}}})
	return err
}

// DuePhotoGames returns bounded, ordered ID-only pages for every ended game,
// regardless of end reason. Keyset pagination prevents early failures or already
// processed games from starving subsequent games.
func (r *GameRepository) DuePhotoGames(ctx context.Context, endedBefore time.Time, afterID string, limit int) ([]string, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	cursor, err := r.games.Find(ctx, photoRetentionFilter(endedBefore, afterID), options.Find().SetProjection(bson.M{"_id": 1}).SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var games []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &games); err != nil {
		return nil, err
	}
	ids := make([]string, len(games))
	for i, game := range games {
		ids[i] = game.ID
	}
	return ids, nil
}
