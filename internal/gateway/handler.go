package gateway

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/lovelytoaster94/routerhub/internal/protocol"
	"github.com/lovelytoaster94/routerhub/internal/storage"
)

// GatewayHandler handles the three LLM API endpoints.
type GatewayHandler struct {
	DB *sql.DB
}

func NewGatewayHandler(db *sql.DB) *GatewayHandler {
	return &GatewayHandler{DB: db}
}

// ServeHTTP dispatches to the appropriate handler based on path.
func (h *GatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	inboundProtocol := protocol.DetectProtocolFromPath(r.URL.Path)
	if inboundProtocol == "" {
		http.Error(w, `{"error":"unknown endpoint"}`, http.StatusNotFound)
		return
	}

	// Only allow POST
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	h.handleProxy(w, r, inboundProtocol)
}

func (h *GatewayHandler) handleProxy(w http.ResponseWriter, r *http.Request, inboundProtocol string) {
	// Read body once (limit to 1MB)
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	_ = r.Body.Close()

	// Replace body for downstream use
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Extract model from body
	model, stream, err := parseRequestBody(bodyBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	if model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	// Select provider
	selected, err := SelectProvider(h.DB, model, inboundProtocol)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("model '%s' not available", model))
		return
	}

	// Create pending log entry and persist immediately so the UI can show the
	// request as "processing" while it flows through. The finalize call at the
	// end of this function updates the same row with success/error status,
	// timings and usage stats.
	requestID := uuid.New().String()
	now := storage.Now()
	logEntry := &storage.RequestLog{
		RequestID:        requestID,
		ProviderName:     selected.Provider.Name,
		ProviderType:     selected.Provider.Type,
		InboundProtocol:  inboundProtocol,
		RequestedModel:   model,
		ActualModel:      selected.ProviderModel,
		Stream:           stream,
		Status:           "pending",
		CreatedAt:        now,
		ClientIP:         extractClientIP(r),
	}
	if key := GetGatewayKeyFromContext(r.Context()); key != nil {
		logEntry.GatewayAPIKeyName = key.Name
	}
	InsertPendingRequestLog(h.DB, logEntry)
	// Deferred so that a panic inside ProxyRequest / ConvertedProxyRequest
	// (recovered by chi's middleware) still finalizes the log row instead of
	// leaving it stuck on "pending" until the next process restart.
	defer FinalizeRequestLog(h.DB, logEntry)

	// Check protocol compatibility
	if inboundProtocol != selected.Provider.Type {
		// Cross-protocol conversion
		ConvertedProxyRequest(w, r, selected, inboundProtocol, logEntry, stream)
	} else {
		// Same protocol - proxy through
		ProxyRequest(w, r, selected, inboundProtocol, logEntry, stream)
	}
}

// parseRequestBody extracts model and stream flag from the JSON body.
func parseRequestBody(body []byte) (model string, stream bool, err error) {
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", false, fmt.Errorf("invalid json: %w", err)
	}

	model, _ = data["model"].(string)

	if s, ok := data["stream"]; ok {
		if b, ok := s.(bool); ok {
			stream = b
		}
	}

	return model, stream, nil
}

// extractClientIP resolves the client's IP address using standard proxy
// headers, falling back to the connection remote address. Returns an empty
// string only when everything fails (should never happen in practice).
func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first hop (client), which is left-most.
		if idx := strings.IndexByte(xff, ','); idx >= 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}
	// r.RemoteAddr is "IP:port"; strip the port. IPv6 form is "[::1]:1234".
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
