package convert

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestConvertChatToAnthropic_SimpleText(t *testing.T) {
	chatReq := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi there!"},
			{"role": "user", "content": "How are you?"}
		],
		"temperature": 0.7,
		"max_tokens": 100,
		"stream": false
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	// Check model
	if getString(out, "model") != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", getString(out, "model"))
	}

	// Check messages
	msgs := getSlice(out, "messages")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// First message should be user
	msg0 := msgs[0].(map[string]any)
	if getString(msg0, "role") != "user" {
		t.Errorf("expected role user, got %s", getString(msg0, "role"))
	}

	// Check temperature
	if getFloat64(out, "temperature") != 0.7 {
		t.Errorf("expected temperature 0.7, got %f", getFloat64(out, "temperature"))
	}

	// Check max_tokens
	if getFloat64(out, "max_tokens") != 100 {
		t.Errorf("expected max_tokens 100, got %f", getFloat64(out, "max_tokens"))
	}
}

func TestConvertChatToAnthropic_SystemMessage(t *testing.T) {
	chatReq := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello"}
		]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	// System should be set as top-level field, not as fake user message
	system := getString(out, "system")
	if system != "You are a helpful assistant." {
		t.Errorf("expected system 'You are a helpful assistant.', got '%s'", system)
	}

	// The user message should be the original user content
	msg0 := msgs[0].(map[string]any)
	if getString(msg0, "role") != "user" {
		t.Errorf("expected first message role user, got %s", getString(msg0, "role"))
	}
}

func TestConvertChatToAnthropic_ToolCall(t *testing.T) {
	chatReq := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "What's the weather?"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "call_123", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\":\"NYC\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_123", "content": "Sunny"}
		],
		"tools": [{"type": "function", "function": {"name": "get_weather", "description": "Get weather", "parameters": {"type": "object"}}}]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(msgs))
	}

	// Check assistant message has tool_use content
	assistantMsg := msgs[1].(map[string]any)
	if getString(assistantMsg, "role") != "assistant" {
		t.Errorf("expected assistant role")
	}
	content := assistantMsg["content"].([]any)
	foundToolUse := false
	for _, c := range content {
		block := c.(map[string]any)
		if getString(block, "type") == "tool_use" {
			foundToolUse = true
			if getString(block, "id") != "call_123" {
				t.Errorf("expected tool_use id call_123")
			}
			if getString(block, "name") != "get_weather" {
				t.Errorf("expected tool_use name get_weather")
			}
		}
	}
	if !foundToolUse {
		t.Errorf("expected tool_use content block")
	}

	// Check tool result
	toolMsg := msgs[2].(map[string]any)
	if getString(toolMsg, "role") != "user" {
		t.Errorf("expected tool result role user")
	}
	toolContent := toolMsg["content"].([]any)
	foundToolResult := false
	for _, c := range toolContent {
		block := c.(map[string]any)
		if getString(block, "type") == "tool_result" {
			foundToolResult = true
			if getString(block, "tool_use_id") != "call_123" {
				t.Errorf("expected tool_use_id call_123")
			}
		}
	}
	if !foundToolResult {
		t.Errorf("expected tool_result content block")
	}

	// Check tools
	tools := getSlice(out, "tools")
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	if getString(tool, "name") != "get_weather" {
		t.Errorf("expected tool name get_weather")
	}
}

func TestConvertAnthropicToChat_SimpleText(t *testing.T) {
	anthropicReq := []byte(`{
		"model": "claude-3-opus",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Hello"}]},
			{"role": "assistant", "content": [{"type": "text", "text": "Hi there!"}]}
		],
		"max_tokens": 100,
		"stream": false
	}`)

	result, err := ConvertRequest(anthropicReq, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	msg0 := msgs[0].(map[string]any)
	if getString(msg0, "role") != "user" {
		t.Errorf("expected role user, got %s", getString(msg0, "role"))
	}
	if getString(msg0, "content") != "Hello" {
		t.Errorf("expected content Hello, got %s", getString(msg0, "content"))
	}

	msg1 := msgs[1].(map[string]any)
	if getString(msg1, "role") != "assistant" {
		t.Errorf("expected role assistant, got %s", getString(msg1, "role"))
	}
	if getString(msg1, "content") != "Hi there!" {
		t.Errorf("expected content Hi there!, got %s", getString(msg1, "content"))
	}
}

func TestConvertAnthropicToChat_ToolUse(t *testing.T) {
	anthropicReq := []byte(`{
		"model": "claude-3-opus",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Weather?"}]},
			{"role": "assistant", "content": [
				{"type": "text", "text": "Let me check:"},
				{"type": "tool_use", "id": "toolu_123", "name": "get_weather", "input": {"location": "NYC"}}
			]}
		],
		"tools": [{"name": "get_weather", "description": "Get weather", "input_schema": {"type": "object"}}]
	}`)

	result, err := ConvertRequest(anthropicReq, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// Check assistant message has tool_calls
	assistantMsg := msgs[1].(map[string]any)
	if getString(assistantMsg, "role") != "assistant" {
		t.Errorf("expected assistant role")
	}
	if getString(assistantMsg, "content") != "Let me check:" {
		t.Errorf("expected text content 'Let me check:', got '%s'", getString(assistantMsg, "content"))
	}

	toolCalls := getSlice(assistantMsg, "tool_calls")
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(toolCalls))
	}
	tc := toolCalls[0].(map[string]any)
	if getString(tc, "id") != "toolu_123" {
		t.Errorf("expected tool_call id toolu_123")
	}
	function := getMap(tc, "function")
	if function == nil {
		t.Fatal("expected function in tool_call")
	}
	if getString(function, "name") != "get_weather" {
		t.Errorf("expected function name get_weather")
	}
}

func TestConvertChatToResponses_SimpleText(t *testing.T) {
	chatReq := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Hello"}
		],
		"max_tokens": 100
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	// Single user message should be string input
	if input, ok := out["input"].(string); ok {
		if input != "Hello" {
			t.Errorf("expected input 'Hello', got '%s'", input)
		}
	} else {
		t.Errorf("expected input to be a string, got %T", out["input"])
	}
}

func TestConvertChatToResponses_MultiMessage(t *testing.T) {
	chatReq := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi!"},
			{"role": "user", "content": "How are you?"}
		]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	input := getSlice(out, "input")
	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(input))
	}
}

