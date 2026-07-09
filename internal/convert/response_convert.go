package convert

import (
	"fmt"
	"strings"
)

// --- Chat Response -> Anthropic Response ---

func convertChatResponseToAnthropic(resp map[string]any) map[string]any {
	out := make(map[string]any)
	out["type"] = "message"
	out["role"] = "assistant"

	// Copy id
	if id := getString(resp, "id"); id != "" {
		out["id"] = id
	}

	// Model
	if model := getString(resp, "model"); model != "" {
		out["model"] = model
	}

	// Build content array
	var content []any

	// Text from choices[0].message.content
	if choices := getSlice(resp, "choices"); len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			message := getMap(choice, "message")
			if message != nil {
				// Reasoning content -> thinking block
				reasoningContent := getString(message, "reasoning_content")
				reasoningSignature := getString(message, "reasoning_signature")
				if reasoningContent != "" {
					thinkingBlock := map[string]any{
						"type":     "thinking",
						"thinking": reasoningContent,
					}
					if reasoningSignature != "" {
						thinkingBlock["signature"] = reasoningSignature
					}
					content = append(content, thinkingBlock)
				}

				// Text content
				if text := getString(message, "content"); text != "" {
					content = append(content, map[string]any{
						"type": "text",
						"text": text,
					})
				}

				// Tool calls
				if toolCalls := getSlice(message, "tool_calls"); len(toolCalls) > 0 {
					for _, tc := range toolCalls {
						tcMap, ok := tc.(map[string]any)
						if !ok {
							continue
						}
						function := getMap(tcMap, "function")
						if function == nil {
							continue
						}
						content = append(content, map[string]any{
							"type":  "tool_use",
							"id":    getString(tcMap, "id"),
							"name":  getString(function, "name"),
							"input": parseJSONString(getString(function, "arguments")),
						})
					}
				}
			}

			// Finish reason
			if fr := getString(choice, "finish_reason"); fr != "" {
				switch fr {
				case "stop":
					out["stop_reason"] = "end_turn"
				case "length":
					out["stop_reason"] = "max_tokens"
				case "tool_calls":
					out["stop_reason"] = "tool_use"
				default:
					out["stop_reason"] = fr
				}
			}
		}
	}

	if len(content) > 0 {
		out["content"] = content
	}

	// Usage
	if usage := getMap(resp, "usage"); usage != nil {
		anthropicUsage := make(map[string]any)
		if v := getFloat64(usage, "prompt_tokens"); v > 0 {
			anthropicUsage["input_tokens"] = int64(v)
		}
		if v := getFloat64(usage, "completion_tokens"); v > 0 {
			anthropicUsage["output_tokens"] = int64(v)
		}
		if details := getMap(usage, "prompt_tokens_details"); details != nil {
			if v := getFloat64(details, "cached_tokens"); v > 0 {
				anthropicUsage["cache_read_input_tokens"] = int64(v)
			}
		}
		if len(anthropicUsage) > 0 {
			out["usage"] = anthropicUsage
		}
	}

	return out
}

// --- Anthropic Response -> Chat Response ---

