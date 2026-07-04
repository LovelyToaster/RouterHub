package protocol

import (
	"strings"
)

// Protocol constants
const (
	ProtocolChatCompletions = "openai-chat-completions"
	ProtocolResponses       = "openai-responses"
	ProtocolAnthropic       = "anthropic-messages"
)

// DetectProtocolFromPath returns the protocol type based on the request path.
func DetectProtocolFromPath(path string) string {
	path = strings.TrimSuffix(path, "/")
	switch path {
	case "/v1/chat/completions":
		return ProtocolChatCompletions
	case "/v1/responses":
		return ProtocolResponses
	case "/v1/messages":
		return ProtocolAnthropic
	default:
		return ""
	}
}

// IsValidProtocol returns true if the given protocol is one of the supported types.
func IsValidProtocol(p string) bool {
	switch p {
	case ProtocolChatCompletions, ProtocolResponses, ProtocolAnthropic:
		return true
	default:
		return false
	}
}

// EndpointByProviderType returns the API endpoint path for a given provider type.
// The base_url is expected to be filled up to /v1, and this function returns the
// remaining path component.
func EndpointByProviderType(providerType string) string {
	switch providerType {
	case ProtocolChatCompletions:
		return "/chat/completions"
	case ProtocolResponses:
		return "/responses"
	case ProtocolAnthropic:
		return "/messages"
	default:
		return "/chat/completions"
	}
}