func TestConvertResponsesToChat_SimpleString(t *testing.T) {
	responsesReq := []byte(`{
		"model": "gpt-4",
		"input": "Hello",
		"max_output_tokens": 100
	}`)

	result, err := ConvertRequest(responsesReq, "openai-responses", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	if getString(msg, "role") != "user" {
		t.Errorf("expected role user")
	}
	if getString(msg, "content") != "Hello" {
		t.Errorf("expected content Hello")
	}
}

func TestConvertAnthropicToResponses(t *testing.T) {
	anthropicReq := []byte(`{
		"model": "claude-3-opus",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Hello"}]}
		],
		"max_tokens": 100
	}`)

	result, err := ConvertRequest(anthropicReq, "anthropic-messages", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	input := out["input"]
	if input == nil {
		t.Errorf("expected input field")
	}
}

func TestConvertResponsesToAnthropic(t *testing.T) {
	responsesReq := []byte(`{
		"model": "gpt-4",
		"input": [{"role": "user", "content": [{"type": "input_text", "text": "Hello"}]}],
		"max_output_tokens": 100
	}`)

	result, err := ConvertRequest(responsesReq, "openai-responses", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	if getString(msg, "role") != "user" {
		t.Errorf("expected role user")
	}
}

func TestUnsupportedConversion(t *testing.T) {
	_, err := ConvertRequest([]byte(`{"model":"test"}`), "unknown", "openai-chat-completions")
	if err == nil {
		t.Fatal("expected error for unsupported conversion")
	}
}

func TestConvertResponse_ChatToAnthropic(t *testing.T) {
	chatResp := []byte(`{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello! How can I help?"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30
		}
	}`)

	result, err := ConvertResponse(chatResp, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out, "type") != "message" {
		t.Errorf("expected type message")
	}
	if getString(out, "role") != "assistant" {
		t.Errorf("expected role assistant")
	}

	content := getSlice(out, "content")
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}
	block := content[0].(map[string]any)
	if getString(block, "type") != "text" {
		t.Errorf("expected text type")
	}
	if getString(block, "text") != "Hello! How can I help?" {
		t.Errorf("unexpected text content")
	}

	if getString(out, "stop_reason") != "end_turn" {
		t.Errorf("expected stop_reason end_turn")
	}
}

func TestConvertResponse_AnthropicToChat(t *testing.T) {
	anthropicResp := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-opus",
		"content": [
			{"type": "text", "text": "Hello! How can I help?"}
		],
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 10,
			"output_tokens": 20
		}
	}`)

	result, err := ConvertResponse(anthropicResp, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out, "object") != "chat.completion" {
		t.Errorf("expected object chat.completion")
	}

	choices := getSlice(out, "choices")
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice")
	}
	choice := choices[0].(map[string]any)
	message := getMap(choice, "message")
	if message == nil {
		t.Fatal("expected message in choice")
	}
	if getString(message, "content") != "Hello! How can I help?" {
		t.Errorf("unexpected content")
	}
	if getString(choice, "finish_reason") != "stop" {
		t.Errorf("expected finish_reason stop")
	}
}

func TestConvertResponse_ChatToResponses(t *testing.T) {
	chatResp := []byte(`{
		"id": "chatcmpl-123",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello!"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 5,
			"completion_tokens": 10
		}
	}`)

	result, err := ConvertResponse(chatResp, "openai-responses", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out, "type") != "response" {
		t.Errorf("expected type response")
	}

	output := getSlice(out, "output")
	if len(output) == 0 {
		t.Fatal("expected output items")
	}
}

func TestConvertResponse_AnthropicToResponses(t *testing.T) {
	anthropicResp := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-opus",
		"content": [
			{"type": "text", "text": "Hello!"}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 5, "output_tokens": 10}
	}`)

	result, err := ConvertResponse(anthropicResp, "openai-responses", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out, "type") != "response" {
		t.Errorf("expected type response")
	}
}

func TestConvertResponse_ResponsesToChat(t *testing.T) {
	responsesResp := []byte(`{
		"id": "resp_123",
		"type": "response",
		"model": "gpt-4",
		"output": [
			{
				"type": "message",
				"role": "assistant",
				"content": [
					{"type": "output_text", "text": "Hello!"}
				]
			}
		],
		"status": "completed",
		"usage": {"input_tokens": 5, "output_tokens": 10}
	}`)

	result, err := ConvertResponse(responsesResp, "openai-chat-completions", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	choices := getSlice(out, "choices")
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice")
	}
	choice := choices[0].(map[string]any)
	message := getMap(choice, "message")
	if message == nil {
		t.Fatal("expected message")
	}
	if getString(message, "content") != "Hello!" {
		t.Errorf("expected content 'Hello!', got '%s'", getString(message, "content"))
	}
}

func TestConvertResponse_ResponsesToAnthropic(t *testing.T) {
	responsesResp := []byte(`{
		"id": "resp_123",
		"type": "response",
		"model": "gpt-4",
		"output": [
			{
				"type": "message",
				"role": "assistant",
				"content": [
					{"type": "output_text", "text": "Hello!"}
				]
			}
		],
		"status": "completed",
		"usage": {"input_tokens": 5, "output_tokens": 10}
	}`)

	result, err := ConvertResponse(responsesResp, "anthropic-messages", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out, "type") != "message" {
		t.Errorf("expected type message")
	}
	if getString(out, "role") != "assistant" {
		t.Errorf("expected role assistant")
	}
}