func convertAnthropicResponseToChat(resp map[string]any) map[string]any {
	out := make(map[string]any)

	// id
	if id := getString(resp, "id"); id != "" {
		out["id"] = id
	}

	// Model
	if model := getString(resp, "model"); model != "" {
		out["model"] = model
	}

	// Object
	out["object"] = "chat.completion"

	// Build choices
	choice := make(map[string]any)
	choice["index"] = 0

	message := make(map[string]any)
	message["role"] = "assistant"

	// Content
	content := resp["content"]
	var text string
	var toolCalls []any
	var reasoningParts []string
	var reasoningSignature string

	switch c := content.(type) {
	case string:
		text = c
	case []any:
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "text":
				text += getString(p, "text")
			case "thinking":
				reasoningParts = append(reasoningParts, getString(p, "thinking"))
				if sig := getString(p, "signature"); sig != "" {
					reasoningSignature = sig
				}
			case "tool_use":
				toolCalls = append(toolCalls, map[string]any{
					"id":   getString(p, "id"),
					"type": "function",
					"function": map[string]any{
						"name":      getString(p, "name"),
						"arguments": mapToJSONString(getMap(p, "input")),
					},
				})
			}
		}
	}

	message["content"] = text
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	if len(reasoningParts) > 0 {
		message["reasoning_content"] = strings.Join(reasoningParts, "\n")
		if reasoningSignature != "" {
			message["reasoning_signature"] = reasoningSignature
		}
	}
	choice["message"] = message

	// Finish reason
	switch getString(resp, "stop_reason") {
	case "end_turn":
		choice["finish_reason"] = "stop"
	case "max_tokens":
		choice["finish_reason"] = "length"
	case "tool_use":
		choice["finish_reason"] = "tool_calls"
	case "stop_sequence":
		choice["finish_reason"] = "stop"
	default:
		choice["finish_reason"] = getString(resp, "stop_reason")
	}

	out["choices"] = []any{choice}

	// Usage
	if usage := getMap(resp, "usage"); usage != nil {
		chatUsage := make(map[string]any)
		if v := getFloat64(usage, "input_tokens"); v > 0 {
			chatUsage["prompt_tokens"] = int64(v)
		}
		if v := getFloat64(usage, "output_tokens"); v > 0 {
			chatUsage["completion_tokens"] = int64(v)
		}
		chatUsage["total_tokens"] = int64(getFloat64(usage, "input_tokens") + getFloat64(usage, "output_tokens"))
		if v := getFloat64(usage, "cache_read_input_tokens"); v > 0 {
			chatUsage["prompt_tokens_details"] = map[string]any{
				"cached_tokens": int64(v),
			}
		}
		out["usage"] = chatUsage
	}

	return out
}

// --- Chat Response -> Responses Response ---

func convertChatResponseToResponses(resp map[string]any) map[string]any {
	out := make(map[string]any)

	out["type"] = "response"
	out["object"] = "response"

	// id
	if id := getString(resp, "id"); id != "" {
		out["id"] = id
	}

	// Model
	if model := getString(resp, "model"); model != "" {
		out["model"] = model
	}

	// Build output from choices
	if choices := getSlice(resp, "choices"); len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			message := getMap(choice, "message")
			if message != nil {
				var output []any

				// Reasoning content -> reasoning output item
				reasoningContent := getString(message, "reasoning_content")
				reasoningSignature := getString(message, "reasoning_signature")
				if reasoningContent != "" {
					reasoningItem := map[string]any{
						"id":     "rs_response",
						"type":   "reasoning",
						"status": "completed",
						"content": []any{
							map[string]any{
								"type": "reasoning_text",
								"text": reasoningContent,
							},
						},
					}
					if reasoningSignature != "" {
						reasoningItem["encrypted_content"] = reasoningSignature
					}
					output = append(output, reasoningItem)
				}

				// Text content
				if text := getString(message, "content"); text != "" {
					output = append(output, map[string]any{
						"type": "message",
						"role": "assistant",
						"content": []any{
							map[string]any{
								"type": "output_text",
								"text": text,
							},
						},
					})
				}

				// Tool calls
				if toolCalls := getSlice(message, "tool_calls"); len(toolCalls) > 0 {
					for _, tc := range toolCalls {
						tcMap, ok := tc.(map[string]any)
						if !ok {
							continue
						}
						function := getMap(tcMap, "function")
						if function == nil {
							continue
						}
						output = append(output, map[string]any{
							"type":      "function_call",
							"id":        getString(tcMap, "id"),
							"name":      getString(function, "name"),
							"arguments": getString(function, "arguments"),
							"status":    "completed",
						})
					}
				}

				if len(output) > 0 {
					out["output"] = output
				}
			}

			// Finish reason
			if fr := getString(choice, "finish_reason"); fr != "" {
				switch fr {
				case "stop":
					out["status"] = "completed"
				case "length":
					out["status"] = "incomplete"
				case "tool_calls":
					out["status"] = "in_progress"
				default:
					out["status"] = "completed"
				}
			}
		}
	}

	// Usage
	if usage := getMap(resp, "usage"); usage != nil {
		responsesUsage := make(map[string]any)
		if v := getFloat64(usage, "prompt_tokens"); v > 0 {
			responsesUsage["input_tokens"] = int64(v)
		}
		if v := getFloat64(usage, "completion_tokens"); v > 0 {
			responsesUsage["output_tokens"] = int64(v)
		}
		if details := getMap(usage, "prompt_tokens_details"); details != nil {
			if v := getFloat64(details, "cached_tokens"); v > 0 {
				responsesUsage["input_tokens_details"] = map[string]any{
					"cached_tokens": int64(v),
				}
			}
		}
		if len(responsesUsage) > 0 {
			out["usage"] = responsesUsage
		}
	}

	return out
}

