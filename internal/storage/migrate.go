package storage

import (
	"database/sql"
	"fmt"
)

// Migrate creates all tables and indexes needed by the application.
// The schema is expressed as-is; there are no legacy compatibility branches.
// If you change the schema, drop `./data/routerhub.db*` and let the app recreate it.
func Migrate(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			base_url TEXT NOT NULL,
			api_key TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			model_prefix TEXT NOT NULL DEFAULT '',
			hide_original_models INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS model_aliases (
			id TEXT PRIMARY KEY,
			alias TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			target_model TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (provider_id) REFERENCES providers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS gateway_api_keys (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			api_key TEXT NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_used_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS admin_users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			timezone TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_login_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS admin_sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_seen_at TEXT,
			FOREIGN KEY (user_id) REFERENCES admin_users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS provider_models (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL,
			model_name TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			source TEXT NOT NULL DEFAULT 'manual',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (provider_id) REFERENCES providers(id) ON DELETE CASCADE,
			UNIQUE(provider_id, model_name)
		)`,
		`CREATE TABLE IF NOT EXISTS request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL UNIQUE,
			provider_name TEXT NOT NULL,
			provider_type TEXT NOT NULL,
			requested_model TEXT NOT NULL,
			actual_model TEXT NOT NULL,
			stream INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			error_message TEXT,
			created_at TEXT NOT NULL,
			finished_at TEXT,
			time_to_first_token_ms INTEGER,
			total_duration_ms INTEGER,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cached_tokens INTEGER NOT NULL DEFAULT 0,
			cache_write_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			client_ip TEXT NOT NULL DEFAULT '',
			gateway_api_key_name TEXT NOT NULL DEFAULT ''
		)`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	// Partial unique index on providers.model_prefix (only when non-empty).
	// If the SQLite version lacks partial index support, fall back to app-level validation.
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_model_prefix ON providers(model_prefix) WHERE model_prefix != ''`)

	indexStatements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_model_aliases_provider_alias ON model_aliases(provider_id, alias)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_status ON request_logs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_provider_name ON request_logs(provider_name)`,
		`CREATE INDEX IF NOT EXISTS idx_model_aliases_alias ON model_aliases(alias)`,
		`CREATE INDEX IF NOT EXISTS idx_provider_models_provider_id ON provider_models(provider_id)`,
	}
	for _, stmt := range indexStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate index: %w", err)
		}
	}

	return nil
}
