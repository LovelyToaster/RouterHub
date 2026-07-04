// Package convert provides cross-protocol conversion between LLM API formats.
//
// Supported protocols:
//   - openai-chat-completions (Chat)
//   - openai-responses (Responses)
//   - anthropic-messages (Anthropic)
//
// The package uses map[string]any with helper functions for lightweight conversion
// without heavy abstractions. Unsupported fields are silently dropped; required
// missing fields return clear errors.
package convert

import (
	"encoding/json"
	"fmt"
)

// Common errors.
var (
	ErrUnsupportedConversion = fmt.Errorf("unsupported protocol conversion")
	ErrMissingField          = fmt.Errorf("missing required field")
	ErrInvalidField          = fmt.Errorf("invalid field value")
)

// ConvertRequest converts a request body from inbound protocol to provider protocol.
// The body must be valid JSON; the model field should already be set to the provider's model name.
func ConvertRequest(body []byte, inboundProtocol, providerType string) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse request: %w", err)
	}

	switch inboundProtocol {
	case "openai-chat-completions":
		switch providerType {
		case "anthropic-messages":
			return toJSON(convertChatToAnthropic(req))
		case "openai-responses":
			return toJSON(convertChatToResponses(req))
		}
	case "anthropic-messages":
		switch providerType {
		case "openai-chat-completions":
			return toJSON(convertAnthropicToChat(req))
		case "openai-responses":
			return toJSON(convertAnthropicToResponses(req))
		}
	case "openai-responses":
		switch providerType {
		case "openai-chat-completions":
			return toJSON(convertResponsesToChat(req))
		case "anthropic-messages":
			return toJSON(convertResponsesToAnthropic(req))
		}
	}

	return nil, fmt.Errorf("%w: %s -> %s", ErrUnsupportedConversion, inboundProtocol, providerType)
}

// ConvertResponse converts a response body from provider protocol back to inbound protocol.
func ConvertResponse(body []byte, inboundProtocol, providerType string) ([]byte, error) {
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	switch providerType {
	case "openai-chat-completions":
		switch inboundProtocol {
		case "anthropic-messages":
			return toJSON(convertChatResponseToAnthropic(resp))
		case "openai-responses":
			return toJSON(convertChatResponseToResponses(resp))
		}
	case "anthropic-messages":
		switch inboundProtocol {
		case "openai-chat-completions":
			return toJSON(convertAnthropicResponseToChat(resp))
		case "openai-responses":
			return toJSON(convertAnthropicResponseToResponses(resp))
		}
	case "openai-responses":
		switch inboundProtocol {
		case "openai-chat-completions":
			return toJSON(convertResponsesResponseToChat(resp))
		case "anthropic-messages":
			return toJSON(convertResponsesResponseToAnthropic(resp))
		}
	}

	return nil, fmt.Errorf("%w: %s -> %s", ErrUnsupportedConversion, providerType, inboundProtocol)
}

// ConvertStreamEvent converts a single SSE data line from provider protocol to inbound protocol.
// Returns nil if the event should be safely skipped/ignored.
// The data should be the raw bytes after the "data: " prefix (or "[DONE]" for OpenAI).
func ConvertStreamEvent(data []byte, inboundProtocol, providerType string) ([]byte, error) {
	// Handle OpenAI [DONE] sentinel
	if string(data) == "[DONE]" {
		switch inboundProtocol {
		case "anthropic-messages":
			return []byte(`{"type":"message_stop"}`), nil
		case "openai-responses":
			return nil, nil // Responses uses response.completed instead
		default:
			return data, nil
		}
	}

	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("parse stream event: %w", err)
	}

	switch providerType {
	case "openai-chat-completions":
		switch inboundProtocol {
		case "anthropic-messages":
			return toJSON(convertChatStreamToAnthropic(event))
		case "openai-responses":
			return toJSON(convertChatStreamToResponses(event))
		}
	case "anthropic-messages":
		switch inboundProtocol {
		case "openai-chat-completions":
			return toJSON(convertAnthropicStreamToChat(event))
		case "openai-responses":
			return toJSON(convertAnthropicStreamToResponses(event))
		}
	case "openai-responses":
		switch inboundProtocol {
		case "openai-chat-completions":
			return toJSON(convertResponsesStreamToChat(event))
		case "anthropic-messages":
			return toJSON(convertResponsesStreamToAnthropic(event))
		}
	}

	return nil, fmt.Errorf("%w: stream %s -> %s", ErrUnsupportedConversion, providerType, inboundProtocol)
}

// StreamUsage holds token usage information parsed from stream events.
type StreamUsage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

// ParseStreamUsage attempts to extract token usage from a stream event.
// Returns nil if no usage info is present in the event.
// This is a side-channel operation that does not modify the event.
func ParseStreamUsage(data []byte, inboundProtocol string) *StreamUsage {
	if string(data) == "[DONE]" {
		return nil
	}

	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return nil
	}

	switch inboundProtocol {
	case "openai-chat-completions":
		return parseChatStreamUsage(event)
	case "openai-responses":
		return parseResponsesStreamUsage(event)
	case "anthropic-messages":
		return parseAnthropicStreamUsage(event)
	}
	return nil
}

// --- JSON helpers ---

func toJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	// json.Marshal(nil) returns []byte("null"), which we don't want
	if string(b) == "null" {
		return nil, nil
	}
	return b, nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getFloat64(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]any); ok {
			return mm
		}
	}
	return nil
}

func getSlice(m map[string]any, key string) []any {
	if v, ok := m[key]; ok {
		if s, ok := v.([]any); ok {
			return s
		}
	}
	return nil
}

func setIfNotEmpty(m map[string]any, key string, v any) {
	if v == nil {
		return
	}
	switch val := v.(type) {
	case string:
		if val != "" {
			m[key] = val
		}
	case bool:
		m[key] = val
	case float64:
		m[key] = val
	case int64:
		m[key] = val
	case []any:
		if len(val) > 0 {
			m[key] = val
		}
	case map[string]any:
		if len(val) > 0 {
			m[key] = val
		}
	default:
		m[key] = v
	}
}

// copyKnownFields copies known fields from src to dst for passthrough.
func copyKnownFields(dst, src map[string]any, fields ...string) {
	for _, f := range fields {
		if v, ok := src[f]; ok {
			dst[f] = v
		}
	}
}
