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
		`CREATE TABLE IF NOT EXISTS stats_counters (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dimension TEXT NOT NULL,
			bucket TEXT NOT NULL,
			request_count INTEGER NOT NULL DEFAULT 0,
			success_count INTEGER NOT NULL DEFAULT 0,
			error_count INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cached_tokens INTEGER NOT NULL DEFAULT 0,
			cache_write_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			duration_sum_ms INTEGER NOT NULL DEFAULT 0,
			ttft_sum_ms INTEGER NOT NULL DEFAULT 0,
			perf_output_tokens INTEGER NOT NULL DEFAULT 0,
			perf_proc_ms INTEGER NOT NULL DEFAULT 0,
			perf_ttft_sum_ms INTEGER NOT NULL DEFAULT 0,
			perf_n INTEGER NOT NULL DEFAULT 0,
			UNIQUE(dimension, bucket)
		)`,
		`CREATE TABLE IF NOT EXISTS stats_series (
			bucket TEXT PRIMARY KEY,
			request_count INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0
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
		`CREATE INDEX IF NOT EXISTS idx_stats_counters_bucket ON stats_counters(bucket)`,
	}
	for _, stmt := range indexStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate index: %w", err)
		}
	}

	if err := ensureRequestLogsInboundProtocol(db); err != nil {
		return fmt.Errorf("ensure inbound_protocol column: %w", err)
	}

	if err := ensureRequestLogsExtraColumns(db); err != nil {
		return fmt.Errorf("ensure extra columns: %w", err)
	}

	if err := runDataMigrations(db); err != nil {
		return fmt.Errorf("data migration: %w", err)
	}

	if err := runDataMigrationOnceDirect(db, "data_migration_backfill_stats_counters_v1", func() error {
		return backfillStatsCounters(db)
	}); err != nil {
		return fmt.Errorf("backfill stats counters: %w", err)
	}

	if err := ensureDefaultSettings(db); err != nil {
		return fmt.Errorf("ensure default settings: %w", err)
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

// ensureRequestLogsExtraColumns checks whether the http_status, request_body and
// response_body columns exist on request_logs and adds any missing ones via
// ALTER TABLE. This handles databases created before these columns were added to
// the schema. The function is idempotent: existing columns are skipped.
func ensureRequestLogsExtraColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(request_logs)`)
	if err != nil {
		return fmt.Errorf("pragma table_info: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan pragma row: %w", err)
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	cols := map[string]string{
		"http_status":   "INTEGER",
		"request_body":  "TEXT",
		"response_body": "TEXT",
	}
	for name, ctype := range cols {
		if existing[name] {
			continue
		}
		stmt := fmt.Sprintf(`ALTER TABLE request_logs ADD COLUMN %s %s`, name, ctype)
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("alter table add column %s: %w", name, err)
		}
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

// runDataMigrationOnceDirect runs fn exactly once per database, recording a
// flag in app_settings so it is not re-executed. Unlike runMigrationOnce it
// does not wrap fn in its own transaction (the caller manages any transaction).
func runDataMigrationOnceDirect(db *sql.DB, key string, fn func() error) error {
	var existing string
	err := db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&existing)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("check migration flag %s: %w", key, err)
	}
	if err := fn(); err != nil {
		return fmt.Errorf("migration %s: %w", key, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)`, key, now, now)
	return err
}

// backfillStatsCounters rebuilds the stats_counters and stats_series tables from
// existing request_logs. Runs in batches so it does not hold large result sets in
// memory and can resume after a crash (id > lastID ordering).
func backfillStatsCounters(db *sql.DB) error {
	// Clear any partially backfilled data to make re-runs idempotent.
	if _, err := db.Exec(`DELETE FROM stats_counters`); err != nil {
		return fmt.Errorf("clear stats_counters before backfill: %w", err)
	}
	if _, err := db.Exec(`DELETE FROM stats_series`); err != nil {
		return fmt.Errorf("clear stats_series before backfill: %w", err)
	}

	const batchSize = 500
	var lastID int64 = 0
	for {
		rows, err := db.Query(
			`SELECT id, request_id, provider_name, provider_type, actual_model, stream, status,
			        created_at, finished_at, time_to_first_token_ms, total_duration_ms,
			        input_tokens, output_tokens, cached_tokens, cache_write_tokens, total_tokens
			 FROM request_logs WHERE id > ? ORDER BY id LIMIT ?`, lastID, batchSize)
		if err != nil {
			return fmt.Errorf("query request_logs for backfill: %w", err)
		}
		count := 0
		var streamInt int
		for rows.Next() {
			var log RequestLog
			if err := rows.Scan(&log.ID, &log.RequestID, &log.ProviderName, &log.ProviderType,
				&log.ActualModel, &streamInt, &log.Status, &log.CreatedAt,
				&log.FinishedAt, &log.TimeToFirstTokenMs, &log.TotalDurationMs,
				&log.InputTokens, &log.OutputTokens, &log.CachedTokens, &log.CacheWriteTokens, &log.TotalTokens); err != nil {
				rows.Close()
				return fmt.Errorf("scan request_log: %w", err)
			}
			log.Stream = streamInt != 0
			if err := UpsertStatsCounters(db, &log); err != nil {
				rows.Close()
				return fmt.Errorf("upsert stats: %w", err)
			}
			lastID = log.ID
			count++
		}
		rows.Close()
		if count < batchSize {
			break
		}
	}
	return nil
}

// ensureDefaultSettings inserts default app_settings rows if they are missing.
func ensureDefaultSettings(db *sql.DB) error {
	defaults := []struct{ key, value string }{
		{"stats.retention_days", "0"},
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, d := range defaults {
		_, err := db.Exec(
			`INSERT OR IGNORE INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)`,
			d.key, d.value, now,
		)
		if err != nil {
			return fmt.Errorf("insert default setting %s: %w", d.key, err)
		}
	}
	return nil
}
