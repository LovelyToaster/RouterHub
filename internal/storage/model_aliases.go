package storage

import (
	"database/sql"
	"fmt"
)

func ListModelAliases(db *sql.DB) ([]ModelAlias, error) {
	rows, err := db.Query(`SELECT id, alias, provider_id, target_model, enabled, created_at, updated_at FROM model_aliases ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list model aliases: %w", err)
	}
	defer rows.Close()

	var aliases []ModelAlias
	for rows.Next() {
		var a ModelAlias
		if err := rows.Scan(&a.ID, &a.Alias, &a.ProviderID, &a.TargetModel, &a.Enabled, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan model alias: %w", err)
		}
		aliases = append(aliases, a)
	}
	return aliases, rows.Err()
}

func GetModelAlias(db *sql.DB, id string) (*ModelAlias, error) {
	var a ModelAlias
	err := db.QueryRow(`SELECT id, alias, provider_id, target_model, enabled, created_at, updated_at FROM model_aliases WHERE id = ?`, id).
		Scan(&a.ID, &a.Alias, &a.ProviderID, &a.TargetModel, &a.Enabled, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get model alias: %w", err)
	}
	return &a, nil
}

func CreateModelAlias(db *sql.DB, a *ModelAlias) error {
	_, err := db.Exec(
		`INSERT INTO model_aliases (id, alias, provider_id, target_model, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Alias, a.ProviderID, a.TargetModel, boolToInt(a.Enabled), a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create model alias: %w", err)
	}
	return nil
}

func UpdateModelAlias(db *sql.DB, a *ModelAlias) error {
	_, err := db.Exec(
		`UPDATE model_aliases SET alias=?, provider_id=?, target_model=?, enabled=?, updated_at=? WHERE id=?`,
		a.Alias, a.ProviderID, a.TargetModel, boolToInt(a.Enabled), a.UpdatedAt, a.ID,
	)
	if err != nil {
		return fmt.Errorf("update model alias: %w", err)
	}
	return nil
}

func DeleteModelAlias(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM model_aliases WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete model alias: %w", err)
	}
	return nil
}

// AliasWithProvider joins a model alias with its provider.
// Provider is a named field (not embedded) to avoid JSON tag conflicts.
type AliasWithProvider struct {
	ModelAlias
	Provider Provider `json:"provider"`
}

// FindEnabledAliasesByModel returns all enabled model aliases matching the given alias name,
// along with their associated provider info.
func FindEnabledAliasesByModel(db *sql.DB, alias string) ([]AliasWithProvider, error) {
	query := `
		SELECT ma.id, ma.alias, ma.provider_id, ma.target_model, ma.enabled, ma.created_at, ma.updated_at,
		       p.id, p.name, p.type, p.base_url, p.api_key, p.model_prefix, p.hide_original_models, p.enabled, p.created_at, p.updated_at
		FROM model_aliases ma
		JOIN providers p ON p.id = ma.provider_id
		WHERE ma.alias = ? AND ma.enabled = 1 AND p.enabled = 1
	`
	rows, err := db.Query(query, alias)
	if err != nil {
		return nil, fmt.Errorf("find enabled aliases: %w", err)
	}
	defer rows.Close()

	var results []AliasWithProvider
	for rows.Next() {
		var r AliasWithProvider
		if err := rows.Scan(
			&r.ModelAlias.ID, &r.ModelAlias.Alias, &r.ModelAlias.ProviderID, &r.ModelAlias.TargetModel,
			&r.ModelAlias.Enabled, &r.ModelAlias.CreatedAt, &r.ModelAlias.UpdatedAt,
			&r.Provider.ID, &r.Provider.Name, &r.Provider.Type, &r.Provider.BaseURL, &r.Provider.APIKey,
			&r.Provider.ModelPrefix, &r.Provider.HideOriginalModels, &r.Provider.Enabled, &r.Provider.CreatedAt, &r.Provider.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan alias with provider: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ListModelAliasesByProvider returns all model aliases for a given provider.
func ListModelAliasesByProvider(db *sql.DB, providerID string) ([]ModelAlias, error) {
	rows, err := db.Query(`SELECT id, alias, provider_id, target_model, enabled, created_at, updated_at FROM model_aliases WHERE provider_id = ? ORDER BY created_at DESC`, providerID)
	if err != nil {
		return nil, fmt.Errorf("list model aliases by provider: %w", err)
	}
	defer rows.Close()

	var aliases []ModelAlias
	for rows.Next() {
		var a ModelAlias
		if err := rows.Scan(&a.ID, &a.Alias, &a.ProviderID, &a.TargetModel, &a.Enabled, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan model alias: %w", err)
		}
		aliases = append(aliases, a)
	}
	return aliases, rows.Err()
}