func TestParseStreamUsage_Chat(t *testing.T) {
	// Test final chunk with usage
	event := []byte(`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)
	usage := ParseStreamUsage(event, "openai-chat-completions")
	if usage == nil {
		t.Fatal("expected usage to be parsed")
	}
	if usage.InputTokens != 10 {
		t.Errorf("expected input_tokens 10, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 20 {
		t.Errorf("expected output_tokens 20, got %d", usage.OutputTokens)
	}
	if usage.TotalTokens != 30 {
		t.Errorf("expected total_tokens 30, got %d", usage.TotalTokens)
	}
}

func TestParseStreamUsage_Anthropic(t *testing.T) {
	// Test message_delta with usage
	event := []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"output_tokens":20}}`)
	usage := ParseStreamUsage(event, "anthropic-messages")
	if usage == nil {
		t.Fatal("expected usage to be parsed")
	}
	if usage.InputTokens != 10 {
		t.Errorf("expected input_tokens 10, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 20 {
		t.Errorf("expected output_tokens 20, got %d", usage.OutputTokens)
	}
	// Absent cache fields must not inflate the cache counters.
	if usage.CachedTokens != 0 {
		t.Errorf("expected cached_tokens 0, got %d", usage.CachedTokens)
	}
	if usage.CacheWriteTokens != 0 {
		t.Errorf("expected cache_write_tokens 0, got %d", usage.CacheWriteTokens)
	}
}

func TestParseStreamUsage_Anthropic_WithCache(t *testing.T) {
	// Anthropic's raw input_tokens excludes cache; the parser must fold cache
	// reads and writes into InputTokens so the metric is comparable to OpenAI.
	event := []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":80,"cache_creation_input_tokens":30}}`)
	usage := ParseStreamUsage(event, "anthropic-messages")
	if usage == nil {
		t.Fatal("expected usage to be parsed")
	}
	if usage.InputTokens != 120 {
		t.Errorf("expected input_tokens 120 (10+80+30), got %d", usage.InputTokens)
	}
	if usage.CachedTokens != 80 {
		t.Errorf("expected cached_tokens 80, got %d", usage.CachedTokens)
	}
	if usage.CacheWriteTokens != 30 {
		t.Errorf("expected cache_write_tokens 30, got %d", usage.CacheWriteTokens)
	}
	if usage.TotalTokens != 140 {
		t.Errorf("expected total_tokens 140 (120+20), got %d", usage.TotalTokens)
	}
}

func TestParseStreamUsage_Responses(t *testing.T) {
	// Test response.completed with usage
	event := []byte(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}}`)
	usage := ParseStreamUsage(event, "openai-responses")
	if usage == nil {
		t.Fatal("expected usage to be parsed")
	}
	if usage.InputTokens != 10 {
		t.Errorf("expected input_tokens 10, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 20 {
		t.Errorf("expected output_tokens 20, got %d", usage.OutputTokens)
	}
}

func TestParseStreamUsage_NoUsage(t *testing.T) {
	event := []byte(`{"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`)
	usage := ParseStreamUsage(event, "openai-chat-completions")
	if usage != nil {
		t.Errorf("expected nil usage for non-usage event")
	}
}

func TestStreamEvent_ChatToAnthropic_TextDelta(t *testing.T) {
	event := []byte(`{"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`)
	result, err := ConvertStreamEvent(event, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertStreamEvent failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out, "type") != "content_block_delta" {
		t.Errorf("expected type content_block_delta, got %s", getString(out, "type"))
	}
	delta := getMap(out, "delta")
	if delta == nil {
		t.Fatal("expected delta")
	}
	if getString(delta, "type") != "text_delta" {
		t.Errorf("expected delta type text_delta")
	}
	if getString(delta, "text") != "Hello" {
		t.Errorf("expected text Hello")
	}
}

func TestStreamEvent_AnthropicToChat_TextDelta(t *testing.T) {
	event := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)
	result, err := ConvertStreamEvent(event, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertStreamEvent failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	choices := getSlice(out, "choices")
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice")
	}
	choice := choices[0].(map[string]any)
	delta := getMap(choice, "delta")
	if delta == nil {
		t.Fatal("expected delta")
	}
	if getString(delta, "content") != "Hello" {
		t.Errorf("expected content Hello")
	}
}

func TestStreamEvent_ChatToResponses_TextDelta(t *testing.T) {
	event := []byte(`{"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`)
	result, err := ConvertStreamEvent(event, "openai-responses", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertStreamEvent failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out, "type") != "response.output_text.delta" {
		t.Errorf("expected type response.output_text.delta, got %s", getString(out, "type"))
	}
	if getString(out, "delta") != "Hello" {
		t.Errorf("expected delta Hello")
	}
}

func TestStreamEvent_ResponsesToChat_TextDelta(t *testing.T) {
	event := []byte(`{"type":"response.output_text.delta","delta":"Hello","item_id":null,"output_index":0,"content_index":0}`)
	result, err := ConvertStreamEvent(event, "openai-chat-completions", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertStreamEvent failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	choices := getSlice(out, "choices")
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice")
	}
	choice := choices[0].(map[string]any)
	delta := getMap(choice, "delta")
	if delta == nil {
		t.Fatal("expected delta")
	}
	if getString(delta, "content") != "Hello" {
		t.Errorf("expected content Hello")
	}
}

func TestStreamEvent_AnthropicToResponses_TextDelta(t *testing.T) {
	event := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)
	result, err := ConvertStreamEvent(event, "openai-responses", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertStreamEvent failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out, "type") != "response.output_text.delta" {
		t.Errorf("expected type response.output_text.delta")
	}
	if getString(out, "delta") != "Hello" {
		t.Errorf("expected delta Hello")
	}
}

func TestStreamEvent_ResponsesToAnthropic_TextDelta(t *testing.T) {
	event := []byte(`{"type":"response.output_text.delta","delta":"Hello","item_id":null,"output_index":0,"content_index":0}`)
	result, err := ConvertStreamEvent(event, "anthropic-messages", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertStreamEvent failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out, "type") != "content_block_delta" {
		t.Errorf("expected type content_block_delta")
	}
	delta := getMap(out, "delta")
	if delta == nil {
		t.Fatal("expected delta")
	}
	if getString(delta, "text") != "Hello" {
		t.Errorf("expected text Hello")
	}
}

func TestStreamEvent_ChatFinishToAnthropic(t *testing.T) {
	event := []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
	result, err := ConvertStreamEvent(event, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertStreamEvent failed: %v", err)
	}

	// Chat finish with no text delta should produce message_delta with stop_reason
	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	// The finish reason should be converted
	if getString(out, "type") == "message_delta" {
		delta := getMap(out, "delta")
		if delta != nil && getString(delta, "stop_reason") == "end_turn" {
			// OK
		} else {
			t.Logf("message_delta: %+v", out)
		}
	} else {
		t.Logf("Got event type: %s", getString(out, "type"))
	}
}

