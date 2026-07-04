package storage

import (
	"database/sql"
	"fmt"
)

// scanProvider scans a provider row. The column order is:
// id, name, type, base_url, api_key, model_prefix, hide_original_models, enabled, created_at, updated_at
func scanProvider(scanner interface {
	Scan(dest ...interface{}) error
}) (Provider, error) {
	var p Provider
	err := scanner.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL, &p.APIKey, &p.ModelPrefix, &p.HideOriginalModels, &p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func ListProviders(db *sql.DB) ([]Provider, error) {
	rows, err := db.Query(`SELECT id, name, type, base_url, api_key, model_prefix, hide_original_models, enabled, created_at, updated_at FROM providers ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()

	var providers []Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

func GetProvider(db *sql.DB, id string) (*Provider, error) {
	row := db.QueryRow(`SELECT id, name, type, base_url, api_key, model_prefix, hide_original_models, enabled, created_at, updated_at FROM providers WHERE id = ?`, id)
	p, err := scanProvider(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get provider: %w", err)
	}
	return &p, nil
}

func CreateProvider(db *sql.DB, p *Provider) error {
	_, err := db.Exec(
		`INSERT INTO providers (id, name, type, base_url, api_key, model_prefix, hide_original_models, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Type, p.BaseURL, p.APIKey, p.ModelPrefix, boolToInt(p.HideOriginalModels), boolToInt(p.Enabled), p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}
	return nil
}

func UpdateProvider(db *sql.DB, p *Provider) error {
	_, err := db.Exec(
		`UPDATE providers SET name=?, type=?, base_url=?, api_key=?, model_prefix=?, hide_original_models=?, enabled=?, updated_at=? WHERE id=?`,
		p.Name, p.Type, p.BaseURL, p.APIKey, p.ModelPrefix, boolToInt(p.HideOriginalModels), boolToInt(p.Enabled), p.UpdatedAt, p.ID,
	)
	if err != nil {
		return fmt.Errorf("update provider: %w", err)
	}
	return nil
}

func DeleteProvider(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM providers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}

func ListEnabledProviders(db *sql.DB) ([]Provider, error) {
	rows, err := db.Query(`SELECT id, name, type, base_url, api_key, model_prefix, hide_original_models, enabled, created_at, updated_at FROM providers WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list enabled providers: %w", err)
	}
	defer rows.Close()

	var providers []Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

// GetProviderByModelPrefix returns a provider by its model_prefix (non-empty).
func GetProviderByModelPrefix(db *sql.DB, prefix string) (*Provider, error) {
	row := db.QueryRow(`SELECT id, name, type, base_url, api_key, model_prefix, hide_original_models, enabled, created_at, updated_at FROM providers WHERE model_prefix = ? AND model_prefix != ''`, prefix)
	p, err := scanProvider(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get provider by model prefix: %w", err)
	}
	return &p, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
