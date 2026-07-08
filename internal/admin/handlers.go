package admin

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/lovelytoaster94/routerhub/internal/events"
	"github.com/lovelytoaster94/routerhub/internal/protocol"
	"github.com/lovelytoaster94/routerhub/internal/providerapi"
	"github.com/lovelytoaster94/routerhub/internal/storage"
)

// AppVersion is the RouterHub build version reported to the UI.
const AppVersion = "1.0.0"

// BuildDate is injected via -ldflags "-X ...BuildDate=<RFC3339 UTC>".
// When empty (e.g. `go run`), we fall back to the binary's mtime at startup.
var BuildDate = ""

// AdminHandler holds handlers for the admin API.
type AdminHandler struct {
	DB *sql.DB
}

func NewAdminHandler(db *sql.DB) *AdminHandler {
	return &AdminHandler{DB: db}
}

// --- Health ---

func (h *AdminHandler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SystemInfo returns app + runtime version information used by the settings page.
func (h *AdminHandler) SystemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"app_version": AppVersion,
		"go_version":  runtime.Version(),
		"platform":    runtime.GOOS + "/" + runtime.GOARCH,
		"build_date":  resolveBuildDate(),
	})
}

// resolveBuildDate returns the ldflags-injected BuildDate, or falls back to the
// running executable's modification time (UTC, RFC3339). Empty string on failure.
func resolveBuildDate() string {
	if BuildDate != "" {
		return BuildDate
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	info, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339)
}

// --- Setup ---

func (h *AdminHandler) SetupStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := SetupStatus(h.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"initialized": initialized})
}

type SetupInitRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AdminHandler) SetupInit(w http.ResponseWriter, r *http.Request) {
	var req SetupInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	if len(req.Password) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}

	if err := InitSetup(h.DB, req.Username, req.Password); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"message": "admin user created"})
}

// --- Login ---

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AdminHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, err := Login(h.DB, req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token":      session.Token,
		"expires_at": session.ExpiresAt,
	})
}

func (h *AdminHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token := extractSessionToken(r)
	if token != "" {
		_ = Logout(h.DB, token)
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

// --- Current admin user (me) ---

type MeResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Timezone string `json:"timezone"`
}

func (h *AdminHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	user := GetAdminUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, MeResponse{
		ID:       user.ID,
		Username: user.Username,
		Timezone: user.Timezone,
	})
}

type UpdateMeRequest struct {
	Timezone *string `json:"timezone,omitempty"`
}

func (h *AdminHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	user := GetAdminUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req UpdateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Timezone != nil {
		tz := *req.Timezone
		if tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				writeError(w, http.StatusBadRequest, "invalid timezone")
				return
			}
		}
		if err := storage.UpdateAdminUserTimezone(h.DB, user.ID, tz, storage.Now()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		user.Timezone = tz
	}
	writeJSON(w, http.StatusOK, MeResponse{
		ID:       user.ID,
		Username: user.Username,
		Timezone: user.Timezone,
	})
}

// --- Providers CRUD ---

func (h *AdminHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := storage.ListProviders(h.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if providers == nil {
		providers = []storage.Provider{}
	}
	writeJSON(w, http.StatusOK, providers)
}

func (h *AdminHandler) GetProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	provider, err := storage.GetProvider(h.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if provider == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

type CreateProviderRequest struct {
	Name               string `json:"name"`
	Type               string `json:"type"`
	BaseURL            string `json:"base_url"`
	APIKey             string `json:"api_key"`
	ModelPrefix        string `json:"model_prefix"`
	HideOriginalModels bool   `json:"hide_original_models"`
	Enabled            *bool  `json:"enabled,omitempty"`
}

func (h *AdminHandler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var req CreateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Type == "" || req.BaseURL == "" || req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "name, type, base_url, and api_key are required")
		return
	}

	// Validate type
	if !protocol.IsValidProtocol(req.Type) {
		writeError(w, http.StatusBadRequest, "invalid provider type")
		return
	}

	// Validate model_prefix
	if req.ModelPrefix != "" {
		if strings.Contains(req.ModelPrefix, "/") {
			writeError(w, http.StatusBadRequest, "model_prefix must not contain '/'")
			return
		}
		// Check uniqueness
		existing, err := storage.GetProviderByModelPrefix(h.DB, req.ModelPrefix)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if existing != nil {
			writeError(w, http.StatusConflict, "model_prefix already in use")
			return
		}
	}
	if req.HideOriginalModels && req.ModelPrefix == "" {
		writeError(w, http.StatusBadRequest, "model_prefix is required when hide_original_models is true")
		return
	}

	now := storage.Now()
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	provider := &storage.Provider{
		ID:                 uuid.New().String(),
		Name:               req.Name,
		Type:               req.Type,
		BaseURL:            req.BaseURL,
		APIKey:             req.APIKey,
		ModelPrefix:        req.ModelPrefix,
		HideOriginalModels: req.HideOriginalModels,
		Enabled:            enabled,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := storage.CreateProvider(h.DB, provider); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, provider)
}

