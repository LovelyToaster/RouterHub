package gateway

import (
	"database/sql"
	"fmt"
	"math/rand"
	"strings"

	"github.com/lovelytoaster94/routerhub/internal/storage"
)

// SelectedProvider holds the result of model selection.
type SelectedProvider struct {
	Provider      storage.Provider
	ModelAlias    *storage.ModelAlias
	ProviderModel string
}

// SelectProvider finds the best provider for the given model name and inbound protocol.
// The selection order is:
//  1. Explicit model alias (alias -> target_model) — bypasses hide_original_models
//  2. Prefix/model (providers.model_prefix = prefix + provider_models.model_name = model)
//  3. Original model (provider_models.model_name = model) — excludes hide_original_models=true
//
// For each step, same-protocol candidates are preferred; if none, all candidates are considered.
// When multiple candidates exist, one is chosen at random.
func SelectProvider(db *sql.DB, model string, inboundProtocol string) (*SelectedProvider, error) {
	// Step 1: Check explicit model alias
	if selected := trySelectFromAlias(db, model, inboundProtocol); selected != nil {
		return selected, nil
	}

	// Step 2: Check prefix/model (e.g., "azure/gpt-4" -> model_prefix="azure", model_name="gpt-4")
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		prefix := parts[0]
		modelName := parts[1]

		if selected := trySelectFromPrefix(db, prefix, modelName, inboundProtocol); selected != nil {
			return selected, nil
		}
	}

	// Step 3: Check original model (excludes hide_original_models=true)
	if selected := trySelectFromOriginal(db, model, inboundProtocol); selected != nil {
		return selected, nil
	}

	return nil, fmt.Errorf("model '%s' not found or no enabled providers", model)
}

// trySelectFromAlias looks up the model as an explicit alias.
// Model aliases bypass hide_original_models.
func trySelectFromAlias(db *sql.DB, alias string, inboundProtocol string) *SelectedProvider {
	results, err := storage.FindEnabledAliasesByModel(db, alias)
	if err != nil || len(results) == 0 {
		return nil
	}

	// Separate same-protocol candidates
	var sameProtocol []storage.AliasWithProvider
	var allCandidates []storage.AliasWithProvider

	for _, r := range results {
		allCandidates = append(allCandidates, r)
		if r.Provider.Type == inboundProtocol {
			sameProtocol = append(sameProtocol, r)
		}
	}

	var chosen storage.AliasWithProvider
	if len(sameProtocol) > 0 {
		chosen = sameProtocol[rand.Intn(len(sameProtocol))]
	} else {
		chosen = allCandidates[rand.Intn(len(allCandidates))]
	}

	return &SelectedProvider{
		Provider:      chosen.Provider,
		ModelAlias:    &chosen.ModelAlias,
		ProviderModel: chosen.TargetModel,
	}
}

// trySelectFromPrefix looks up the model by provider model_prefix + model_name.
// The prefix is the first part before "/", and modelName is the rest.
func trySelectFromPrefix(db *sql.DB, prefix string, modelName string, inboundProtocol string) *SelectedProvider {
	// Find provider by model_prefix
	provider, err := storage.GetProviderByModelPrefix(db, prefix)
	if err != nil || provider == nil {
		return nil
	}

	// Find the model within this provider
	results, err := storage.FindEnabledModelsByNameForProvider(db, provider.ID, modelName)
	if err != nil || len(results) == 0 {
		return nil
	}

	// Separate same-protocol candidates
	var sameProtocol []storage.ProviderModelWithProvider
	var allCandidates []storage.ProviderModelWithProvider

	for _, r := range results {
		allCandidates = append(allCandidates, r)
		if r.Provider.Type == inboundProtocol {
			sameProtocol = append(sameProtocol, r)
		}
	}

	var chosen storage.ProviderModelWithProvider
	if len(sameProtocol) > 0 {
		chosen = sameProtocol[rand.Intn(len(sameProtocol))]
	} else {
		chosen = allCandidates[rand.Intn(len(allCandidates))]
	}

	return &SelectedProvider{
		Provider:      chosen.Provider,
		ProviderModel: chosen.ModelName,
	}
}

// trySelectFromOriginal looks up the model by provider_models.model_name.
// Providers with hide_original_models=true are excluded.
func trySelectFromOriginal(db *sql.DB, modelName string, inboundProtocol string) *SelectedProvider {
	results, err := storage.FindEnabledModelsByName(db, modelName)
	if err != nil || len(results) == 0 {
		return nil
	}

	// Separate same-protocol candidates
	var sameProtocol []storage.ProviderModelWithProvider
	var allCandidates []storage.ProviderModelWithProvider

	for _, r := range results {
		allCandidates = append(allCandidates, r)
		if r.Provider.Type == inboundProtocol {
			sameProtocol = append(sameProtocol, r)
		}
	}

	var chosen storage.ProviderModelWithProvider
	if len(sameProtocol) > 0 {
		chosen = sameProtocol[rand.Intn(len(sameProtocol))]
	} else {
		chosen = allCandidates[rand.Intn(len(allCandidates))]
	}

	return &SelectedProvider{
		Provider:      chosen.Provider,
		ProviderModel: chosen.ModelName,
	}
}
