package mongodb

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/ratrektlabs/rl-agent/memory"
)

type Backend struct {
	client     *mongo.Client
	database   *mongo.Database
	collection *mongo.Collection
	uri        string
	dbName     string
}

type Config struct {
	URI    string
	DBName string
}

func New(cfg Config) *Backend {
	dbName := cfg.DBName
	if dbName == "" {
		dbName = "rlagent"
	}
	return &Backend{
		uri:    cfg.URI,
		dbName: dbName,
	}
}

func NewWithURI(uri string) *Backend {
	return New(Config{URI: uri})
}

func (b *Backend) Connect(ctx context.Context) error {
	clientOpts := options.Client().
		ApplyURI(b.uri).
		SetMaxPoolSize(100).
		SetMinPoolSize(10).
		SetMaxConnIdleTime(time.Minute * 5)

	var err error
	b.client, err = mongo.Connect(ctx, clientOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	if err := b.client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	b.database = b.client.Database(b.dbName)
	b.collection = b.database.Collection("memory_entries")

	if err := b.createIndexes(ctx); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	return nil
}

func (b *Backend) createIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}},
			Options: options.Index().SetName("idx_user_id"),
		},
		{
			Keys:    bson.D{{Key: "session_id", Value: 1}},
			Options: options.Index().SetName("idx_session_id"),
		},
		{
			Keys:    bson.D{{Key: "created_at", Value: -1}},
			Options: options.Index().SetName("idx_created_at"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "session_id", Value: 1}},
			Options: options.Index().SetName("idx_user_session"),
		},
	}

	_, err := b.collection.Indexes().CreateMany(ctx, indexes)
	return err
}

func (b *Backend) Close() error {
	if b.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return b.client.Disconnect(ctx)
	}
	return nil
}

func (b *Backend) Memory() memory.Memory {
	return &mongodbMemory{backend: b}
}

type mongodbMemory struct {
	backend *Backend
}

type mongoEntry struct {
	ID        string                 `bson:"_id"`
	UserID    string                 `bson:"user_id"`
	SessionID string                 `bson:"session_id"`
	Type      memory.EntryType       `bson:"type"`
	Role      string                 `bson:"role"`
	Content   string                 `bson:"content"`
	Metadata  map[string]interface{} `bson:"metadata"`
	CreatedAt time.Time              `bson:"created_at"`
}

func (m *mongodbMemory) Add(ctx context.Context, userID, sessionID string, entry memory.Entry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	if entry.ID == "" {
		entry.ID = generateID()
	}

	doc := mongoEntry{
		ID:        entry.ID,
		UserID:    userID,
		SessionID: sessionID,
		Type:      entry.Type,
		Role:      entry.Role,
		Content:   entry.Content,
		Metadata:  entry.Metadata,
		CreatedAt: entry.Timestamp.UTC(),
	}

	_, err := m.backend.collection.InsertOne(ctx, doc)
	if err != nil {
		return fmt.Errorf("failed to add entry: %w", err)
	}

	return nil
}

func (m *mongodbMemory) Get(ctx context.Context, userID, sessionID string, limit int) ([]memory.Entry, error) {
	if limit <= 0 {
		limit = 100
	}

	filter := bson.M{
		"user_id":    userID,
		"session_id": sessionID,
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := m.backend.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get entries: %w", err)
	}
	defer cursor.Close(ctx)

	var mongoEntries []mongoEntry
	if err := cursor.All(ctx, &mongoEntries); err != nil {
		return nil, fmt.Errorf("failed to decode entries: %w", err)
	}

	entries := make([]memory.Entry, len(mongoEntries))
	for i, me := range mongoEntries {
		entries[i] = memory.Entry{
			ID:        me.ID,
			Type:      me.Type,
			Role:      me.Role,
			Content:   me.Content,
			Metadata:  me.Metadata,
			Timestamp: me.CreatedAt,
		}
	}

	reverseEntries(entries)

	return entries, nil
}

func (m *mongodbMemory) Search(ctx context.Context, userID string, opts memory.SearchOptions) ([]memory.SearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	filter := bson.M{"user_id": userID}

	if opts.Query != "" {
		regex := bson.M{"$regex": opts.Query, "$options": "i"}
		filter["content"] = regex
	}

	if opts.Type != "" {
		filter["type"] = opts.Type
	}

	if !opts.FromDate.IsZero() || !opts.ToDate.IsZero() {
		dateFilter := bson.M{}
		if !opts.FromDate.IsZero() {
			dateFilter["$gte"] = opts.FromDate
		}
		if !opts.ToDate.IsZero() {
			dateFilter["$lte"] = opts.ToDate
		}
		filter["created_at"] = dateFilter
	}

	findOpts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := m.backend.collection.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to search entries: %w", err)
	}
	defer cursor.Close(ctx)

	var mongoEntries []mongoEntry
	if err := cursor.All(ctx, &mongoEntries); err != nil {
		return nil, fmt.Errorf("failed to decode entries: %w", err)
	}

	results := make([]memory.SearchResult, len(mongoEntries))
	for i, me := range mongoEntries {
		score := calculateScore(me.Content, opts.Query)
		results[i] = memory.SearchResult{
			Entry: memory.Entry{
				ID:        me.ID,
				Type:      me.Type,
				Role:      me.Role,
				Content:   me.Content,
				Metadata:  me.Metadata,
				Timestamp: me.CreatedAt,
			},
			Score: score,
		}
	}

	return results, nil
}

func (m *mongodbMemory) Clear(ctx context.Context, userID, sessionID string) error {
	filter := bson.M{
		"user_id":    userID,
		"session_id": sessionID,
	}

	_, err := m.backend.collection.DeleteMany(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to clear entries: %w", err)
	}

	return nil
}

func calculateScore(content, query string) float64 {
	if content == "" || query == "" {
		return 1.0
	}

	contentLower := toLower(content)
	queryLower := toLower(query)

	if contentLower == queryLower {
		return 1.0
	}

	if contains(contentLower, queryLower) {
		return 0.8
	}

	return 0.5
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func reverseEntries(entries []memory.Entry) {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
}

func generateID() string {
	return time.Now().Format("20060102150405") + randomSuffix(6)
}

func randomSuffix(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	now := time.Now().UnixNano()
	for i := range b {
		b[i] = letters[(now+int64(i))%int64(len(letters))]
	}
	return string(b)
}