type UpdateProviderRequest struct {
	Name               *string `json:"name,omitempty"`
	Type               *string `json:"type,omitempty"`
	BaseURL            *string `json:"base_url,omitempty"`
	APIKey             *string `json:"api_key,omitempty"`
	ModelPrefix        *string `json:"model_prefix,omitempty"`
	HideOriginalModels *bool   `json:"hide_original_models,omitempty"`
	Enabled            *bool   `json:"enabled,omitempty"`
}

func (h *AdminHandler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := storage.GetProvider(h.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	var req UpdateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Type != nil {
		if !protocol.IsValidProtocol(*req.Type) {
			writeError(w, http.StatusBadRequest, "invalid provider type")
			return
		}
		existing.Type = *req.Type
	}
	if req.BaseURL != nil {
		if *req.BaseURL == "" {
			writeError(w, http.StatusBadRequest, "base_url must not be empty")
			return
		}
		existing.BaseURL = *req.BaseURL
	}
	if req.APIKey != nil {
		existing.APIKey = *req.APIKey
	}
	if req.ModelPrefix != nil {
		if strings.Contains(*req.ModelPrefix, "/") {
			writeError(w, http.StatusBadRequest, "model_prefix must not contain '/'")
			return
		}
		// Check uniqueness (exclude current provider)
		if *req.ModelPrefix != "" {
			existingByPrefix, err := storage.GetProviderByModelPrefix(h.DB, *req.ModelPrefix)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if existingByPrefix != nil && existingByPrefix.ID != id {
				writeError(w, http.StatusConflict, "model_prefix already in use")
				return
			}
		}
		existing.ModelPrefix = *req.ModelPrefix
	}
	if req.HideOriginalModels != nil {
		existing.HideOriginalModels = *req.HideOriginalModels
	}
	if existing.HideOriginalModels && existing.ModelPrefix == "" {
		writeError(w, http.StatusBadRequest, "model_prefix is required when hide_original_models is true")
		return
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	existing.UpdatedAt = storage.Now()

	if err := storage.UpdateProvider(h.DB, existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, existing)
}

func (h *AdminHandler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := storage.DeleteProvider(h.DB, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// --- Model Aliases CRUD ---

func (h *AdminHandler) ListModelAliases(w http.ResponseWriter, r *http.Request) {
	aliases, err := storage.ListModelAliases(h.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if aliases == nil {
		aliases = []storage.ModelAlias{}
	}
	writeJSON(w, http.StatusOK, aliases)
}

func (h *AdminHandler) ListProviderModelAliases(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")

	// Verify provider exists
	provider, err := storage.GetProvider(h.DB, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if provider == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	aliases, err := storage.ListModelAliasesByProvider(h.DB, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if aliases == nil {
		aliases = []storage.ModelAlias{}
	}
	writeJSON(w, http.StatusOK, aliases)
}

func (h *AdminHandler) GetModelAlias(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	alias, err := storage.GetModelAlias(h.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if alias == nil {
		writeError(w, http.StatusNotFound, "model alias not found")
		return
	}
	writeJSON(w, http.StatusOK, alias)
}

type CreateModelAliasRequest struct {
	Alias       string `json:"alias"`
	ProviderID  string `json:"provider_id"`
	TargetModel string `json:"target_model"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

func (h *AdminHandler) CreateModelAlias(w http.ResponseWriter, r *http.Request) {
	var req CreateModelAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Alias == "" || req.ProviderID == "" || req.TargetModel == "" {
		writeError(w, http.StatusBadRequest, "alias, provider_id, and target_model are required")
		return
	}

	// Verify provider exists
	provider, err := storage.GetProvider(h.DB, req.ProviderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if provider == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	now := storage.Now()
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	alias := &storage.ModelAlias{
		ID:          uuid.New().String(),
		Alias:       req.Alias,
		ProviderID:  req.ProviderID,
		TargetModel: req.TargetModel,
		Enabled:     enabled,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := storage.CreateModelAlias(h.DB, alias); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, alias)
}

type UpdateModelAliasRequest struct {
	Alias       *string `json:"alias,omitempty"`
	TargetModel *string `json:"target_model,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

func (h *AdminHandler) UpdateModelAlias(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := storage.GetModelAlias(h.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "model alias not found")
		return
	}

	var req UpdateModelAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Alias != nil {
		existing.Alias = *req.Alias
	}
	if req.TargetModel != nil {
		existing.TargetModel = *req.TargetModel
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	existing.UpdatedAt = storage.Now()

	if err := storage.UpdateModelAlias(h.DB, existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, existing)
}

func (h *AdminHandler) DeleteModelAlias(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := storage.DeleteModelAlias(h.DB, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// --- Gateway API Keys CRUD ---

// gatewayKeyPrefix is used by the auto-generator to mark server-issued keys.
const gatewayKeyPrefix = "rh-"

// generateGatewayAPIKey produces a URL-safe token with the "rh-" prefix using
// 32 bytes of cryptographic randomness.
func generateGatewayAPIKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return gatewayKeyPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func (h *AdminHandler) ListGatewayAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := storage.ListGatewayAPIKeys(h.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if keys == nil {
		keys = []storage.GatewayAPIKey{}
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *AdminHandler) GetGatewayAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	key, err := storage.GetGatewayAPIKey(h.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if key == nil {
		writeError(w, http.StatusNotFound, "api key not found")
		return
	}
	writeJSON(w, http.StatusOK, key)
}

type CreateGatewayAPIKeyRequest struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled,omitempty"`
}

func (h *AdminHandler) CreateGatewayAPIKey(w http.ResponseWriter, r *http.Request) {
	var req CreateGatewayAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	generated, err := generateGatewayAPIKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := storage.Now()
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	key := &storage.GatewayAPIKey{
		ID:        uuid.New().String(),
		Name:      strings.TrimSpace(req.Name),
		APIKey:    generated,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := storage.CreateGatewayAPIKey(h.DB, key); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, key)
}

type UpdateGatewayAPIKeyRequest struct {
	Name    *string `json:"name,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

func (h *AdminHandler) UpdateGatewayAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := storage.GetGatewayAPIKey(h.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "api key not found")
		return
	}

	var req UpdateGatewayAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		existing.Name = trimmed
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	existing.UpdatedAt = storage.Now()

	if err := storage.UpdateGatewayAPIKey(h.DB, existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, existing)
}

func (h *AdminHandler) DeleteGatewayAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := storage.DeleteGatewayAPIKey(h.DB, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// --- Settings ---

func (h *AdminHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := storage.ListAppSettings(h.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if settings == nil {
		settings = []storage.AppSetting{}
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *AdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := storage.SetAppSettings(h.DB, req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Return updated settings
	settings, err := storage.ListAppSettings(h.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if settings == nil {
		settings = []storage.AppSetting{}
	}
	writeJSON(w, http.StatusOK, settings)
}

// --- Stats ---

func (h *AdminHandler) buildStatsParams(r *http.Request) storage.StatsParams {
	user := GetAdminUserFromContext(r.Context())
	loc := LoadUserLocation(user)
	rk := ParseRange(r.URL.Query().Get("range"))
	now := time.Now()
	w := ComputeWindow(now, loc, rk)
	if rk == RangeAll {
		if earliest, err := storage.EarliestRequestLogTime(h.DB); err == nil {
			w = AdjustAllWindowForSeries(w, earliest, now)
		}
	}
	buckets := BuildBuckets(w)
	tz := ""
	if user != nil {
		tz = user.Timezone
	}
	return storage.StatsParams{
		Range:       string(rk),
		Timezone:    tz,
		Loc:         loc,
		CurStart:    w.CurStart,
		CurEnd:      w.CurEnd,
		HasPrev:     w.HasPrev,
		PrevStart:   w.PrevStart,
		PrevEnd:     w.PrevEnd,
		BucketKind:  string(w.Bucket),
		SeriesStart: w.SeriesStart,
		SeriesEnd:   w.SeriesEnd,
		Buckets:     buckets,
	}
}

func (h *AdminHandler) StatsSummary(w http.ResponseWriter, r *http.Request) {
	params := h.buildStatsParams(r)
	stats, err := storage.GetStatsSummary(h.DB, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// StatsSummaryStream pushes stats updates via Server-Sent Events.
// Emits an initial payload immediately, then re-emits whenever a new request log
// is inserted (throttled to at most one push every 200ms).
func (h *AdminHandler) StatsSummaryStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sendStats := func() bool {
		params := h.buildStatsParams(r)
		stats, err := storage.GetStatsSummary(h.DB, params)
		if err != nil {
			// Best effort: send an error event and continue.
			fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
			flusher.Flush()
			return true
		}
		b, err := json.Marshal(stats)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// Initial payload
	if !sendStats() {
		return
	}

	sub := events.Global.Subscribe()
	defer events.Global.Unsubscribe(sub)

	ctx := r.Context()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	throttle := 200 * time.Millisecond
	var lastPush time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub:
			// Throttle: if we pushed recently, wait a bit.
			if wait := throttle - time.Since(lastPush); wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
			// Drain any additional events that piled up during the wait.
			select {
			case <-sub:
			default:
			}
			if !sendStats() {
				return
			}
			lastPush = time.Now()
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// --- Request Logs ---

func (h *AdminHandler) ListRequestLogs(w http.ResponseWriter, r *http.Request) {
	// Parse query params for pagination
	limit := 50
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := parseInt(l); err == nil && v > 0 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := parseInt(o); err == nil && v >= 0 {
			offset = v
		}
	}

	filter := storage.RequestLogFilter{
		Limit:  limit,
		Offset: offset,
	}

	logs, err := storage.ListRequestLogs(h.DB, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []storage.RequestLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

// RequestLogsStream notifies the client whenever a request log is inserted or
// updated via Server-Sent Events. The payload is intentionally empty (the
// frontend simply re-fetches its list); this keeps the server side simple and
// lets the client apply its own paging/filter.
func (h *AdminHandler) RequestLogsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sendUpdate := func() bool {
		if _, err := fmt.Fprintf(w, "event: update\ndata: {}\n\n"); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// Prime the connection so the client knows the stream is live.
	if !sendUpdate() {
		return
	}

	sub := events.Global.Subscribe()
	defer events.Global.Unsubscribe(sub)

	ctx := r.Context()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	throttle := 200 * time.Millisecond
	var lastPush time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub:
			if wait := throttle - time.Since(lastPush); wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
			// Coalesce any events accumulated during the throttle window.
			select {
			case <-sub:
			default:
			}
			if !sendUpdate() {
				return
			}
			lastPush = time.Now()
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// --- Provider Models CRUD ---

func (h *AdminHandler) ListProviderModels(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")

	// Verify provider exists
	provider, err := storage.GetProvider(h.DB, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if provider == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	models, err := storage.ListProviderModels(h.DB, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if models == nil {
		models = []storage.ProviderModel{}
	}
	writeJSON(w, http.StatusOK, models)
}

type CreateProviderModelRequest struct {
	ModelName   string `json:"model_name"`
	DisplayName string `json:"display_name,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

func (h *AdminHandler) CreateProviderModel(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")

	// Verify provider exists
	provider, err := storage.GetProvider(h.DB, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if provider == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	var req CreateProviderModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ModelName == "" {
		writeError(w, http.StatusBadRequest, "model_name is required")
		return
	}

	now := storage.Now()
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	model := &storage.ProviderModel{
		ID:          uuid.New().String(),
		ProviderID:  providerID,
		ModelName:   req.ModelName,
		DisplayName: req.DisplayName,
		Enabled:     enabled,
		Source:      "manual",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := storage.CreateProviderModel(h.DB, model); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, model)
}

type UpdateProviderModelRequest struct {
	ModelName   *string `json:"model_name,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

func (h *AdminHandler) UpdateProviderModel(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")
	modelID := chi.URLParam(r, "modelId")

	existing, err := storage.GetProviderModel(h.DB, modelID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "provider model not found")
		return
	}
	if existing.ProviderID != providerID {
		writeError(w, http.StatusNotFound, "provider model not found")
		return
	}

	var req UpdateProviderModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ModelName != nil {
		existing.ModelName = *req.ModelName
	}
	if req.DisplayName != nil {
		existing.DisplayName = *req.DisplayName
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	existing.UpdatedAt = storage.Now()

	if err := storage.UpdateProviderModel(h.DB, existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, existing)
}

func (h *AdminHandler) DeleteProviderModel(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")
	modelID := chi.URLParam(r, "modelId")

	existing, err := storage.GetProviderModel(h.DB, modelID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "provider model not found")
		return
	}
	if existing.ProviderID != providerID {
		writeError(w, http.StatusNotFound, "provider model not found")
		return
	}

	if err := storage.DeleteProviderModel(h.DB, modelID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// --- Provider Models Import ---

type ImportProviderModelsRequest struct {
	Models []ImportProviderModelItem `json:"models"`
}

type ImportProviderModelItem struct {
	ModelName   string `json:"model_name"`
	DisplayName string `json:"display_name,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

func (h *AdminHandler) ImportProviderModels(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")

	// Verify provider exists
	provider, err := storage.GetProvider(h.DB, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if provider == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	var req ImportProviderModelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Models) == 0 {
		writeError(w, http.StatusBadRequest, "models list is required")
		return
	}

	now := storage.Now()
	var models []storage.ProviderModel
	for _, item := range req.Models {
		if item.ModelName == "" {
			continue
		}
		enabled := true
		if item.Enabled != nil {
			enabled = *item.Enabled
		}
		models = append(models, storage.ProviderModel{
			ID:          uuid.New().String(),
			ProviderID:  providerID,
			ModelName:   item.ModelName,
			DisplayName: item.DisplayName,
			Enabled:     enabled,
			Source:      "upstream",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	if len(models) == 0 {
		writeError(w, http.StatusBadRequest, "no valid models to import")
		return
	}

	result, err := storage.ImportProviderModels(h.DB, models)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		result = []storage.ProviderModel{}
	}

	writeJSON(w, http.StatusOK, result)
}

// --- Provider Fetch Models (upstream, not persisted) ---

func (h *AdminHandler) FetchProviderModels(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")

	// Verify provider exists
	provider, err := storage.GetProvider(h.DB, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if provider == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	models, err := providerapi.FetchModels(r.Context(), provider)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("fetch models failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, models)
}

// --- Provider Fetch Models Preview (for unsaved providers) ---

type FetchModelsPreviewRequest struct {
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

func (h *AdminHandler) FetchModelsPreview(w http.ResponseWriter, r *http.Request) {
	var req FetchModelsPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Type == "" || req.BaseURL == "" || req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "type, base_url, and api_key are required")
		return
	}

	// Validate provider type
	if !protocol.IsValidProtocol(req.Type) {
		writeError(w, http.StatusBadRequest, "invalid provider type")
		return
	}

	// Create a temporary provider object for fetching models
	provider := &storage.Provider{
		Type:    req.Type,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
	}

	models, err := providerapi.FetchModels(r.Context(), provider)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("fetch models failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, models)
}

// --- Provider Test Connection ---

func (h *AdminHandler) TestProviderConnection(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")

	// Verify provider exists
	provider, err := storage.GetProvider(h.DB, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if provider == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	if err := providerapi.TestConnection(r.Context(), provider); err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("connection test failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// parseInt is a helper to parse a string to int, returning error on failure.
func parseInt(s string) (int, error) {
	var v int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		v = v*10 + int(c-'0')
	}
	return v, nil
}