// --- Anthropic Response -> Responses Response ---

func convertAnthropicResponseToResponses(resp map[string]any) map[string]any {
	out := make(map[string]any)

	out["type"] = "response"
	out["object"] = "response"

	if id := getString(resp, "id"); id != "" {
		out["id"] = id
	}
	if model := getString(resp, "model"); model != "" {
		out["model"] = model
	}

	// Build output from content
	content := resp["content"]
	var output []any

	switch c := content.(type) {
	case string:
		if c != "" {
			output = append(output, map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": c,
					},
				},
			})
		}
	case []any:
		var text string
		var thinkingIdx int
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "text":
				text += getString(p, "text")
			case "thinking":
				thinkingIdx++
				reasoningItem := map[string]any{
					"id":     fmt.Sprintf("rs_response_%d", thinkingIdx),
					"type":   "reasoning",
					"status": "completed",
					"content": []any{
						map[string]any{"type": "reasoning_text", "text": getString(p, "thinking")},
					},
				}
				if sig := getString(p, "signature"); sig != "" {
					reasoningItem["encrypted_content"] = sig
				}
				output = append(output, reasoningItem)
			case "tool_use":
				output = append(output, map[string]any{
					"type":      "function_call",
					"id":        getString(p, "id"),
					"name":      getString(p, "name"),
					"arguments": mapToJSONString(getMap(p, "input")),
					"status":    "completed",
				})
			}
		}
		// Append the aggregated text message at the end so that reasoning/tool
		// items appear before the final assistant message (matches Responses
		// ordering convention: reasoning -> message).
		if text != "" {
			output = append(output, map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": text,
					},
				},
			})
		}
	}

	if len(output) > 0 {
		out["output"] = output
	}

	// Stop reason -> status
	switch getString(resp, "stop_reason") {
	case "end_turn":
		out["status"] = "completed"
	case "max_tokens":
		out["status"] = "incomplete"
	case "tool_use":
		out["status"] = "in_progress"
	default:
		out["status"] = "completed"
	}

	// Usage
	if usage := getMap(resp, "usage"); usage != nil {
		responsesUsage := make(map[string]any)
		if v := getFloat64(usage, "input_tokens"); v > 0 {
			responsesUsage["input_tokens"] = int64(v)
		}
		if v := getFloat64(usage, "output_tokens"); v > 0 {
			responsesUsage["output_tokens"] = int64(v)
		}
		if v := getFloat64(usage, "cache_read_input_tokens"); v > 0 {
			responsesUsage["input_tokens_details"] = map[string]any{
				"cached_tokens": int64(v),
			}
		}
		if len(responsesUsage) > 0 {
			out["usage"] = responsesUsage
		}
	}

	return out
}

// --- Responses Response -> Chat Response ---

func convertResponsesResponseToChat(resp map[string]any) map[string]any {
	out := make(map[string]any)

	if id := getString(resp, "id"); id != "" {
		out["id"] = id
	}
	if model := getString(resp, "model"); model != "" {
		out["model"] = model
	}
	out["object"] = "chat.completion"

	choice := make(map[string]any)
	choice["index"] = 0
	message := make(map[string]any)
	message["role"] = "assistant"

	var text string
	var toolCalls []any
	var reasoningParts []string
	var reasoningSignature string

	// Parse output array
	if output := getSlice(resp, "output"); len(output) > 0 {
		for _, item := range output {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch getString(itemMap, "type") {
			case "message":
				if content := getSlice(itemMap, "content"); len(content) > 0 {
					for _, c := range content {
						contentMap, ok := c.(map[string]any)
						if !ok {
							continue
						}
						if getString(contentMap, "type") == "output_text" {
							text += getString(contentMap, "text")
						}
					}
				}
			case "function_call":
				toolCalls = append(toolCalls, map[string]any{
					"id":   getString(itemMap, "id"),
					"type": "function",
					"function": map[string]any{
						"name":      getString(itemMap, "name"),
						"arguments": getString(itemMap, "arguments"),
					},
				})
			case "reasoning":
				if content := getSlice(itemMap, "content"); len(content) > 0 {
					if first, ok := content[0].(map[string]any); ok {
						if getString(first, "type") == "reasoning_text" {
							reasoningParts = append(reasoningParts, getString(first, "text"))
						}
					}
				}
				if sig := getString(itemMap, "encrypted_content"); sig != "" {
					reasoningSignature = sig
				}
			}
		}
	}

	message["content"] = text
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	if len(reasoningParts) > 0 {
		message["reasoning_content"] = strings.Join(reasoningParts, "\n")
		if reasoningSignature != "" {
			message["reasoning_signature"] = reasoningSignature
		}
	}
	choice["message"] = message

	// Status -> finish_reason
	switch getString(resp, "status") {
	case "completed":
		choice["finish_reason"] = "stop"
	case "incomplete":
		choice["finish_reason"] = "length"
	case "in_progress":
		choice["finish_reason"] = "tool_calls"
	default:
		choice["finish_reason"] = "stop"
	}

	out["choices"] = []any{choice}

	// Usage
	if usage := getMap(resp, "usage"); usage != nil {
		chatUsage := make(map[string]any)
		if v := getFloat64(usage, "input_tokens"); v > 0 {
			chatUsage["prompt_tokens"] = int64(v)
		}
		if v := getFloat64(usage, "output_tokens"); v > 0 {
			chatUsage["completion_tokens"] = int64(v)
		}
		chatUsage["total_tokens"] = int64(getFloat64(usage, "input_tokens") + getFloat64(usage, "output_tokens"))
		if details := getMap(usage, "input_tokens_details"); details != nil {
			if v := getFloat64(details, "cached_tokens"); v > 0 {
				chatUsage["prompt_tokens_details"] = map[string]any{
					"cached_tokens": int64(v),
				}
			}
		}
		out["usage"] = chatUsage
	}

	return out
}

