package storage

import (
	"database/sql"
	"fmt"
	"time"
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
			inbound_protocol TEXT NOT NULL DEFAULT '',
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

	if err := ensureRequestLogsInboundProtocol(db); err != nil {
		return fmt.Errorf("ensure inbound_protocol column: %w", err)
	}

	if err := runDataMigrations(db); err != nil {
		return fmt.Errorf("data migration: %w", err)
	}

	// Reap any "pending" rows left over from a previous run that never got to
	// finalize (crash, forced shutdown, etc.). Best-effort: failures here
	// should not block startup.
	if err := MarkPendingLogsAsError(db, "server restarted before request finished"); err != nil {
		fmt.Printf("cleanup pending request logs: %v\n", err)
	}

	return nil
}

// ensureRequestLogsInboundProtocol checks whether the inbound_protocol column
// exists on request_logs and adds it via ALTER TABLE if not. This handles the
// case where the database was created before the column was added to the schema.
func ensureRequestLogsInboundProtocol(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(request_logs)`)
	if err != nil {
		return fmt.Errorf("pragma table_info: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan pragma row: %w", err)
		}
		if name == "inbound_protocol" {
			return nil // column already exists
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(`ALTER TABLE request_logs ADD COLUMN inbound_protocol TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("alter table add column: %w", err)
	}
	return nil
}

// runDataMigrations executes one-off data-normalisation SQL. Each migration is
// keyed in app_settings and only runs once per database.
func runDataMigrations(db *sql.DB) error {
	migrations := []struct {
		key  string
		exec func(tx *sql.Tx) error
	}{
		{
			key: "data_migration_normalize_anthropic_input_tokens_v1",
			exec: func(tx *sql.Tx) error {
				// Historically we stored Anthropic's raw input_tokens (which excludes cache
				// reads/writes) verbatim. Normalise those rows so that input_tokens ==
				// "total input tokens including cache reads/writes", matching how we now
				// record fresh Anthropic requests and mirroring the OpenAI convention.
				// SQLite evaluates all SET expressions against the row's original values,
				// so the total_tokens expression still sees the pre-update input_tokens.
				// NB: provider_type stores the protocol constant (see internal/protocol),
				// which for Anthropic is "anthropic-messages" — not the bare "anthropic".
				_, err := tx.Exec(`
					UPDATE request_logs
					   SET input_tokens = input_tokens + cached_tokens + cache_write_tokens,
					       total_tokens = input_tokens + cached_tokens + cache_write_tokens + output_tokens
					 WHERE provider_type = 'anthropic-messages'
				`)
				return err
			},
		},
		{
			key: "data_migration_backfill_inbound_protocol_v1",
			exec: func(tx *sql.Tx) error {
				_, err := tx.Exec(`UPDATE request_logs SET inbound_protocol = provider_type WHERE inbound_protocol = ''`)
				return err
			},
		},
	}

	for _, m := range migrations {
		if err := runMigrationOnce(db, m.key, m.exec); err != nil {
			return err
		}
	}
	return nil
}

// runMigrationOnce runs exec inside a transaction and records the migration key
// in app_settings so it only ever runs once per database.
func runMigrationOnce(db *sql.DB, key string, exec func(tx *sql.Tx) error) error {
	var existing string
	err := db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&existing)
	if err == nil {
		return nil // already applied
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("check migration flag %s: %w", key, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := exec(tx); err != nil {
		return fmt.Errorf("migration %s: %w", key, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(
		`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)`,
		key, now, now,
	); err != nil {
		return fmt.Errorf("record migration flag %s: %w", key, err)
	}

	return tx.Commit()
}
