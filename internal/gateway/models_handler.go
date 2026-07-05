package gateway

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"

	"github.com/lovelytoaster94/routerhub/internal/storage"
)

// ModelsHandler returns an HTTP handler for GET /v1/models.
// It lists all exposed models (aliases, prefix/model, and optionally original names)
// in either OpenAI or Anthropic format based on request headers.
func ModelsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET
		if r.Method != http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
			return
		}

		providerModels, err := storage.ListExposedProviderModels(db)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		aliases, err := storage.ListExposedAliases(db)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Assemble deduplicated model set
		modelSet := make(map[string]struct{})

		// Add all aliases
		for _, a := range aliases {
			modelSet[a] = struct{}{}
		}

		// Add provider models
		for _, pm := range providerModels {
			if pm.ModelPrefix != "" {
				modelSet[pm.ModelPrefix+"/"+pm.ModelName] = struct{}{}
			}
			if !pm.HideOriginalModels {
				modelSet[pm.ModelName] = struct{}{}
			}
		}

		// Sort ascending
		models := make([]string, 0, len(modelSet))
		for m := range modelSet {
			models = append(models, m)
		}
		sort.Strings(models)

		w.Header().Set("Content-Type", "application/json")

		// Determine format based on request headers
		if r.Header.Get("x-api-key") != "" || r.Header.Get("anthropic-version") != "" {
			// Anthropic format
			type anthropicModel struct {
				Type        string `json:"type"`
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
				CreatedAt   string `json:"created_at"`
			}
			data := make([]anthropicModel, len(models))
			for i, m := range models {
				data[i] = anthropicModel{
					Type:        "model",
					ID:          m,
					DisplayName: m,
					CreatedAt:   "1970-01-01T00:00:00Z",
				}
			}
			firstID := ""
			lastID := ""
			if len(models) > 0 {
				firstID = models[0]
				lastID = models[len(models)-1]
			}
			resp := map[string]interface{}{
				"data":     data,
				"has_more": false,
				"first_id": firstID,
				"last_id":  lastID,
			}
			_ = json.NewEncoder(w).Encode(resp)
		} else {
			// OpenAI format (default)
			type openAIModel struct {
				ID       string `json:"id"`
				Object   string `json:"object"`
				Created  int    `json:"created"`
				OwnedBy  string `json:"owned_by"`
			}
			data := make([]openAIModel, len(models))
			for i, m := range models {
				data[i] = openAIModel{
					ID:      m,
					Object:  "model",
					Created: 0,
					OwnedBy: "routerhub",
				}
			}
			resp := map[string]interface{}{
				"object": "list",
				"data":   data,
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}
}
