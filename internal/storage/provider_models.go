package storage

import (
	"database/sql"
	"fmt"
)

// ProviderModel represents a model associated with a provider.
type ProviderModel struct {
	ID          string `json:"id"`
	ProviderID  string `json:"provider_id"`
	ModelName   string `json:"model_name"`
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ListProviderModels returns all models for a given provider.
func ListProviderModels(db *sql.DB, providerID string) ([]ProviderModel, error) {
	rows, err := db.Query(
		`SELECT id, provider_id, model_name, display_name, enabled, source, created_at, updated_at
		 FROM provider_models WHERE provider_id = ? ORDER BY model_name`, providerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list provider models: %w", err)
	}
	defer rows.Close()

	var models []ProviderModel
	for rows.Next() {
		var m ProviderModel
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.ModelName, &m.DisplayName, &m.Enabled, &m.Source, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan provider model: %w", err)
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

// GetProviderModel returns a single provider model by ID.
func GetProviderModel(db *sql.DB, id string) (*ProviderModel, error) {
	var m ProviderModel
	err := db.QueryRow(
		`SELECT id, provider_id, model_name, display_name, enabled, source, created_at, updated_at
		 FROM provider_models WHERE id = ?`, id,
	).Scan(&m.ID, &m.ProviderID, &m.ModelName, &m.DisplayName, &m.Enabled, &m.Source, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get provider model: %w", err)
	}
	return &m, nil
}

// CreateProviderModel inserts a new provider model.
func CreateProviderModel(db *sql.DB, m *ProviderModel) error {
	_, err := db.Exec(
		`INSERT INTO provider_models (id, provider_id, model_name, display_name, enabled, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ProviderID, m.ModelName, m.DisplayName, boolToInt(m.Enabled), m.Source, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create provider model: %w", err)
	}
	return nil
}

// UpdateProviderModel updates an existing provider model.
func UpdateProviderModel(db *sql.DB, m *ProviderModel) error {
	_, err := db.Exec(
		`UPDATE provider_models SET model_name=?, display_name=?, enabled=?, source=?, updated_at=? WHERE id=?`,
		m.ModelName, m.DisplayName, boolToInt(m.Enabled), m.Source, m.UpdatedAt, m.ID,
	)
	if err != nil {
		return fmt.Errorf("update provider model: %w", err)
	}
	return nil
}

// DeleteProviderModel deletes a provider model by ID.
func DeleteProviderModel(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM provider_models WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete provider model: %w", err)
	}
	return nil
}

// UpsertProviderModel inserts or updates a provider model based on provider_id + model_name uniqueness.
// It returns the inserted/updated model.
// When a duplicate exists, it preserves the existing source (e.g., "manual") and only updates
// display_name, enabled, and updated_at.
func UpsertProviderModel(db *sql.DB, m *ProviderModel) (*ProviderModel, error) {
	// Check if a model with the same provider_id and model_name already exists
	var existingID, existingSource, existingCreatedAt string
	err := db.QueryRow(
		`SELECT id, source, created_at FROM provider_models WHERE provider_id = ? AND model_name = ?`,
		m.ProviderID, m.ModelName,
	).Scan(&existingID, &existingSource, &existingCreatedAt)

	if err == nil {
		// Update existing — preserve source, only update display_name/enabled/updated_at
		m.ID = existingID
		m.CreatedAt = existingCreatedAt
		m.Source = existingSource
		if err := UpdateProviderModel(db, m); err != nil {
			return nil, err
		}
		return m, nil
	}

	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("upsert provider model check: %w", err)
	}

	// Insert new
	if err := CreateProviderModel(db, m); err != nil {
		return nil, err
	}
	return m, nil
}

// ImportProviderModels bulk-upserts a list of provider models. Duplicates (same provider_id + model_name)
// are updated; new ones are inserted. Returns the final list of models for the provider.
func ImportProviderModels(db *sql.DB, models []ProviderModel) ([]ProviderModel, error) {
	for i := range models {
		if _, err := UpsertProviderModel(db, &models[i]); err != nil {
			return nil, err
		}
	}
	// Return all models for the provider (use the first model's provider_id)
	if len(models) == 0 {
		return nil, nil
	}
	return ListProviderModels(db, models[0].ProviderID)
}

// ProviderModelWithProvider joins a provider model with its provider.
type ProviderModelWithProvider struct {
	ProviderModel
	Provider Provider `json:"provider"`
}

// FindEnabledModelsByName returns all enabled provider models matching the given model name,
// along with their associated provider info. Providers with hide_original_models=true are excluded.
func FindEnabledModelsByName(db *sql.DB, modelName string) ([]ProviderModelWithProvider, error) {
	query := `
		SELECT pm.id, pm.provider_id, pm.model_name, pm.display_name, pm.enabled, pm.source, pm.created_at, pm.updated_at,
		       p.id, p.name, p.type, p.base_url, p.api_key, p.model_prefix, p.hide_original_models, p.enabled, p.created_at, p.updated_at
		FROM provider_models pm
		JOIN providers p ON p.id = pm.provider_id
		WHERE pm.model_name = ? AND pm.enabled = 1 AND p.enabled = 1 AND p.hide_original_models = 0
	`
	rows, err := db.Query(query, modelName)
	if err != nil {
		return nil, fmt.Errorf("find enabled models by name: %w", err)
	}
	defer rows.Close()

	var results []ProviderModelWithProvider
	for rows.Next() {
		var r ProviderModelWithProvider
		if err := rows.Scan(
			&r.ProviderModel.ID, &r.ProviderModel.ProviderID, &r.ProviderModel.ModelName, &r.ProviderModel.DisplayName,
			&r.ProviderModel.Enabled, &r.ProviderModel.Source, &r.ProviderModel.CreatedAt, &r.ProviderModel.UpdatedAt,
			&r.Provider.ID, &r.Provider.Name, &r.Provider.Type, &r.Provider.BaseURL, &r.Provider.APIKey,
			&r.Provider.ModelPrefix, &r.Provider.HideOriginalModels, &r.Provider.Enabled, &r.Provider.CreatedAt, &r.Provider.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan model with provider: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// FindEnabledModelsByNameForProvider returns all enabled provider models matching the given model name
// for a specific provider. Used for prefix model lookup.
func FindEnabledModelsByNameForProvider(db *sql.DB, providerID string, modelName string) ([]ProviderModelWithProvider, error) {
	query := `
		SELECT pm.id, pm.provider_id, pm.model_name, pm.display_name, pm.enabled, pm.source, pm.created_at, pm.updated_at,
		       p.id, p.name, p.type, p.base_url, p.api_key, p.model_prefix, p.hide_original_models, p.enabled, p.created_at, p.updated_at
		FROM provider_models pm
		JOIN providers p ON p.id = pm.provider_id
		WHERE pm.provider_id = ? AND pm.model_name = ? AND pm.enabled = 1 AND p.enabled = 1
	`
	rows, err := db.Query(query, providerID, modelName)
	if err != nil {
		return nil, fmt.Errorf("find enabled models by name for provider: %w", err)
	}
	defer rows.Close()

	var results []ProviderModelWithProvider
	for rows.Next() {
		var r ProviderModelWithProvider
		if err := rows.Scan(
			&r.ProviderModel.ID, &r.ProviderModel.ProviderID, &r.ProviderModel.ModelName, &r.ProviderModel.DisplayName,
			&r.ProviderModel.Enabled, &r.ProviderModel.Source, &r.ProviderModel.CreatedAt, &r.ProviderModel.UpdatedAt,
			&r.Provider.ID, &r.Provider.Name, &r.Provider.Type, &r.Provider.BaseURL, &r.Provider.APIKey,
			&r.Provider.ModelPrefix, &r.Provider.HideOriginalModels, &r.Provider.Enabled, &r.Provider.CreatedAt, &r.Provider.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan model with provider: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ExposedProviderModel represents a model visible through the gateway /v1/models endpoint.
type ExposedProviderModel struct {
	ModelName          string
	ModelPrefix        string
	HideOriginalModels bool
}

// ListExposedProviderModels returns all enabled provider models from enabled providers.
func ListExposedProviderModels(db *sql.DB) ([]ExposedProviderModel, error) {
	rows, err := db.Query(`
		SELECT pm.model_name, p.model_prefix, p.hide_original_models
		FROM provider_models pm
		JOIN providers p ON p.id = pm.provider_id
		WHERE pm.enabled = 1 AND p.enabled = 1
	`)
	if err != nil {
		return nil, fmt.Errorf("list exposed provider models: %w", err)
	}
	defer rows.Close()

	var models []ExposedProviderModel
	for rows.Next() {
		var m ExposedProviderModel
		if err := rows.Scan(&m.ModelName, &m.ModelPrefix, &m.HideOriginalModels); err != nil {
			return nil, fmt.Errorf("scan exposed provider model: %w", err)
		}
		models = append(models, m)
	}
	return models, rows.Err()
}
