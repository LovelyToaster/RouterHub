package providerapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/lovelytoaster94/routerhub/internal/storage"
)

const (
	defaultTimeout   = 15 * time.Second
	anthropicVersion = "2023-06-01"
)

// FetchModels fetches the list of available model names from the upstream provider.
// It returns deduplicated and sorted model names.
func FetchModels(ctx context.Context, provider *storage.Provider) ([]string, error) {
	client := &http.Client{Timeout: defaultTimeout}

	modelsURL := buildModelsURL(provider.BaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	setRequestHeaders(req, provider)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return parseModelsResponse(body)
}

// TestConnection tests connectivity to the upstream provider by calling its models endpoint.
func TestConnection(ctx context.Context, provider *storage.Provider) error {
	client := &http.Client{Timeout: defaultTimeout}

	modelsURL := buildModelsURL(provider.BaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	setRequestHeaders(req, provider)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upstream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

// buildModelsURL constructs the models endpoint URL from the provider's base URL.
// It handles trailing slashes and the presence of "/v1" in the base URL.
func buildModelsURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/models"
	}
	return baseURL + "/v1/models"
}

// setRequestHeaders sets the appropriate authentication and version headers
// based on the provider type.
func setRequestHeaders(req *http.Request, provider *storage.Provider) {
	switch provider.Type {
	case "openai-chat-completions", "openai-responses":
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	case "anthropic-messages":
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", anthropicVersion)
	}
}

// parseModelsResponse attempts to parse a provider's models list response.
// It supports multiple formats:
//   - {"data":[{"id":"..."}]}  (OpenAI / Anthropic)
//   - {"models":[{"id":"..."}]}
//   - {"models":[{"name":"models/..."}]}
func parseModelsResponse(body []byte) ([]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var models []string

	// Try "data" array (OpenAI / Anthropic style)
	if dataRaw, ok := raw["data"]; ok {
		var dataList []map[string]json.RawMessage
		if err := json.Unmarshal(dataRaw, &dataList); err == nil {
			for _, item := range dataList {
				if id, ok := item["id"]; ok {
					var idStr string
					if err := json.Unmarshal(id, &idStr); err == nil && idStr != "" {
						models = append(models, idStr)
					}
				}
			}
			if len(models) > 0 {
				return dedupAndSort(models), nil
			}
		}
	}

	// Try "models" array
	if modelsRaw, ok := raw["models"]; ok {
		var modelsList []map[string]json.RawMessage
		if err := json.Unmarshal(modelsRaw, &modelsList); err == nil {
			for _, item := range modelsList {
				// Try "id" first
				if id, ok := item["id"]; ok {
					var idStr string
					if err := json.Unmarshal(id, &idStr); err == nil && idStr != "" {
						models = append(models, idStr)
						continue
					}
				}
				// Try "name" (e.g., "models/claude-3-5-sonnet")
				if name, ok := item["name"]; ok {
					var nameStr string
					if err := json.Unmarshal(name, &nameStr); err == nil && nameStr != "" {
						// Strip "models/" prefix if present
						nameStr = strings.TrimPrefix(nameStr, "models/")
						models = append(models, nameStr)
					}
				}
			}
			if len(models) > 0 {
				return dedupAndSort(models), nil
			}
		}
	}

	return nil, fmt.Errorf("unable to parse models from response: no known format found")
}

// dedupAndSort removes duplicates and sorts the model names for stable output.
func dedupAndSort(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}
