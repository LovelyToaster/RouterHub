package storage

import "time"

// Provider represents a LLM provider configuration.
type Provider struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	BaseURL            string `json:"base_url"`
	APIKey             string `json:"api_key"`
	ModelPrefix        string `json:"model_prefix"`
	HideOriginalModels bool   `json:"hide_original_models"`
	Enabled            bool   `json:"enabled"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

// ModelAlias maps a user-facing alias to a provider's target model name.
// The alias is unique within a provider and allows accessing models even
// when hide_original_models is enabled.
type ModelAlias struct {
	ID          string `json:"id"`
	Alias       string `json:"alias"`
	ProviderID  string `json:"provider_id"`
	TargetModel string `json:"target_model"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// GatewayAPIKey represents an API key for gateway access.
type GatewayAPIKey struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	APIKey     string  `json:"api_key"`
	Enabled    bool    `json:"enabled"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
}

// AdminUser represents an admin user.
type AdminUser struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	Password    string  `json:"-"`
	Enabled     bool    `json:"enabled"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	LastLoginAt *string `json:"last_login_at,omitempty"`
	Timezone    string  `json:"timezone"`
}

// AdminSession represents an admin session.
type AdminSession struct {
	ID         string  `json:"id"`
	UserID     string  `json:"user_id"`
	Token      string  `json:"token"`
	ExpiresAt  string  `json:"expires_at"`
	CreatedAt  string  `json:"created_at"`
	LastSeenAt *string `json:"last_seen_at,omitempty"`
}

// AppSetting represents a key-value setting.
type AppSetting struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

// RequestLog represents a single request log entry.
type RequestLog struct {
	ID                 int64   `json:"id"`
	RequestID          string  `json:"request_id"`
	ProviderName       string  `json:"provider_name"`
	ProviderType       string  `json:"provider_type"`
	InboundProtocol    string  `json:"inbound_protocol"`
	RequestedModel     string  `json:"requested_model"`
	ActualModel        string  `json:"actual_model"`
	Stream             bool    `json:"stream"`
	Status             string  `json:"status"`
	ErrorMessage       *string `json:"error_message,omitempty"`
	CreatedAt          string  `json:"created_at"`
	FinishedAt         *string `json:"finished_at,omitempty"`
	TimeToFirstTokenMs *int64  `json:"time_to_first_token_ms,omitempty"`
	TotalDurationMs    *int64  `json:"total_duration_ms,omitempty"`
	InputTokens        int64   `json:"input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	CachedTokens       int64   `json:"cached_tokens"`
	CacheWriteTokens   int64   `json:"cache_write_tokens"`
	TotalTokens        int64   `json:"total_tokens"`
	ClientIP           string  `json:"client_ip"`
	GatewayAPIKeyName  string  `json:"gateway_api_key_name"`
}

// Helper functions for timestamps
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
