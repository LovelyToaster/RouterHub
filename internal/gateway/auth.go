package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lovelytoaster94/routerhub/internal/storage"
)

type contextKey string

const gatewayKeyContextKey contextKey = "gateway_api_key"

// GatewayAuthMiddleware validates gateway API keys from Authorization header or x-api-key header.
func GatewayAuthMiddleware(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := extractAPIKey(r)
			if apiKey == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing api key"})
				return
			}

			key, err := storage.GetGatewayAPIKeyByKey(db, apiKey)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
				return
			}
			if key == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid api key"})
				return
			}

			// Touch last_used_at
			now := storage.Now()
			_ = storage.TouchGatewayAPIKey(db, key.ID, now)

			ctx := context.WithValue(r.Context(), gatewayKeyContextKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetGatewayKeyFromContext retrieves the gateway API key from context.
func GetGatewayKeyFromContext(ctx context.Context) *storage.GatewayAPIKey {
	key, _ := ctx.Value(gatewayKeyContextKey).(*storage.GatewayAPIKey)
	return key
}

func extractAPIKey(r *http.Request) string {
	// Try Authorization: Bearer <key>
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// Try x-api-key header
	key := r.Header.Get("x-api-key")
	if key != "" {
		return key
	}

	return ""
}
