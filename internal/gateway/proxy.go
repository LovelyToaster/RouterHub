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
		writeError(w, http.StatusInternalServerError, "failed to read request body")
		return
	}
	_ = r.Body.Close()

	// Replace model in body
	modifiedBody, err := replaceModelInBody(bodyBytes, selected.ProviderModel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to modify request body")
		return
	}

	// Create outgoing request
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(modifiedBody))
	if err != nil {
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
			errMsg := fmt.Sprintf("upstream returned status %d", resp.StatusCode)
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
		errMsg := fmt.Sprintf("upstream returned status %d", resp.StatusCode)
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
		writeError(w, http.StatusInternalServerError, "failed to read request body")
		return
	}
	_ = r.Body.Close()

	// Replace model in body first
	modifiedBody, err := replaceModelInBody(bodyBytes, selected.ProviderModel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to modify request body")
		return
	}

	// Convert request body from inbound protocol to provider protocol
	convertedBody, err := convert.ConvertRequest(modifiedBody, inboundProtocol, selected.Provider.Type)
	if err != nil {
		logEntry.Status = "error"
		errMsg := fmt.Sprintf("request conversion failed: %v", err)
		logEntry.ErrorMessage = &errMsg
		finishedAt := storage.Now()
		logEntry.FinishedAt = &finishedAt
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	// Create outgoing request
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(convertedBody))
	if err != nil {
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
			errMsg := fmt.Sprintf("upstream returned status %d", resp.StatusCode)
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

// handleConvertedStream handles streaming response with event-by-event conversion.
func handleConvertedStream(w http.ResponseWriter, resp *http.Response, inboundProtocol, providerType string, logEntry *storage.RequestLog, startTime time.Time) {
	// Set headers for streaming
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

	if resp.StatusCode >= 400 {
		// For error responses, just forward the body as-is
		body, _ := io.ReadAll(resp.Body)
		_, _ = w.Write(body)
		logEntry.Status = "error"
		errMsg := fmt.Sprintf("upstream returned status %d", resp.StatusCode)
		logEntry.ErrorMessage = &errMsg
		return
	}

	firstChunk := true
	flusher, canFlush := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer for large lines (e.g., tool call arguments)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lastUsage *convert.StreamUsage

	for scanner.Scan() {
		line := scanner.Text()

		// Forward blank lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
				break
			}
			if canFlush {
				flusher.Flush()
			}
			continue
		}

		// Forward event type lines
		if strings.HasPrefix(line, "event:") {
			if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
				break
			}
			if canFlush {
				flusher.Flush()
			}
			continue
		}

		// Process data lines
		if strings.HasPrefix(line, "data:") {
			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

			// Try to parse usage from the original event (side channel)
			if usage := convert.ParseStreamUsage([]byte(dataStr), providerType); usage != nil {
				lastUsage = usage
			}

			// Convert the event
			convertedData, err := convert.ConvertStreamEvent([]byte(dataStr), inboundProtocol, providerType)
			if err != nil {
				// If conversion fails, skip the event (don't break streaming)
				continue
			}

			if convertedData == nil {
				// Event should be skipped
				continue
			}

			// Track first token time
			if firstChunk {
				ttft := time.Since(startTime).Milliseconds()
				logEntry.TimeToFirstTokenMs = &ttft
				firstChunk = false
			}

			// Write the converted event
			if _, err := fmt.Fprintf(w, "data: %s\n\n", string(convertedData)); err != nil {
				break
			}
			if canFlush {
				flusher.Flush()
			}
			continue
		}

		// Forward any other lines as-is
		if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
			break
		}
		if canFlush {
			flusher.Flush()
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

// SaveRequestLog persists the request log entry to the database.
func SaveRequestLog(db *sql.DB, logEntry *storage.RequestLog) {
	if err := storage.InsertRequestLog(db, logEntry); err != nil {
		// Log failure but don't block the response
		fmt.Printf("Failed to save request log: %v\n", err)
		return
	}
	// Notify subscribers (e.g., dashboard SSE) of stats change.
	events.Global.Publish()
}
