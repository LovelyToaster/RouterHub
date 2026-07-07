package storage

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// insertRawLog inserts a request_logs row directly, bypassing the normal
// InsertRequestLog helper so we can simulate legacy data with any provider_type.
func insertRawLog(t *testing.T, db *sql.DB, requestID, providerType string, input, output, cached, cacheWrite, total int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO request_logs (
			request_id, provider_name, provider_type, requested_model, actual_model,
			stream, status, created_at, input_tokens, output_tokens,
			cached_tokens, cache_write_tokens, total_tokens
		) VALUES (?, ?, ?, ?, ?, 0, 'success', '2026-01-01T00:00:00Z', ?, ?, ?, ?, ?)
	`, requestID, "p", providerType, "m", "m", input, output, cached, cacheWrite, total)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

// TestDataMigration_NormalizeAnthropicInputTokens verifies that legacy Anthropic
// request_logs rows get their input_tokens/total_tokens rewritten to the new
// "total input including cache reads/writes" convention, that non-Anthropic
// rows are left untouched, and that the migration is idempotent.
func TestDataMigration_NormalizeAnthropicInputTokens(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Run the base migration once to create tables (this also runs the data
	// migration, but request_logs is empty at this point so it's a no-op that
	// still sets the completion flag). To exercise the data migration on
	// existing rows, we clear the flag afterwards, insert legacy rows, and
	// call Migrate again.
	if err := Migrate(db); err != nil {
		t.Fatalf("initial migrate: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM app_settings WHERE key = 'data_migration_normalize_anthropic_input_tokens_v1'`); err != nil {
		t.Fatalf("reset flag: %v", err)
	}

	// Legacy Anthropic row: input_tokens excludes cache read/write.
	insertRawLog(t, db, "r-a1", "anthropic-messages", 100, 20, 800, 300, 120)
	// Legacy Anthropic row without any cache: should still get input rewritten
	// to itself (input + 0 + 0), effectively unchanged.
	insertRawLog(t, db, "r-a2", "anthropic-messages", 50, 10, 0, 0, 60)
	// OpenAI Chat row: input_tokens already includes cached_tokens, must not
	// be touched by the migration.
	insertRawLog(t, db, "r-oc", "openai-chat-completions", 1843, 20, 1024, 0, 1863)
	// OpenAI Responses row: same invariant.
	insertRawLog(t, db, "r-or", "openai-responses", 500, 50, 200, 0, 550)

	// Re-run migrations to trigger the data migration on the seeded rows.
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	assertRow := func(id string, wantInput, wantTotal int64) {
		t.Helper()
		var input, total int64
		if err := db.QueryRow(
			`SELECT input_tokens, total_tokens FROM request_logs WHERE request_id = ?`, id,
		).Scan(&input, &total); err != nil {
			t.Fatalf("select %s: %v", id, err)
		}
		if input != wantInput || total != wantTotal {
			t.Errorf("row %s: want input=%d total=%d, got input=%d total=%d",
				id, wantInput, wantTotal, input, total)
		}
	}

	// Anthropic row with cache: input becomes 100+800+300 = 1200; total becomes
	// (original_input + cached + cache_write) + output = 1200 + 20 = 1220.
	assertRow("r-a1", 1200, 1220)
	// Anthropic row without cache: input stays 50; total = 50 + 10 = 60.
	assertRow("r-a2", 50, 60)
	// OpenAI rows must be unchanged.
	assertRow("r-oc", 1843, 1863)
	assertRow("r-or", 500, 550)

	// Idempotency: running the migration again must not further modify rows.
	if err := Migrate(db); err != nil {
		t.Fatalf("third migrate: %v", err)
	}
	assertRow("r-a1", 1200, 1220)
	assertRow("r-a2", 50, 60)
	assertRow("r-oc", 1843, 1863)
	assertRow("r-or", 500, 550)
}
