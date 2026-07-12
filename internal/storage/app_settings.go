package storage

import (
	"database/sql"
	"fmt"
)

func GetAppSetting(db *sql.DB, key string) (*AppSetting, error) {
	var s AppSetting
	err := db.QueryRow(`SELECT key, value, updated_at FROM app_settings WHERE key = ?`, key).
		Scan(&s.Key, &s.Value, &s.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get app setting: %w", err)
	}
	return &s, nil
}

// GetAppSettingString returns the string value of an app setting, falling back
// to def when the setting is missing or empty.
func GetAppSettingString(db *sql.DB, key, def string) string {
	s, err := GetAppSetting(db, key)
	if err != nil {
		return def
	}
	if s == nil || s.Value == "" {
		return def
	}
	return s.Value
}

func SetAppSetting(db *sql.DB, key, value, updatedAt string) error {
	_, err := db.Exec(
		`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("set app setting: %w", err)
	}
	return nil
}

// ListAppSettings returns all app settings.
func ListAppSettings(db *sql.DB) ([]AppSetting, error) {
	rows, err := db.Query(`SELECT key, value, updated_at FROM app_settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list app settings: %w", err)
	}
	defer rows.Close()

	var settings []AppSetting
	for rows.Next() {
		var s AppSetting
		if err := rows.Scan(&s.Key, &s.Value, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan app setting: %w", err)
		}
		settings = append(settings, s)
	}
	return settings, rows.Err()
}

// SetAppSettings batch-upserts multiple settings in a single transaction.
func SetAppSettings(db *sql.DB, settings map[string]string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := Now()
	stmt, err := tx.Prepare(`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`)
	if err != nil {
		return fmt.Errorf("prepare stmt: %w", err)
	}
	defer stmt.Close()

	for key, value := range settings {
		if _, err := stmt.Exec(key, value, now); err != nil {
			return fmt.Errorf("set setting %s: %w", key, err)
		}
	}

	return tx.Commit()
}
