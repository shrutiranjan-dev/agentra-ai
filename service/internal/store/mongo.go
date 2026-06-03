package store

import (
	"context"
	"time"

	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoStore struct {
	client      *mongo.Client
	db          *mongo.Database
	sessions    *mongo.Collection
	messages    *mongo.Collection
	permissions *mongo.Collection
	toolRuns    *mongo.Collection
	settings    *mongo.Collection
}

func NewMongoStore(ctx context.Context, uri, database string) (*MongoStore, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	db := client.Database(database)
	store := &MongoStore{
		client:      client,
		db:          db,
		sessions:    db.Collection("sessions"),
		messages:    db.Collection("messages"),
		permissions: db.Collection("permissions"),
		toolRuns:    db.Collection("toolRuns"),
		settings:    db.Collection("settings"),
	}

	_, _ = store.sessions.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "updatedAt", Value: -1}},
	})
	_, _ = store.messages.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "sessionId", Value: 1}, {Key: "createdAt", Value: 1}},
	})

	return store, nil
}

func (s *MongoStore) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

func (s *MongoStore) SaveSession(ctx context.Context, session domain.Session) error {
	session.UpdatedAt = time.Now().UTC()
	_, err := s.sessions.ReplaceOne(
		ctx,
		bson.M{"_id": session.ID},
		session,
		options.Replace().SetUpsert(true),
	)
	return err
}

func (s *MongoStore) ListSessions(ctx context.Context) ([]domain.Session, error) {
	cursor, err := s.sessions.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "updatedAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var items []domain.Session
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []domain.Session{}
	}
	return items, nil
}

func (s *MongoStore) SaveMessage(ctx context.Context, message domain.Message) error {
	message.UpdatedAt = time.Now().UTC()
	_, err := s.messages.ReplaceOne(
		ctx,
		bson.M{"_id": message.ID},
		message,
		options.Replace().SetUpsert(true),
	)
	return err
}

func (s *MongoStore) ListMessages(ctx context.Context, sessionID string) ([]domain.Message, error) {
	cursor, err := s.messages.Find(ctx, bson.M{"sessionId": sessionID}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var items []domain.Message
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []domain.Message{}
	}
	return items, nil
}

func (s *MongoStore) SavePermission(ctx context.Context, request domain.PermissionRequest) error {
	_, err := s.permissions.ReplaceOne(ctx, bson.M{"_id": request.ID}, request, options.Replace().SetUpsert(true))
	return err
}

func (s *MongoStore) UpdatePermissionStatus(ctx context.Context, requestID, status string) error {
	_, err := s.permissions.UpdateByID(ctx, requestID, bson.M{"$set": bson.M{"status": status}})
	return err
}

func (s *MongoStore) ListPendingPermissions(ctx context.Context, requestID string) ([]domain.PermissionRequest, error) {
	cursor, err := s.permissions.Find(ctx, bson.M{"_id": requestID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var items []domain.PermissionRequest
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []domain.PermissionRequest{}
	}
	return items, nil
}

func (s *MongoStore) SaveToolRun(ctx context.Context, run domain.ToolExecution) error {
	_, err := s.toolRuns.ReplaceOne(ctx, bson.M{"_id": run.ID}, run, options.Replace().SetUpsert(true))
	return err
}

func (s *MongoStore) UpsertSetting(ctx context.Context, setting domain.Setting) error {
	_, err := s.settings.ReplaceOne(ctx, bson.M{"_id": setting.Key}, setting, options.Replace().SetUpsert(true))
	return err
}
