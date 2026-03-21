package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Model struct {
	ID          int64     `json:"id"`
	Provider    string    `json:"provider"`
	ModelID     string    `json:"model_id"`
	DisplayName string    `json:"display_name"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
}

type ModelDB struct {
	db *sql.DB
}

var modelDB *ModelDB

func InitModelDB(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS models (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
			model_id TEXT NOT NULL,
			display_name TEXT NOT NULL,
			is_default BOOLEAN DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(provider, model_id)
		)
	`)
	if err != nil {
		db.Close()
		return fmt.Errorf("failed to create table: %w", err)
	}

	modelDB = &ModelDB{db: db}

	if err := modelDB.seedIfEmpty(); err != nil {
		db.Close()
		return fmt.Errorf("failed to seed database: %w", err)
	}

	return nil
}

func (mdb *ModelDB) seedIfEmpty() error {
	var count int
	err := mdb.db.QueryRow("SELECT COUNT(*) FROM models").Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	return mdb.SeedDefaults()
}

func (mdb *ModelDB) SeedDefaults() error {
	defaultModels := []Model{
		{Provider: "openai", ModelID: "gpt-4.5-turbo", DisplayName: "GPT-4.5 Turbo", IsDefault: true},
		{Provider: "openai", ModelID: "gpt-4o", DisplayName: "GPT-4o", IsDefault: false},
		{Provider: "openai", ModelID: "gpt-4o-mini", DisplayName: "GPT-4o Mini", IsDefault: false},
		{Provider: "openai", ModelID: "o3-mini", DisplayName: "O3 Mini", IsDefault: false},
		{Provider: "openai", ModelID: "o1-mini", DisplayName: "O1 Mini", IsDefault: false},

		{Provider: "anthropic", ModelID: "claude-3.7-sonnet", DisplayName: "Claude 3.7 Sonnet", IsDefault: true},
		{Provider: "anthropic", ModelID: "claude-3.5-sonnet", DisplayName: "Claude 3.5 Sonnet", IsDefault: false},
		{Provider: "anthropic", ModelID: "claude-3-opus", DisplayName: "Claude 3 Opus", IsDefault: false},
		{Provider: "anthropic", ModelID: "claude-3-haiku", DisplayName: "Claude 3 Haiku", IsDefault: false},

		{Provider: "gemini", ModelID: "gemini-2.0-flash", DisplayName: "Gemini 2.0 Flash", IsDefault: true},
		{Provider: "gemini", ModelID: "gemini-1.5-pro", DisplayName: "Gemini 1.5 Pro", IsDefault: false},
		{Provider: "gemini", ModelID: "gemini-1.5-flash", DisplayName: "Gemini 1.5 Flash", IsDefault: false},

		{Provider: "zai", ModelID: "zai-1", DisplayName: "ZAI-1", IsDefault: true},
		{Provider: "zai", ModelID: "zai-turbo", DisplayName: "ZAI Turbo", IsDefault: false},
		{Provider: "zai", ModelID: "glm-4-plus", DisplayName: "GLM-4 Plus", IsDefault: false},
		{Provider: "zai", ModelID: "glm-z1-air", DisplayName: "GLM-Z1 Air", IsDefault: false},
	}

	for _, m := range defaultModels {
		if err := mdb.Create(m); err != nil {
			return err
		}
	}

	return nil
}

func (mdb *ModelDB) Create(m Model) error {
	_, err := mdb.db.Exec(
		"INSERT OR IGNORE INTO models (provider, model_id, display_name, is_default) VALUES (?, ?, ?, ?)",
		m.Provider, m.ModelID, m.DisplayName, m.IsDefault,
	)
	return err
}

func (mdb *ModelDB) GetAll(provider string) ([]Model, error) {
	var rows *sql.Rows
	var err error

	if provider != "" {
		rows, err = mdb.db.Query("SELECT id, provider, model_id, display_name, is_default, created_at FROM models WHERE provider = ? ORDER BY is_default DESC, display_name", provider)
	} else {
		rows, err = mdb.db.Query("SELECT id, provider, model_id, display_name, is_default, created_at FROM models ORDER BY provider, is_default DESC, display_name")
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []Model
	for rows.Next() {
		var m Model
		var isDefault int
		if err := rows.Scan(&m.ID, &m.Provider, &m.ModelID, &m.DisplayName, &isDefault, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.IsDefault = isDefault == 1
		models = append(models, m)
	}

	return models, rows.Err()
}

func (mdb *ModelDB) GetByID(id int64) (*Model, error) {
	var m Model
	var isDefault int
	err := mdb.db.QueryRow(
		"SELECT id, provider, model_id, display_name, is_default, created_at FROM models WHERE id = ?",
		id,
	).Scan(&m.ID, &m.Provider, &m.ModelID, &m.DisplayName, &isDefault, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	m.IsDefault = isDefault == 1
	return &m, nil
}

func (mdb *ModelDB) Update(id int64, m Model) error {
	_, err := mdb.db.Exec(
		"UPDATE models SET provider = ?, model_id = ?, display_name = ?, is_default = ? WHERE id = ?",
		m.Provider, m.ModelID, m.DisplayName, m.IsDefault, id,
	)
	return err
}

func (mdb *ModelDB) Delete(id int64) error {
	_, err := mdb.db.Exec("DELETE FROM models WHERE id = ?", id)
	return err
}

func (mdb *ModelDB) Close() error {
	return mdb.db.Close()
}