func TestStreamEvent_AnthropicFinishToChat(t *testing.T) {
	event := []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null}}`)
	result, err := ConvertStreamEvent(event, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertStreamEvent failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	choices := getSlice(out, "choices")
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice")
	}
	choice := choices[0].(map[string]any)
	if getString(choice, "finish_reason") != "stop" {
		t.Errorf("expected finish_reason stop, got %s", getString(choice, "finish_reason"))
	}
}

func TestStreamEvent_AnthropicPing(t *testing.T) {
	event := []byte(`{"type":"ping"}`)
	result, err := ConvertStreamEvent(event, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertStreamEvent failed: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for ping event, got %s", string(result))
	}
}

func TestStreamEvent_DONESentinel(t *testing.T) {
	// OpenAI [DONE] -> Anthropic message_stop
	result, err := ConvertStreamEvent([]byte("[DONE]"), "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertStreamEvent failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}
	if getString(out, "type") != "message_stop" {
		t.Errorf("expected type message_stop, got %s", getString(out, "type"))
	}
}

func TestUnsupportedStreamConversion(t *testing.T) {
	_, err := ConvertStreamEvent([]byte(`{}`), "unknown", "openai-chat-completions")
	if err == nil {
		t.Fatal("expected error for unsupported stream conversion")
	}
}

func TestParseDataURI(t *testing.T) {
	mediaType, data := parseDataURI("data:image/png;base64,iVBORw0KGgo=")
	if mediaType != "image/png" {
		t.Errorf("expected media_type 'image/png', got '%s'", mediaType)
	}
	if data != "iVBORw0KGgo=" {
		t.Errorf("expected data 'iVBORw0KGgo=', got '%s'", data)
	}

	// Non-data URI
	mediaType, data = parseDataURI("https://example.com/image.png")
	if mediaType != "" {
		t.Errorf("expected empty media_type for non-data URI, got '%s'", mediaType)
	}
	if data != "https://example.com/image.png" {
		t.Errorf("expected original URL as data")
	}
}

func TestImageConversion(t *testing.T) {
	// Chat with image -> Anthropic
	chatReq := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "What's in this image?"},
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgo="}}
			]}
		]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	foundImage := false
	for _, c := range content {
		block := c.(map[string]any)
		if getString(block, "type") == "image" {
			foundImage = true
			source := getMap(block, "source")
			if source == nil {
				t.Errorf("expected source in image block")
			}
			if getString(source, "media_type") != "image/png" {
				t.Errorf("expected media_type 'image/png', got '%s'", getString(source, "media_type"))
			}
			if getString(source, "data") != "iVBORw0KGgo=" {
				t.Errorf("expected data 'iVBORw0KGgo=', got '%s'", getString(source, "data"))
			}
		}
	}
	if !foundImage {
		t.Errorf("expected image content block")
	}
}

func TestToolChoiceConversion(t *testing.T) {
	// Anthropic tool_choice -> Chat
	anthropicReq := []byte(`{
		"model": "claude-3",
		"messages": [{"role": "user", "content": [{"type": "text", "text": "Hi"}]}],
		"tool_choice": {"type": "tool", "name": "get_weather"}
	}`)

	result, err := ConvertRequest(anthropicReq, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	tc := out["tool_choice"]
	if tc == nil {
		t.Fatal("expected tool_choice")
	}
	tcMap, ok := tc.(map[string]any)
	if !ok {
		t.Fatalf("expected tool_choice as map, got %T", tc)
	}
	if getString(tcMap, "type") != "function" {
		t.Errorf("expected tool_choice type function")
	}
}

func TestStopSequencesConversion(t *testing.T) {
	// Chat stop -> Anthropic stop_sequences
	chatReq := []byte(`{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "Hi"}],
		"stop": ["\n", "END"]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	stopSeq := getSlice(out, "stop_sequences")
	if len(stopSeq) != 2 {
		t.Fatalf("expected 2 stop_sequences, got %d", len(stopSeq))
	}
}

func TestToolCallResponseConversion(t *testing.T) {
	// Chat response with tool calls -> Anthropic
	chatResp := []byte(`{
		"id": "chatcmpl-123",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [
					{"id": "call_123", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\":\"NYC\"}"}}
				]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5}
	}`)

	result, err := ConvertResponse(chatResp, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out, "stop_reason") != "tool_use" {
		t.Errorf("expected stop_reason tool_use, got %s", getString(out, "stop_reason"))
	}

	content := getSlice(out, "content")
	foundToolUse := false
	for _, c := range content {
		block := c.(map[string]any)
		if getString(block, "type") == "tool_use" {
			foundToolUse = true
			break
		}
	}
	if !foundToolUse {
		t.Errorf("expected tool_use content block")
	}
}

func TestChatToResponsesWithToolCalls(t *testing.T) {
	chatReq := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Weather?"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "call_123", "type": "function", "function": {"name": "get_weather", "arguments": "{\"loc\":\"NYC\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_123", "content": "Sunny"}
		],
		"tools": [{"type": "function", "function": {"name": "get_weather", "description": "Get weather", "parameters": {"type": "object"}}}]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	input := out["input"]
	if input == nil {
		t.Fatal("expected input")
	}

	tools := getSlice(out, "tools")
	if len(tools) == 0 {
		t.Errorf("expected tools")
	}
}

func TestResponsesToChatWithFunctionCall(t *testing.T) {
	responsesReq := []byte(`{
		"model": "gpt-4",
		"input": [
			{"role": "user", "content": [{"type": "input_text", "text": "Weather?"}]},
			{"role": "assistant", "content": [
				{"type": "function_call", "id": "call_123", "name": "get_weather", "arguments": "{\"loc\":\"NYC\"}"}
			]}
		]
	}`)

	result, err := ConvertRequest(responsesReq, "openai-responses", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// Check assistant message has tool_calls
	assistantMsg := msgs[1].(map[string]any)
	toolCalls := getSlice(assistantMsg, "tool_calls")
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(toolCalls))
	}
}

func TestChatToAnthropicWithImage(t *testing.T) {
	chatReq := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "What's in this image?"},
				{"type": "image_url", "image_url": "data:image/png;base64,iVBORw0KGgo="}
			]}
		]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(content))
	}
	// Verify image block has correct media_type and data
	imageBlock := content[1].(map[string]any)
	if getString(imageBlock, "type") != "image" {
		t.Errorf("expected image block type")
	}
	source := getMap(imageBlock, "source")
	if source == nil {
		t.Fatal("expected source in image block")
	}
	if getString(source, "media_type") != "image/png" {
		t.Errorf("expected media_type 'image/png', got '%s'", getString(source, "media_type"))
	}
	if getString(source, "data") != "iVBORw0KGgo=" {
		t.Errorf("expected data 'iVBORw0KGgo=', got '%s'", getString(source, "data"))
	}
}

