package gateway

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lovelytoaster94/routerhub/internal/convert"
	"github.com/lovelytoaster94/routerhub/internal/events"
	"github.com/lovelytoaster94/routerhub/internal/protocol"
	"github.com/lovelytoaster94/routerhub/internal/storage"
)

// hop-by-hop headers that must be removed when forwarding
var hopByHopHeaders = map[string]bool{
	"Host":                true,
	"Content-Length":      true,
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"TE":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

// shared HTTP client with reasonable timeout, reused across requests
var sharedClient = &http.Client{Timeout: 5 * time.Minute}

// ProxyRequest forwards the request to the provider and returns the response.
// It handles header forwarding, auth header setting, and model replacement.
// stream indicates whether the request is a streaming request.
func ProxyRequest(w http.ResponseWriter, r *http.Request, selected *SelectedProvider, inboundProtocol string, logEntry *storage.RequestLog, stream bool) {
	startTime := time.Now()

	// Build the target URL using base_url + endpoint path determined by provider type
	targetURL := strings.TrimRight(selected.Provider.BaseURL, "/") + protocol.EndpointByProviderType(selected.Provider.Type)

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		setLogError(logEntry, err.Error())
		writeError(w, http.StatusInternalServerError, "failed to read request body")
		return
	}
	_ = r.Body.Close()

	// Replace model in body
	modifiedBody, err := replaceModelInBody(bodyBytes, selected.ProviderModel)
	if err != nil {
		setLogError(logEntry, err.Error())
		writeError(w, http.StatusInternalServerError, "failed to modify request body")
		return
	}

	// Create outgoing request
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(modifiedBody))
	if err != nil {
		setLogError(logEntry, err.Error())
		writeError(w, http.StatusInternalServerError, "failed to create request")
		return
	}

	// Copy headers (except auth and hop-by-hop)
	for key, values := range r.Header {
		keyLower := strings.ToLower(key)
		// Skip gateway auth headers
		if keyLower == "authorization" || keyLower == "x-api-key" {
			continue
		}
		// Drop Accept-Encoding so Go's transport handles gzip transparently;
		// otherwise the upstream may return a gzipped body that we forward
		// as-is, breaking token-usage parsing in the non-streaming path.
		if keyLower == "accept-encoding" {
			continue
		}
		// Skip hop-by-hop headers
		if hopByHopHeaders[key] {
			continue
		}
		for _, v := range values {
			outReq.Header.Add(key, v)
		}
	}

	// Set provider auth header based on provider type
	setProviderAuth(outReq, selected.Provider)

	// Send request using shared client
	resp, err := sharedClient.Do(outReq)
	if err != nil {
		logEntry.Status = "error"
		errMsg := err.Error()
		logEntry.ErrorMessage = &errMsg
		writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	// Copy response headers, filtering hop-by-hop headers
	for key, values := range resp.Header {
		if hopByHopHeaders[key] {
			continue
		}
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}

	// Write status code
	w.WriteHeader(resp.StatusCode)

	now := storage.Now()
	logEntry.FinishedAt = &now
	durationMs := time.Since(startTime).Milliseconds()
	logEntry.TotalDurationMs = &durationMs

	if stream {
		// Streaming: forward chunks as they arrive, record first token time
		// Also parse SSE events for usage information (side channel, best effort)
		handleSameProtocolStream(w, resp, inboundProtocol, logEntry, startTime)
	} else {
		// Non-streaming: read full body
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			logEntry.Status = "error"
			errMsg := readErr.Error()
			logEntry.ErrorMessage = &errMsg
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"failed to read upstream response: %v"}`, readErr)))
			return
		}

		// Set time_to_first_token_ms to total_duration_ms for non-streaming
		logEntry.TimeToFirstTokenMs = &durationMs

		// Determine status based on response code
		if resp.StatusCode >= 400 {
			logEntry.Status = "error"
			errMsg := fmt.Sprintf("upstream returned status %d: %s", resp.StatusCode, string(respBody))
			logEntry.ErrorMessage = &errMsg
		} else {
			logEntry.Status = "success"
		}

		// Try to parse usage from response (best effort, non-streaming only)
		if resp.StatusCode < 400 {
			parseUsageFromResponse(respBody, logEntry, inboundProtocol)
		}

		// Write body
		_, _ = w.Write(respBody)
	}
}

// handleSameProtocolStream handles streaming for same-protocol passthrough.
// It forwards SSE events as-is while parsing usage information as a side channel.
// Note: headers and status code are already set by ProxyRequest before calling this function.
func handleSameProtocolStream(w http.ResponseWriter, resp *http.Response, inboundProtocol string, logEntry *storage.RequestLog, startTime time.Time) {
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		_, _ = w.Write(body)
		logEntry.Status = "error"
		errMsg := fmt.Sprintf("upstream returned status %d: %s", resp.StatusCode, string(body))
		logEntry.ErrorMessage = &errMsg
		return
	}

	firstChunk := true
	flusher, canFlush := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lastUsage *convert.StreamUsage

	for scanner.Scan() {
		line := scanner.Text()

		// Forward the line as-is
		if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
			break
		}
		if canFlush {
			flusher.Flush()
		}

		// Track first token time on first data line
		if strings.HasPrefix(line, "data:") {
			if firstChunk {
				ttft := time.Since(startTime).Milliseconds()
				logEntry.TimeToFirstTokenMs = &ttft
				firstChunk = false
			}

			// Parse usage from every data line (side channel)
			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if usage := convert.ParseStreamUsage([]byte(dataStr), inboundProtocol); usage != nil {
				lastUsage = usage
			}
		}
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		logEntry.Status = "error"
		errMsg := fmt.Sprintf("stream read error: %v", err)
		logEntry.ErrorMessage = &errMsg
	} else {
		logEntry.Status = "success"
	}

	// Record usage from stream if available
	if lastUsage != nil {
		logEntry.InputTokens = lastUsage.InputTokens
		logEntry.OutputTokens = lastUsage.OutputTokens
		logEntry.TotalTokens = lastUsage.TotalTokens
		logEntry.CachedTokens = lastUsage.CachedTokens
		logEntry.CacheWriteTokens = lastUsage.CacheWriteTokens
	}

	// Record total duration
	durationMs := time.Since(startTime).Milliseconds()
	logEntry.TotalDurationMs = &durationMs

	// If no first token was recorded, use total duration
	if logEntry.TimeToFirstTokenMs == nil {
		logEntry.TimeToFirstTokenMs = &durationMs
	}
}

// ConvertedProxyRequest forwards a request with cross-protocol conversion.
// It converts the request body, forwards to the provider, converts the response back,
// and handles streaming conversion when applicable.
func ConvertedProxyRequest(w http.ResponseWriter, r *http.Request, selected *SelectedProvider, inboundProtocol string, logEntry *storage.RequestLog, stream bool) {
	startTime := time.Now()

	// Build the target URL using base_url + endpoint path determined by provider type
	targetURL := strings.TrimRight(selected.Provider.BaseURL, "/") + protocol.EndpointByProviderType(selected.Provider.Type)

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		setLogError(logEntry, err.Error())
		writeError(w, http.StatusInternalServerError, "failed to read request body")
		return
	}
	_ = r.Body.Close()

	// Replace model in body first
	modifiedBody, err := replaceModelInBody(bodyBytes, selected.ProviderModel)
	if err != nil {
		setLogError(logEntry, err.Error())
		writeError(w, http.StatusInternalServerError, "failed to modify request body")
		return
	}

	// Convert request body from inbound protocol to provider protocol
	convertedBody, err := convert.ConvertRequest(modifiedBody, inboundProtocol, selected.Provider.Type)
	if err != nil {
		errMsg := fmt.Sprintf("request conversion failed: %v", err)
		setLogError(logEntry, errMsg)
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	// Create outgoing request
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(convertedBody))
	if err != nil {
		setLogError(logEntry, err.Error())
		writeError(w, http.StatusInternalServerError, "failed to create request")
		return
	}

	// Copy headers (except auth and hop-by-hop)
	for key, values := range r.Header {
		keyLower := strings.ToLower(key)
		if keyLower == "authorization" || keyLower == "x-api-key" {
			continue
		}
		// Drop Accept-Encoding so Go's transport handles gzip transparently;
		// see ProxyRequest for the rationale.
		if keyLower == "accept-encoding" {
			continue
		}
		if hopByHopHeaders[key] {
			continue
		}
		for _, v := range values {
			outReq.Header.Add(key, v)
		}
	}

	// Set provider auth header
	setProviderAuth(outReq, selected.Provider)

	// Send request
	resp, err := sharedClient.Do(outReq)
	if err != nil {
		logEntry.Status = "error"
		errMsg := err.Error()
		logEntry.ErrorMessage = &errMsg
		writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	now := storage.Now()
	logEntry.FinishedAt = &now

	if stream {
		// Streaming: convert each SSE event
		handleConvertedStream(w, resp, inboundProtocol, selected.Provider.Type, logEntry, startTime)
	} else {
		// Non-streaming: read full body, convert response
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			logEntry.Status = "error"
			errMsg := readErr.Error()
			logEntry.ErrorMessage = &errMsg
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"failed to read upstream response: %v"}`, readErr)))
			return
		}

		durationMs := time.Since(startTime).Milliseconds()
		logEntry.TotalDurationMs = &durationMs
		logEntry.TimeToFirstTokenMs = &durationMs

		if resp.StatusCode >= 400 {
			logEntry.Status = "error"
			errMsg := fmt.Sprintf("upstream returned status %d: %s", resp.StatusCode, string(respBody))
			logEntry.ErrorMessage = &errMsg
			// Return the original error response
			for key, values := range resp.Header {
				if hopByHopHeaders[key] {
					continue
				}
				for _, v := range values {
					w.Header().Add(key, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(respBody)
			return
		}

		// Convert response back to inbound protocol
		convertedResp, err := convert.ConvertResponse(respBody, inboundProtocol, selected.Provider.Type)
		if err != nil {
			logEntry.Status = "error"
			errMsg := fmt.Sprintf("response conversion failed: %v", err)
			logEntry.ErrorMessage = &errMsg
			writeError(w, http.StatusInternalServerError, errMsg)
			return
		}

		logEntry.Status = "success"

		// Try to parse usage from original response (best effort)
		parseUsageFromResponse(respBody, logEntry, selected.Provider.Type)

		// Write converted response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(convertedResp)
	}
}

// handleConvertedStream handles streaming response with cross-protocol conversion.
// It uses a state machine (streamState) to generate proper SSE events with
// lifecycle events, tool-call fragment accumulation, and stream termination.
func handleConvertedStream(w http.ResponseWriter, resp *http.Response, inboundProtocol, providerType string, logEntry *storage.RequestLog, startTime time.Time) {
	if resp.StatusCode >= 400 {
		// For error responses, copy original headers (filtering hop-by-hop)
		// and forward the body as-is, preserving the original Content-Type.
		for key, values := range resp.Header {
			if hopByHopHeaders[key] {
				continue
			}
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		_, _ = w.Write(body)
		logEntry.Status = "error"
		errMsg := fmt.Sprintf("upstream returned status %d", resp.StatusCode)
		logEntry.ErrorMessage = &errMsg
		return
	}

	// Normal SSE streaming: force SSE headers.
	for key, values := range resp.Header {
		if hopByHopHeaders[key] {
			continue
		}
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	state := newStreamState(inboundProtocol, providerType, logEntry.RequestID, logEntry.ActualModel, startTime, logEntry)

	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer for large lines (e.g., tool call arguments)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Ignore blank lines, comments, and upstream event: lines.
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}

		if strings.HasPrefix(line, "data:") {
			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataStr == "[DONE]" {
				continue // writeStreamEnd handles termination
			}
			// Usage side channel
			if usage := convert.ParseStreamUsage([]byte(dataStr), providerType); usage != nil {
				state.SetUsage(&convert.StreamUsage{
					InputTokens:      usage.InputTokens,
					OutputTokens:     usage.OutputTokens,
					TotalTokens:      usage.TotalTokens,
					CachedTokens:     usage.CachedTokens,
					CacheWriteTokens: usage.CacheWriteTokens,
				})
			}
			state.processUpstreamData(w, flusher, []byte(dataStr))
			continue
		}
		// Other lines silently dropped.
	}

	state.writeStreamEnd(w, flusher)

	if err := scanner.Err(); err != nil {
		logEntry.Status = "error"
		errMsg := fmt.Sprintf("stream read error: %v", err)
		logEntry.ErrorMessage = &errMsg
	} else if logEntry.Status != "error" {
		logEntry.Status = "success"
	}

	// Persist final usage from state.lastUsage if available.
	if state.lastUsage != nil {
		logEntry.InputTokens = state.lastUsage.InputTokens
		logEntry.OutputTokens = state.lastUsage.OutputTokens
		logEntry.TotalTokens = state.lastUsage.TotalTokens
		logEntry.CachedTokens = state.lastUsage.CachedTokens
		logEntry.CacheWriteTokens = state.lastUsage.CacheWriteTokens
	}

	durationMs := time.Since(startTime).Milliseconds()
	logEntry.TotalDurationMs = &durationMs
	if logEntry.TimeToFirstTokenMs == nil {
		logEntry.TimeToFirstTokenMs = &durationMs
	}
}

// replaceModelInBody replaces the "model" field in a JSON body.
func replaceModelInBody(body []byte, newModel string) ([]byte, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		// If body is not valid JSON, return as-is
		return body, nil
	}
	data["model"] = newModel
	return json.Marshal(data)
}

