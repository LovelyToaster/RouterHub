package storage

import (
	"database/sql"
	"fmt"
)

func ListGatewayAPIKeys(db *sql.DB) ([]GatewayAPIKey, error) {
	rows, err := db.Query(`SELECT id, name, api_key, enabled, created_at, updated_at, last_used_at FROM gateway_api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list gateway api keys: %w", err)
	}
	defer rows.Close()

	var keys []GatewayAPIKey
	for rows.Next() {
		var k GatewayAPIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.APIKey, &k.Enabled, &k.CreatedAt, &k.UpdatedAt, &k.LastUsedAt); err != nil {
			return nil, fmt.Errorf("scan gateway api key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func GetGatewayAPIKey(db *sql.DB, id string) (*GatewayAPIKey, error) {
	var k GatewayAPIKey
	err := db.QueryRow(`SELECT id, name, api_key, enabled, created_at, updated_at, last_used_at FROM gateway_api_keys WHERE id = ?`, id).
		Scan(&k.ID, &k.Name, &k.APIKey, &k.Enabled, &k.CreatedAt, &k.UpdatedAt, &k.LastUsedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get gateway api key: %w", err)
	}
	return &k, nil
}

func GetGatewayAPIKeyByKey(db *sql.DB, key string) (*GatewayAPIKey, error) {
	var k GatewayAPIKey
	err := db.QueryRow(`SELECT id, name, api_key, enabled, created_at, updated_at, last_used_at FROM gateway_api_keys WHERE api_key = ? AND enabled = 1`, key).
		Scan(&k.ID, &k.Name, &k.APIKey, &k.Enabled, &k.CreatedAt, &k.UpdatedAt, &k.LastUsedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get gateway api key by key: %w", err)
	}
	return &k, nil
}

func CreateGatewayAPIKey(db *sql.DB, k *GatewayAPIKey) error {
	_, err := db.Exec(
		`INSERT INTO gateway_api_keys (id, name, api_key, enabled, created_at, updated_at, last_used_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.Name, k.APIKey, boolToInt(k.Enabled), k.CreatedAt, k.UpdatedAt, k.LastUsedAt,
	)
	if err != nil {
		return fmt.Errorf("create gateway api key: %w", err)
	}
	return nil
}

func UpdateGatewayAPIKey(db *sql.DB, k *GatewayAPIKey) error {
	_, err := db.Exec(
		`UPDATE gateway_api_keys SET name=?, api_key=?, enabled=?, updated_at=? WHERE id=?`,
		k.Name, k.APIKey, boolToInt(k.Enabled), k.UpdatedAt, k.ID,
	)
	if err != nil {
		return fmt.Errorf("update gateway api key: %w", err)
	}
	return nil
}

func DeleteGatewayAPIKey(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM gateway_api_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete gateway api key: %w", err)
	}
	return nil
}

func TouchGatewayAPIKey(db *sql.DB, id string, lastUsedAt string) error {
	_, err := db.Exec(`UPDATE gateway_api_keys SET last_used_at = ? WHERE id = ?`, lastUsedAt, id)
	if err != nil {
		return fmt.Errorf("touch gateway api key: %w", err)
	}
	return nil
}
