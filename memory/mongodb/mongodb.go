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
	client      *mongo.Client
	database    *mongo.Database
	collection  *mongo.Collection
	archiveColl *mongo.Collection
	uri         string
	dbName      string
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
	b.archiveColl = b.database.Collection("memory_entries_archive")

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
	if err != nil {
		return err
	}

	archiveIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "session_id", Value: 1}},
			Options: options.Index().SetName("idx_archive_user_session"),
		},
		{
			Keys:    bson.D{{Key: "archived_at", Value: -1}},
			Options: options.Index().SetName("idx_archived_at"),
		},
	}

	_, err = b.archiveColl.Indexes().CreateMany(ctx, archiveIndexes)
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

func (m *mongodbMemory) GetAll(ctx context.Context, userID, sessionID string) ([]memory.Entry, error) {
	filter := bson.M{
		"user_id":    userID,
		"session_id": sessionID,
	}

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})

	cursor, err := m.backend.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get all entries: %w", err)
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

	return entries, nil
}

func (m *mongodbMemory) Count(ctx context.Context, userID, sessionID string) (int, error) {
	filter := bson.M{
		"user_id":    userID,
		"session_id": sessionID,
	}

	count, err := m.backend.collection.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to count entries: %w", err)
	}

	return int(count), nil
}