// setProviderAuth sets the appropriate auth header based on provider type.
func setProviderAuth(req *http.Request, provider storage.Provider) {
	switch provider.Type {
	case protocol.ProtocolChatCompletions, protocol.ProtocolResponses:
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	case protocol.ProtocolAnthropic:
		req.Header.Set("x-api-key", provider.APIKey)
	}
}

// parseUsageFromResponse attempts to extract token usage from the response.
func parseUsageFromResponse(body []byte, logEntry *storage.RequestLog, inboundProtocol string) {
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		// This should not happen for a successful upstream response, so log
		// enough context to diagnose it (e.g. an unexpected Content-Encoding
		// slipping through, or a non-JSON body from a misbehaving upstream).
		fmt.Printf("parseUsageFromResponse: json unmarshal failed request_id=%s protocol=%s err=%v\n",
			logEntry.RequestID, inboundProtocol, err)
		return
	}

	switch inboundProtocol {
	case protocol.ProtocolChatCompletions:
		if usage, ok := data["usage"].(map[string]interface{}); ok {
			if v, ok := usage["prompt_tokens"].(float64); ok {
				logEntry.InputTokens = int64(v)
			}
			if v, ok := usage["completion_tokens"].(float64); ok {
				logEntry.OutputTokens = int64(v)
			}
			if v, ok := usage["total_tokens"].(float64); ok {
				logEntry.TotalTokens = int64(v)
			}
			// OpenAI Chat: prompt_tokens_details.cached_tokens
			if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
				if v, ok := details["cached_tokens"].(float64); ok {
					logEntry.CachedTokens = int64(v)
				}
			}
		}
	case protocol.ProtocolResponses:
		if usage, ok := data["usage"].(map[string]interface{}); ok {
			if v, ok := usage["input_tokens"].(float64); ok {
				logEntry.InputTokens = int64(v)
			}
			if v, ok := usage["output_tokens"].(float64); ok {
				logEntry.OutputTokens = int64(v)
			}
			if v, ok := usage["total_tokens"].(float64); ok {
				logEntry.TotalTokens = int64(v)
			}
			// OpenAI Responses: input_tokens_details.cached_tokens
			if details, ok := usage["input_tokens_details"].(map[string]interface{}); ok {
				if v, ok := details["cached_tokens"].(float64); ok {
					logEntry.CachedTokens = int64(v)
				}
			}
		}
	case protocol.ProtocolAnthropic:
		if usage, ok := data["usage"].(map[string]interface{}); ok {
			// Anthropic reports three independent input counters: raw input,
			// cache-read, cache-creation. We normalise them so that
			// InputTokens == "total input tokens including cache reads/writes",
			// matching the OpenAI convention. CachedTokens keeps only the read
			// portion; CacheWriteTokens keeps the creation portion separately.
			var rawInput, cacheRead, cacheWrite int64
			if v, ok := usage["input_tokens"].(float64); ok {
				rawInput = int64(v)
			}
			if v, ok := usage["output_tokens"].(float64); ok {
				logEntry.OutputTokens = int64(v)
			}
			if v, ok := usage["cache_read_input_tokens"].(float64); ok {
				cacheRead = int64(v)
			}
			if v, ok := usage["cache_creation_input_tokens"].(float64); ok {
				cacheWrite = int64(v)
			}
			logEntry.InputTokens = rawInput + cacheRead + cacheWrite
			logEntry.CachedTokens = cacheRead
			logEntry.CacheWriteTokens = cacheWrite
			// Anthropic doesn't have total_tokens in the same way
			logEntry.TotalTokens = logEntry.InputTokens + logEntry.OutputTokens
		}
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// setLogError marks the log entry as an error with the given message and, when
// provided, records a finished_at timestamp. Safe to call multiple times; the
// latest call wins.
func setLogError(logEntry *storage.RequestLog, msg string) {
	if logEntry == nil {
		return
	}
	logEntry.Status = "error"
	m := msg
	logEntry.ErrorMessage = &m
}