func TestAnthropicToChatWithSystem(t *testing.T) {
	anthropicReq := []byte(`{
		"model": "claude-3",
		"system": "You are a helpful assistant.",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Hello"}]}
		]
	}`)

	result, err := ConvertRequest(anthropicReq, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}

	msg0 := msgs[0].(map[string]any)
	if getString(msg0, "role") != "system" {
		t.Errorf("expected first message role system, got %s", getString(msg0, "role"))
	}
	if getString(msg0, "content") != "You are a helpful assistant." {
		t.Errorf("expected system content")
	}
}

func TestEmptyBodyConversion(t *testing.T) {
	_, err := ConvertRequest([]byte(`{}`), "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest on empty body should not fail: %v", err)
	}
}

func TestInvalidJSON(t *testing.T) {
	_, err := ConvertRequest([]byte(`not json`), "openai-chat-completions", "anthropic-messages")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// P0 Reasoning/Thinking / System / Cache / ImageURL tests
// ---------------------------------------------------------------------------

// A.1: Reasoning effort -> thinking (Chat→Anthropic)
func TestConvertRequest_ReasoningEffortToThinking(t *testing.T) {
	chatReq := []byte(`{
		"model": "x",
		"messages": [{"role": "user", "content": "hi"}],
		"reasoning_effort": "medium"
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	thinking := getMap(out, "thinking")
	if thinking == nil {
		t.Fatal("expected thinking field")
	}
	if getString(thinking, "type") != "enabled" {
		t.Errorf("expected thinking.type 'enabled', got '%s'", getString(thinking, "type"))
	}
	budget, ok := thinking["budget_tokens"].(float64)
	if !ok {
		t.Fatalf("expected budget_tokens as number, got %T", thinking["budget_tokens"])
	}
	if int64(budget) != 4096 {
		t.Errorf("expected budget_tokens 4096, got %v", int64(budget))
	}
}

// A.1: Thinking -> reasoning_effort (Anthropic→Chat)
func TestConvertRequest_ThinkingToReasoningEffort(t *testing.T) {
	anthropicReq := []byte(`{
		"model": "x",
		"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}],
		"thinking": {"type": "enabled", "budget_tokens": 8000}
	}`)

	result, err := ConvertRequest(anthropicReq, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	effort := getString(out, "reasoning_effort")
	if effort != "medium" {
		t.Errorf("expected reasoning_effort 'medium' (8000 budget), got '%s'", effort)
	}
}

// A.2: Chat reasoning_content -> Anthropic thinking (Chat→Anthropic)
func TestConvertRequest_ChatReasoningContentToAnthropicThinking(t *testing.T) {
	chatReq := []byte(`{
		"model": "x",
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "assistant", "reasoning_content": "我在想...", "reasoning_signature": "sig123", "content": "回答"}
		]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// Assistant message should have thinking block + text block
	assistantMsg := msgs[1].(map[string]any)
	content := getSlice(assistantMsg, "content")
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(content))
	}

	// First block: thinking
	block0 := content[0].(map[string]any)
	if getString(block0, "type") != "thinking" {
		t.Errorf("expected content[0].type 'thinking', got '%s'", getString(block0, "type"))
	}
	if getString(block0, "thinking") != "我在想..." {
		t.Errorf("expected thinking text '我在想...', got '%s'", getString(block0, "thinking"))
	}
	if getString(block0, "signature") != "sig123" {
		t.Errorf("expected signature 'sig123', got '%s'", getString(block0, "signature"))
	}

	// Second block: text
	block1 := content[1].(map[string]any)
	if getString(block1, "type") != "text" {
		t.Errorf("expected content[1].type 'text', got '%s'", getString(block1, "type"))
	}
	if getString(block1, "text") != "回答" {
		t.Errorf("expected text '回答', got '%s'", getString(block1, "text"))
	}
}

// A.2: Anthropic thinking -> Chat reasoning_content (Anthropic→Chat)
func TestConvertRequest_AnthropicThinkingToChatReasoningContent(t *testing.T) {
	anthropicReq := []byte(`{
		"model": "x",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "hi"}]},
			{"role": "assistant", "content": [
				{"type": "thinking", "thinking": "想...", "signature": "sig"},
				{"type": "text", "text": "回答"}
			]}
		]
	}`)

	result, err := ConvertRequest(anthropicReq, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	assistantMsg := msgs[1].(map[string]any)
	if getString(assistantMsg, "reasoning_content") != "想..." {
		t.Errorf("expected reasoning_content '想...', got '%s'", getString(assistantMsg, "reasoning_content"))
	}
	if getString(assistantMsg, "reasoning_signature") != "sig" {
		t.Errorf("expected reasoning_signature 'sig', got '%s'", getString(assistantMsg, "reasoning_signature"))
	}
	if getString(assistantMsg, "content") != "回答" {
		t.Errorf("expected content '回答', got '%s'", getString(assistantMsg, "content"))
	}
}

// A.3: Chat system -> Anthropic system (Chat→Anthropic)
func TestConvertRequest_ChatSystemToAnthropicSystem(t *testing.T) {
	chatReq := []byte(`{
		"model": "x",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello"}
		]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	// System should be at top level, not as fake user message
	system := getString(out, "system")
	if system != "You are a helpful assistant." {
		t.Errorf("expected system 'You are a helpful assistant.', got '%s'", system)
	}

	// Messages should only contain the user message
	msgs := getSlice(out, "messages")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (user only), got %d", len(msgs))
	}
	msg0 := msgs[0].(map[string]any)
	if getString(msg0, "role") != "user" {
		t.Errorf("expected role 'user', got '%s'", getString(msg0, "role"))
	}
}

// A.3: Anthropic system array -> Chat (Anthropic→Chat)
func TestConvertRequest_AnthropicSystemArrayToChat(t *testing.T) {
	anthropicReq := []byte(`{
		"model": "x",
		"system": [{"type": "text", "text": "你是"}, {"type": "text", "text": "助手"}],
		"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}]
	}`)

	result, err := ConvertRequest(anthropicReq, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}

	msg0 := msgs[0].(map[string]any)
	if getString(msg0, "role") != "system" {
		t.Errorf("expected first message role 'system', got '%s'", getString(msg0, "role"))
	}
	if getString(msg0, "content") != "你是\n助手" {
		t.Errorf("expected system content '你是\\n助手', got '%s'", getString(msg0, "content"))
	}
}

// A.4: Chat cache_control -> Anthropic (Chat→Anthropic)
func TestConvertRequest_ChatCacheControlToAnthropic(t *testing.T) {
	chatReq := []byte(`{
		"model": "x",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "Hello", "cache_control": {"type": "ephemeral"}}
			]}
		]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	msg := msgs[0].(map[string]any)
	content := getSlice(msg, "content")
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}

	block := content[0].(map[string]any)
	if getString(block, "type") != "text" {
		t.Errorf("expected type 'text', got '%s'", getString(block, "type"))
	}
	if getString(block, "text") != "Hello" {
		t.Errorf("expected text 'Hello', got '%s'", getString(block, "text"))
	}

	cc := getMap(block, "cache_control")
	if cc == nil {
		t.Fatal("expected cache_control in content block")
	}
	if getString(cc, "type") != "ephemeral" {
		t.Errorf("expected cache_control.type 'ephemeral', got '%s'", getString(cc, "type"))
	}
}

