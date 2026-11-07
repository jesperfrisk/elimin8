package mongodb

import (
	"context"
	"errors"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type GameRepository struct{ games, access *mongo.Collection }

func NewGameRepository(db *mongo.Database) *GameRepository {
	return &GameRepository{db.Collection("games"), db.Collection("game_access")}
}

func (r *GameRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.games.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "invitationCode", Value: 1}}, Options: options.Index().SetUnique(true)})
	if err != nil {
		return err
	}
	_, err = r.access.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)})
	return err
}

func mapError(err error) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return services.ErrNotFound
	}
	if mongo.IsDuplicateKeyError(err) {
		return services.ErrConflict
	}
	return err
}

func (r *GameRepository) Insert(ctx context.Context, g *models.Game) error {
	_, err := r.games.InsertOne(ctx, g)
	return mapError(err)
}

func (r *GameRepository) Get(ctx context.Context, id string) (*models.Game, error) {
	var g models.Game
	err := r.games.FindOne(ctx, bson.M{"_id": id}).Decode(&g)
	return &g, mapError(err)
}

func (r *GameRepository) ByCode(ctx context.Context, code string) (*models.Game, error) {
	var g models.Game
	err := r.games.FindOne(ctx, bson.M{"invitationCode": code}).Decode(&g)
	return &g, mapError(err)
}

func (r *GameRepository) Save(ctx context.Context, g *models.Game, previous int64) error {
	data, err := bson.Marshal(g)
	if err != nil {
		return err
	}
	// Leave room under MongoDB's 16 MiB document limit for future metadata.
	if len(data) > 12*1024*1024 {
		return services.ErrInvalid
	}
	var fields bson.M
	if err = bson.Unmarshal(data, &fields); err != nil {
		return err
	}
	delete(fields, "_id")
	delete(fields, "publishedRevision")
	result, err := r.games.UpdateOne(ctx, bson.M{"_id": g.ID, "revision": previous}, bson.M{"$set": fields})
	if err != nil {
		return mapError(err)
	}
	if result.MatchedCount == 0 {
		return services.ErrConflict
	}
	return nil
}

func (r *GameRepository) PutAccess(ctx context.Context, a models.Access) error {
	_, err := r.access.InsertOne(ctx, a)
	return mapError(err)
}
func (r *GameRepository) GetAccess(ctx context.Context, hash string) (models.Access, error) {
	var a models.Access
	err := r.access.FindOne(ctx, bson.M{"_id": hash}).Decode(&a)
	return a, mapError(err)
}

// Revision and publishedRevision form a coalescing outbox in the same document
// as the state change. Events invalidate views; intermediate events may coalesce.
func (r *GameRepository) Pending(ctx context.Context) ([]models.Game, error) {
	cursor, err := r.games.Find(ctx, bson.M{"$expr": bson.M{"$gt": bson.A{"$revision", "$publishedRevision"}}}, options.Find().SetLimit(100))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var games []models.Game
	err = cursor.All(ctx, &games)
	return games, err
}

func (r *GameRepository) Published(ctx context.Context, id string, revision int64) error {
	_, err := r.games.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$max": bson.M{"publishedRevision": revision}})
	return err
}