func (m *mongodbMemory) Compact(ctx context.Context, userID, sessionID string, opts memory.CompactionOptions) (*memory.CompactionStats, error) {
	start := time.Now()
	stats := &memory.CompactionStats{Strategy: opts.Strategy}

	entries, err := m.GetAll(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}

	stats.EntriesBefore = len(entries)
	stats.BytesBefore = calculateTotalBytes(entries)

	if len(entries) == 0 {
		stats.EntriesAfter = 0
		stats.Duration = time.Since(start)
		return stats, nil
	}

	keepRecent := opts.KeepRecent
	if keepRecent <= 0 {
		keepRecent = opts.MaxEntries
	}
	if keepRecent <= 0 {
		keepRecent = len(entries)
	}

	var toCompact []memory.Entry
	var toKeep []memory.Entry

	if keepRecent < len(entries) {
		toKeep = entries[len(entries)-keepRecent:]
		toCompact = entries[:len(entries)-keepRecent]
	} else {
		toKeep = entries
		toCompact = []memory.Entry{}
	}

	if opts.MaxAge > 0 {
		cutoff := time.Now().Add(-opts.MaxAge)
		var filteredKeep []memory.Entry
		for _, e := range toKeep {
			if e.Timestamp.After(cutoff) {
				filteredKeep = append(filteredKeep, e)
			} else {
				toCompact = append(toCompact, e)
			}
		}
		toKeep = filteredKeep
	}

	if len(toCompact) == 0 {
		stats.EntriesAfter = len(toKeep)
		stats.BytesAfter = calculateTotalBytes(toKeep)
		stats.Duration = time.Since(start)
		return stats, nil
	}

	if opts.DryRun {
		stats.EntriesAfter = len(toKeep)
		stats.EntriesRemoved = len(toCompact)
		stats.BytesAfter = calculateTotalBytes(toKeep)
		stats.BytesSaved = stats.BytesBefore - stats.BytesAfter
		stats.Duration = time.Since(start)
		return stats, nil
	}

	switch opts.Strategy {
	case memory.CompactionStrategyTruncate:
		var idsToDelete []string
		for _, e := range toCompact {
			idsToDelete = append(idsToDelete, e.ID)
		}
		_, err = m.backend.collection.DeleteMany(ctx, bson.M{
			"user_id":    userID,
			"session_id": sessionID,
			"_id":        bson.M{"$in": idsToDelete},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to truncate entries: %w", err)
		}
		stats.EntriesRemoved = len(toCompact)

	case memory.CompactionStrategySummarize:
		if len(toCompact) > 0 && opts.LLMProvider != nil {
			prompt := opts.SummarizePrompt
			if prompt == "" {
				prompt = "Summarize the following conversation history concisely, preserving key information and decisions:"
			}
			summary, err := opts.LLMProvider.Summarize(ctx, toCompact, prompt)
			if err != nil {
				return nil, fmt.Errorf("failed to summarize entries: %w", err)
			}

			var idsToDelete []string
			for _, e := range toCompact {
				idsToDelete = append(idsToDelete, e.ID)
			}
			_, err = m.backend.collection.DeleteMany(ctx, bson.M{
				"user_id":    userID,
				"session_id": sessionID,
				"_id":        bson.M{"$in": idsToDelete},
			})
			if err != nil {
				return nil, fmt.Errorf("failed to delete entries after summary: %w", err)
			}

			summaryDoc := mongoEntry{
				ID:        generateID(),
				UserID:    userID,
				SessionID: sessionID,
				Type:      memory.EntryTypeSummary,
				Role:      "system",
				Content:   summary,
				CreatedAt: time.Now().UTC(),
				Metadata: map[string]interface{}{
					"compacted_entries": len(toCompact),
					"compaction_time":   time.Now().Format(time.RFC3339),
				},
			}
			_, err = m.backend.collection.InsertOne(ctx, summaryDoc)
			if err != nil {
				return nil, fmt.Errorf("failed to add summary entry: %w", err)
			}
			stats.EntriesSummary = len(toCompact)
		} else {
			var idsToDelete []string
			for _, e := range toCompact {
				idsToDelete = append(idsToDelete, e.ID)
			}
			_, err = m.backend.collection.DeleteMany(ctx, bson.M{
				"user_id":    userID,
				"session_id": sessionID,
				"_id":        bson.M{"$in": idsToDelete},
			})
			if err != nil {
				return nil, fmt.Errorf("failed to truncate entries: %w", err)
			}
			stats.EntriesRemoved = len(toCompact)
		}

	case memory.CompactionStrategyArchive:
		var archiveDocs []interface{}
		var idsToDelete []string
		archivedAt := time.Now().UTC()

		for _, e := range toCompact {
			archiveDocs = append(archiveDocs, bson.M{
				"_id":         e.ID,
				"user_id":     userID,
				"session_id":  sessionID,
				"type":        e.Type,
				"role":        e.Role,
				"content":     e.Content,
				"metadata":    e.Metadata,
				"created_at":  e.Timestamp.UTC(),
				"archived_at": archivedAt,
			})
			idsToDelete = append(idsToDelete, e.ID)
		}

		if len(archiveDocs) > 0 {
			_, err = m.backend.archiveColl.InsertMany(ctx, archiveDocs)
			if err != nil {
				return nil, fmt.Errorf("failed to archive entries: %w", err)
			}

			_, err = m.backend.collection.DeleteMany(ctx, bson.M{
				"user_id":    userID,
				"session_id": sessionID,
				"_id":        bson.M{"$in": idsToDelete},
			})
			if err != nil {
				return nil, fmt.Errorf("failed to delete archived entries: %w", err)
			}
		}
		stats.EntriesArchived = len(toCompact)

	default:
		var idsToDelete []string
		for _, e := range toCompact {
			idsToDelete = append(idsToDelete, e.ID)
		}
		_, err = m.backend.collection.DeleteMany(ctx, bson.M{
			"user_id":    userID,
			"session_id": sessionID,
			"_id":        bson.M{"$in": idsToDelete},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to truncate entries: %w", err)
		}
		stats.EntriesRemoved = len(toCompact)
	}

	newCount, _ := m.Count(ctx, userID, sessionID)
	stats.EntriesAfter = newCount

	keptEntries, _ := m.GetAll(ctx, userID, sessionID)
	stats.BytesAfter = calculateTotalBytes(keptEntries)
	stats.BytesSaved = stats.BytesBefore - stats.BytesAfter
	stats.Duration = time.Since(start)

	return stats, nil
}

func calculateTotalBytes(entries []memory.Entry) int64 {
	var total int64
	for _, e := range entries {
		total += int64(len(e.Content))
	}
	return total
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