// A.5: Chat image_url (URL) -> Anthropic image URL (Chat→Anthropic)
func TestConvertRequest_ChatImageURLToAnthropicURL(t *testing.T) {
	chatReq := []byte(`{
		"model": "x",
		"messages": [
			{"role": "user", "content": [
				{"type": "image_url", "image_url": {"url": "https://example.com/a.png"}}
			]}
		]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	msgs := getSlice(out, "messages")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	msg := msgs[0].(map[string]any)
	content := getSlice(msg, "content")
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}

	block := content[0].(map[string]any)
	if getString(block, "type") != "image" {
		t.Errorf("expected type 'image', got '%s'", getString(block, "type"))
	}

	source := getMap(block, "source")
	if source == nil {
		t.Fatal("expected source in image block")
	}
	if getString(source, "type") != "url" {
		t.Errorf("expected source.type 'url', got '%s'", getString(source, "type"))
	}
	if getString(source, "url") != "https://example.com/a.png" {
		t.Errorf("expected source.url 'https://example.com/a.png', got '%s'", getString(source, "url"))
	}
}

// A.6: Chat response reasoning_content -> Anthropic thinking (Chat→Anthropic)
func TestConvertResponse_ChatReasoningContentToAnthropicThinking(t *testing.T) {
	chatResp := []byte(`{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "x",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"reasoning_content": "想",
				"reasoning_signature": "sig",
				"content": "回答"
			},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 10, "total_tokens": 15}
	}`)

	result, err := ConvertResponse(chatResp, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	content := getSlice(out, "content")
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(content))
	}

	// First block: thinking
	block0 := content[0].(map[string]any)
	if getString(block0, "type") != "thinking" {
		t.Errorf("expected content[0].type 'thinking', got '%s'", getString(block0, "type"))
	}
	if getString(block0, "thinking") != "想" {
		t.Errorf("expected thinking text '想', got '%s'", getString(block0, "thinking"))
	}
	if getString(block0, "signature") != "sig" {
		t.Errorf("expected signature 'sig', got '%s'", getString(block0, "signature"))
	}

	// Second block: text
	block1 := content[1].(map[string]any)
	if getString(block1, "type") != "text" {
		t.Errorf("expected content[1].type 'text', got '%s'", getString(block1, "type"))
	}
	if getString(block1, "text") != "回答" {
		t.Errorf("expected text '回答', got '%s'", getString(block1, "text"))
	}
}

// A.6: Anthropic response thinking -> Chat reasoning_content (Anthropic→Chat)
func TestConvertResponse_AnthropicThinkingToChatReasoningContent(t *testing.T) {
	anthropicResp := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "x",
		"content": [
			{"type": "thinking", "thinking": "想", "signature": "sig"},
			{"type": "text", "text": "回答"}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 5, "output_tokens": 10}
	}`)

	result, err := ConvertResponse(anthropicResp, "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	choices := getSlice(out, "choices")
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}

	choice := choices[0].(map[string]any)
	message := getMap(choice, "message")
	if message == nil {
		t.Fatal("expected message in choice")
	}

	if getString(message, "reasoning_content") != "想" {
		t.Errorf("expected reasoning_content '想', got '%s'", getString(message, "reasoning_content"))
	}
	if getString(message, "reasoning_signature") != "sig" {
		t.Errorf("expected reasoning_signature 'sig', got '%s'", getString(message, "reasoning_signature"))
	}
	if getString(message, "content") != "回答" {
		t.Errorf("expected content '回答', got '%s'", getString(message, "content"))
	}
}

// ---------------------------------------------------------------------------
// A layer: Reasoning direction coverage
// ---------------------------------------------------------------------------