// --- Responses Response -> Anthropic Response ---

func convertResponsesResponseToAnthropic(resp map[string]any) map[string]any {
	out := make(map[string]any)
	out["type"] = "message"
	out["role"] = "assistant"

	if id := getString(resp, "id"); id != "" {
		out["id"] = id
	}
	if model := getString(resp, "model"); model != "" {
		out["model"] = model
	}

	var content []any

	if output := getSlice(resp, "output"); len(output) > 0 {
		for _, item := range output {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch getString(itemMap, "type") {
			case "message":
				if msgContent := getSlice(itemMap, "content"); len(msgContent) > 0 {
					for _, c := range msgContent {
						contentMap, ok := c.(map[string]any)
						if !ok {
							continue
						}
						if getString(contentMap, "type") == "output_text" {
							content = append(content, map[string]any{
								"type": "text",
								"text": getString(contentMap, "text"),
							})
						}
					}
				}
			case "function_call":
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    getString(itemMap, "id"),
					"name":  getString(itemMap, "name"),
					"input": parseJSONString(getString(itemMap, "arguments")),
				})
			case "reasoning":
				thinkingText := ""
				if c := getSlice(itemMap, "content"); len(c) > 0 {
					if first, ok := c[0].(map[string]any); ok {
						if getString(first, "type") == "reasoning_text" {
							thinkingText = getString(first, "text")
						}
					}
				}
				if thinkingText != "" {
					thinkingBlock := map[string]any{
						"type":     "thinking",
						"thinking": thinkingText,
					}
					if sig := getString(itemMap, "encrypted_content"); sig != "" {
						thinkingBlock["signature"] = sig
					}
					content = append(content, thinkingBlock)
				}
			}
		}
	}

	if len(content) > 0 {
		out["content"] = content
	}

	// Status -> stop_reason
	switch getString(resp, "status") {
	case "completed":
		out["stop_reason"] = "end_turn"
	case "incomplete":
		out["stop_reason"] = "max_tokens"
	case "in_progress":
		out["stop_reason"] = "tool_use"
	default:
		out["stop_reason"] = "end_turn"
	}

	// Usage
	if usage := getMap(resp, "usage"); usage != nil {
		anthropicUsage := make(map[string]any)
		if v := getFloat64(usage, "input_tokens"); v > 0 {
			anthropicUsage["input_tokens"] = int64(v)
		}
		if v := getFloat64(usage, "output_tokens"); v > 0 {
			anthropicUsage["output_tokens"] = int64(v)
		}
		if details := getMap(usage, "input_tokens_details"); details != nil {
			if v := getFloat64(details, "cached_tokens"); v > 0 {
				anthropicUsage["cache_read_input_tokens"] = int64(v)
			}
		}
		if len(anthropicUsage) > 0 {
			out["usage"] = anthropicUsage
		}
	}

	return out
}