// InsertPendingRequestLog persists a freshly-created log row in "pending"
// state. Errors are logged but not returned so the request flow is never
// blocked by logging infrastructure issues.
func InsertPendingRequestLog(db *sql.DB, logEntry *storage.RequestLog) {
	if err := storage.InsertRequestLog(db, logEntry); err != nil {
		fmt.Printf("Failed to insert pending request log: %v\n", err)
		return
	}
	events.Global.Publish()
}

// FinalizeRequestLog updates the previously-inserted pending row with the
// final status/timings/usage carried on logEntry. If Status is somehow still
// "pending" here (missing branch or a recovered panic), fall back to "error"
// with a diagnostic message so we never leave a row stuck as "processing".
// Also fills in FinishedAt and TotalDurationMs when the handler forgot.
func FinalizeRequestLog(db *sql.DB, logEntry *storage.RequestLog) {
	if logEntry.Status == "pending" {
		logEntry.Status = "error"
		msg := "status not finalized"
		logEntry.ErrorMessage = &msg
	}
	if logEntry.FinishedAt == nil {
		now := storage.Now()
		logEntry.FinishedAt = &now
	}
	// Reconstruct total_duration_ms from created_at/finished_at when the
	// handler left it nil (e.g. an early-return error path). Without this,
	// error rows would have NULL duration which hurts triage.
	if logEntry.TotalDurationMs == nil && logEntry.FinishedAt != nil {
		if created, err := time.Parse(time.RFC3339, logEntry.CreatedAt); err == nil {
			if finished, err := time.Parse(time.RFC3339, *logEntry.FinishedAt); err == nil {
				ms := finished.Sub(created).Milliseconds()
				if ms < 0 {
					ms = 0
				}
				logEntry.TotalDurationMs = &ms
			}
		}
	}
	if err := storage.UpdateRequestLog(db, logEntry); err != nil {
		fmt.Printf("Failed to finalize request log: %v\n", err)
		return
	}
	events.Global.Publish()
}