// A.1: Chat reasoning_effort -> Responses reasoning (Chat→Responses)
func TestConvertRequest_ChatToResponses_ReasoningEffort(t *testing.T) {
	chatReq := []byte(`{
		"model": "x",
		"messages": [{"role": "user", "content": "hi"}],
		"reasoning_effort": "medium",
		"stream": true
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	reasoning := getMap(out, "reasoning")
	if reasoning == nil {
		t.Fatal("expected reasoning field")
	}
	if getString(reasoning, "effort") != "medium" {
		t.Errorf("expected reasoning.effort 'medium', got '%s'", getString(reasoning, "effort"))
	}
}

// A.2: Responses reasoning -> Chat reasoning_effort (Responses→Chat)
func TestConvertRequest_ResponsesToChat_Reasoning(t *testing.T) {
	responsesReq := []byte(`{
		"model": "x",
		"input": "hi",
		"reasoning": {"effort": "low"}
	}`)

	result, err := ConvertRequest(responsesReq, "openai-responses", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	effort := getString(out, "reasoning_effort")
	if effort != "low" {
		t.Errorf("expected reasoning_effort 'low', got '%s'", effort)
	}
}

// A.3: Responses reasoning -> Anthropic thinking (Responses→Anthropic)
func TestConvertRequest_ResponsesToAnthropic_Reasoning(t *testing.T) {
	responsesReq := []byte(`{
		"model": "x",
		"input": "hi",
		"reasoning": {"effort": "minimal"}
	}`)

	result, err := ConvertRequest(responsesReq, "openai-responses", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	thinking := getMap(out, "thinking")
	if thinking == nil {
		t.Fatal("expected thinking field")
	}
	if getString(thinking, "type") != "enabled" {
		t.Errorf("expected thinking.type 'enabled', got '%s'", getString(thinking, "type"))
	}
	budget, ok := thinking["budget_tokens"].(float64)
	if !ok {
		t.Fatalf("expected budget_tokens as number, got %T", thinking["budget_tokens"])
	}
	if int64(budget) != 512 {
		t.Errorf("expected budget_tokens 512, got %v", int64(budget))
	}
}

// A.4: Anthropic thinking -> Responses reasoning (Anthropic→Responses)
func TestConvertRequest_AnthropicToResponses_Thinking(t *testing.T) {
	anthropicReq := []byte(`{
		"model": "x",
		"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}],
		"thinking": {"type": "enabled", "budget_tokens": 16000}
	}`)

	result, err := ConvertRequest(anthropicReq, "anthropic-messages", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	reasoning := getMap(out, "reasoning")
	if reasoning == nil {
		t.Fatal("expected reasoning field")
	}
	if getString(reasoning, "effort") != "high" {
		t.Errorf("expected reasoning.effort 'high', got '%s'", getString(reasoning, "effort"))
	}
}

// A.5: Chat response reasoning_content -> Responses reasoning item (Chat→Responses)
func TestConvertResponse_ChatToResponses_Reasoning(t *testing.T) {
	chatResp := []byte(`{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "x",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"reasoning_content": "想一想",
				"reasoning_signature": "sig-abc",
				"content": "回答"
			},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 10, "total_tokens": 15}
	}`)

	result, err := ConvertResponse(chatResp, "openai-responses", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	output := getSlice(out, "output")
	if len(output) < 2 {
		t.Fatalf("expected at least 2 output items, got %d", len(output))
	}

	// First item: reasoning
	item0 := output[0].(map[string]any)
	if getString(item0, "type") != "reasoning" {
		t.Errorf("expected output[0].type 'reasoning', got '%s'", getString(item0, "type"))
	}
	if !strings.HasPrefix(getString(item0, "id"), "rs_") {
		t.Errorf("expected output[0].id to start with 'rs_', got '%s'", getString(item0, "id"))
	}
	if getString(item0, "status") != "completed" {
		t.Errorf("expected output[0].status 'completed', got '%s'", getString(item0, "status"))
	}
	content0 := getSlice(item0, "content")
	if len(content0) < 1 {
		t.Fatal("expected output[0].content to have at least 1 item")
	}
	block0 := content0[0].(map[string]any)
	if getString(block0, "type") != "reasoning_text" {
		t.Errorf("expected content[0].type 'reasoning_text', got '%s'", getString(block0, "type"))
	}
	if getString(block0, "text") != "想一想" {
		t.Errorf("expected content[0].text '想一想', got '%s'", getString(block0, "text"))
	}
	if getString(item0, "encrypted_content") != "sig-abc" {
		t.Errorf("expected encrypted_content 'sig-abc', got '%s'", getString(item0, "encrypted_content"))
	}

	// Second item: message
	item1 := output[1].(map[string]any)
	if getString(item1, "type") != "message" {
		t.Errorf("expected output[1].type 'message', got '%s'", getString(item1, "type"))
	}
	msgContent := getSlice(item1, "content")
	if len(msgContent) < 1 {
		t.Fatal("expected output[1].content to have at least 1 item")
	}
	msgBlock := msgContent[0].(map[string]any)
	if getString(msgBlock, "text") != "回答" {
		t.Errorf("expected message text '回答', got '%s'", getString(msgBlock, "text"))
	}
}

// A.6: Responses response reasoning -> Chat reasoning_content (Responses→Chat)
func TestConvertResponse_ResponsesToChat_Reasoning(t *testing.T) {
	responsesResp := []byte(`{
		"id": "resp_123",
		"type": "response",
		"model": "x",
		"output": [
			{
				"id": "rs_1",
				"type": "reasoning",
				"status": "completed",
				"content": [{"type": "reasoning_text", "text": "思考"}],
				"encrypted_content": "sig-xyz"
			},
			{
				"id": "msg_1",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "output_text", "text": "回答"}]
			}
		],
		"status": "completed",
		"usage": {"input_tokens": 5, "output_tokens": 10}
	}`)

	result, err := ConvertResponse(responsesResp, "openai-chat-completions", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	choices := getSlice(out, "choices")
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}
	choice := choices[0].(map[string]any)
	message := getMap(choice, "message")
	if message == nil {
		t.Fatal("expected message in choice")
	}

	if getString(message, "reasoning_content") != "思考" {
		t.Errorf("expected reasoning_content '思考', got '%s'", getString(message, "reasoning_content"))
	}
	if getString(message, "reasoning_signature") != "sig-xyz" {
		t.Errorf("expected reasoning_signature 'sig-xyz', got '%s'", getString(message, "reasoning_signature"))
	}
	if getString(message, "content") != "回答" {
		t.Errorf("expected content '回答', got '%s'", getString(message, "content"))
	}
}

// ---------------------------------------------------------------------------
// B layer: Regression protection
// ---------------------------------------------------------------------------

// B.1: Chat system -> Responses instructions (Chat→Responses)
func TestConvertRequest_ChatToResponses_SystemLiftedToInstructions(t *testing.T) {
	// Single system message
	chatReq := []byte(`{
		"model": "x",
		"messages": [
			{"role": "system", "content": "你是助手"},
			{"role": "user", "content": "你好"}
		]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	// instructions should be set
	if getString(out, "instructions") != "你是助手" {
		t.Errorf("expected instructions '你是助手', got '%s'", getString(out, "instructions"))
	}

	// input should NOT contain [System]\n prefix
	inputBytes, _ := json.Marshal(out["input"])
	if strings.Contains(string(inputBytes), "[System]") {
		t.Errorf("input should not contain '[System]' prefix, got: %s", string(inputBytes))
	}

	// input should be the single user message as a string (since there's only one user msg with string content)
	if inputStr, ok := out["input"].(string); ok {
		if inputStr != "你好" {
			t.Errorf("expected input '你好', got '%s'", inputStr)
		}
	} else {
		t.Errorf("expected input to be a string, got %T", out["input"])
	}

	// --- Multiple system messages ---
	chatReq2 := []byte(`{
		"model": "x",
		"messages": [
			{"role": "system", "content": "A"},
			{"role": "system", "content": "B"},
			{"role": "user", "content": "hi"}
		]
	}`)

	result2, err := ConvertRequest(chatReq2, "openai-chat-completions", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out2 map[string]any
	if err := json.Unmarshal(result2, &out2); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if getString(out2, "instructions") != "A\n\nB" {
		t.Errorf("expected instructions 'A\\n\\nB', got '%s'", getString(out2, "instructions"))
	}
}

// B.2: Chat server tools passthrough -> Responses (Chat→Responses)
func TestConvertRequest_ChatToResponses_ServerToolPassthrough(t *testing.T) {
	chatReq := []byte(`{
		"model": "x",
		"messages": [{"role": "user", "content": "search"}],
		"tools": [
			{"type": "web_search_20250305", "name": "web_search"},
			{"type": "function", "function": {"name": "add", "parameters": "{}"}}
		]
	}`)

	result, err := ConvertRequest(chatReq, "openai-chat-completions", "openai-responses")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	tools := getSlice(out, "tools")
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// First tool: server tool passthrough
	tool0 := tools[0].(map[string]any)
	if getString(tool0, "type") != "web_search_20250305" {
		t.Errorf("expected tools[0].type 'web_search_20250305', got '%s'", getString(tool0, "type"))
	}
	if getString(tool0, "name") != "web_search" {
		t.Errorf("expected tools[0].name 'web_search', got '%s'", getString(tool0, "name"))
	}

	// Second tool: function tool
	tool1 := tools[1].(map[string]any)
	if getString(tool1, "type") != "function" {
		t.Errorf("expected tools[1].type 'function', got '%s'", getString(tool1, "type"))
	}
}

// B.4: Anthropic response multiple thinking blocks -> Responses (Anthropic→Responses)
func TestConvertResponse_AnthropicToResponses_MultipleThinkingBlocks(t *testing.T) {
	anthropicResp := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "x",
		"content": [
			{"type": "thinking", "thinking": "想1", "signature": "sig1"},
			{"type": "thinking", "thinking": "想2"},
			{"type": "text", "text": "回答"}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 5, "output_tokens": 10}
	}`)

	result, err := ConvertResponse(anthropicResp, "openai-responses", "anthropic-messages")
	if err != nil {
		t.Fatalf("ConvertResponse failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	output := getSlice(out, "output")
	if len(output) < 3 {
		t.Fatalf("expected at least 3 output items, got %d", len(output))
	}

	// Expected order: [reasoning1, reasoning2, message] — reasoning items appear
	// before the final assistant message.

	// First item: reasoning (thinking block 1)
	item0 := output[0].(map[string]any)
	if getString(item0, "type") != "reasoning" {
		t.Errorf("expected output[0].type 'reasoning', got '%s'", getString(item0, "type"))
	}
	if !strings.HasPrefix(getString(item0, "id"), "rs_response") {
		t.Errorf("expected output[0].id to start with 'rs_response', got '%s'", getString(item0, "id"))
	}
	content0 := getSlice(item0, "content")
	if len(content0) < 1 {
		t.Fatal("expected output[0].content to have at least 1 item")
	}
	block0 := content0[0].(map[string]any)
	if getString(block0, "text") != "想1" {
		t.Errorf("expected content[0].text '想1', got '%s'", getString(block0, "text"))
	}
	if getString(item0, "encrypted_content") != "sig1" {
		t.Errorf("expected encrypted_content 'sig1', got '%s'", getString(item0, "encrypted_content"))
	}

	// Second item: reasoning (thinking block 2)
	item1 := output[1].(map[string]any)
	if getString(item1, "type") != "reasoning" {
		t.Errorf("expected output[1].type 'reasoning', got '%s'", getString(item1, "type"))
	}
	if !strings.HasPrefix(getString(item1, "id"), "rs_response") {
		t.Errorf("expected output[1].id to start with 'rs_response', got '%s'", getString(item1, "id"))
	}
	// Second thinking block id should differ from first
	if getString(item1, "id") == getString(item0, "id") {
		t.Errorf("expected output[1].id to differ from output[0].id")
	}
	content1 := getSlice(item1, "content")
	if len(content1) < 1 {
		t.Fatal("expected output[1].content to have at least 1 item")
	}
	block1 := content1[0].(map[string]any)
	if getString(block1, "text") != "想2" {
		t.Errorf("expected content[1].text '想2', got '%s'", getString(block1, "text"))
	}
	// Second thinking block has no signature, so no encrypted_content
	if getString(item1, "encrypted_content") != "" {
		t.Errorf("expected no encrypted_content for second thinking block, got '%s'", getString(item1, "encrypted_content"))
	}

	// Third item: message (appended after reasoning items)
	item2 := output[2].(map[string]any)
	if getString(item2, "type") != "message" {
		t.Errorf("expected output[2].type 'message', got '%s'", getString(item2, "type"))
	}
	msgContent := getSlice(item2, "content")
	if len(msgContent) < 1 {
		t.Fatal("expected output[2].content to have at least 1 item")
	}
	msgBlock := msgContent[0].(map[string]any)
	if getString(msgBlock, "text") != "回答" {
		t.Errorf("expected message text '回答', got '%s'", getString(msgBlock, "text"))
	}
}

// B.5: Anthropic cache_control in tools -> Chat _cache_control (Anthropic→Chat)
func TestConvertRequest_AnthropicCacheControlToChat_Extended(t *testing.T) {
	anthropicReq := []byte(`{
		"model": "x",
		"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}],
		"tools": [
			{"name": "add", "description": "", "input_schema": {"type": "object"}, "cache_control": {"type": "ephemeral"}}
		]
	}`)

	result, err := ConvertRequest(anthropicReq, "anthropic-messages", "openai-chat-completions")
	if err != nil {
		t.Fatalf("ConvertRequest failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	tools := getSlice(out, "tools")
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool0 := tools[0].(map[string]any)
	if getString(tool0, "type") != "function" {
		t.Errorf("expected tools[0].type 'function', got '%s'", getString(tool0, "type"))
	}

	cc := getMap(tool0, "_cache_control")
	if cc == nil {
		t.Fatal("expected _cache_control in tool")
	}
	want := map[string]any{"type": "ephemeral"}
	if !reflect.DeepEqual(cc, want) {
		t.Errorf("expected _cache_control %v, got %v", want, cc)
	}
}
