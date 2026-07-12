package convert

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- Chat -> Anthropic ---

func convertChatToAnthropic(req map[string]any) map[string]any {
	out := make(map[string]any)

	// Copy scalar fields
	copyKnownFields(out, req, "model", "temperature", "top_p", "stream")
	setIfNotEmpty(out, "max_tokens", int64(getFloat64(req, "max_tokens")))

	// Stop sequences
	if stop := getSlice(req, "stop"); len(stop) > 0 {
		out["stop_sequences"] = stop
	}

	// Reasoning effort -> thinking
	if effort := getString(req, "reasoning_effort"); effort != "" {
		out["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": mapEffortToBudget(effort),
		}
	}

	// Messages
	if msgs := getSlice(req, "messages"); len(msgs) > 0 {
		anthropicMsgs, systemStr := convertChatMessagesToAnthropic(msgs)
		if len(anthropicMsgs) > 0 {
			out["messages"] = anthropicMsgs
		}
		if systemStr != "" {
			out["system"] = systemStr
		}
	}

	// Tools
	if tools := getSlice(req, "tools"); len(tools) > 0 {
		out["tools"] = convertChatToolsToAnthropic(tools)
		// Default tool_choice to auto if tools present
		out["tool_choice"] = map[string]any{"type": "auto"}
	}

	return out
}

func convertChatMessagesToAnthropic(msgs []any) ([]any, string) {
	var out []any
	var systemContent string

	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role := getString(msg, "role")
		content := msg["content"]

		switch role {
		case "system":
			// Accumulate system content to return separately
			if s, ok := content.(string); ok {
				if systemContent != "" {
					systemContent += "\n\n" + s
				} else {
					systemContent = s
				}
			}
			// Anthropic doesn't have system role in messages array;
			// system is set at top level.
			continue

		case "user":
			anthropicContent := convertChatContentToAnthropic(content)
			out = append(out, map[string]any{
				"role":    "user",
				"content": anthropicContent,
			})

		case "assistant":
			anthropicContent := convertChatAssistantContentToAnthropic(msg, content)
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": anthropicContent,
			})

		case "tool":
			// Anthropic tool_result is a user message with content type tool_result
			toolCallID := getString(msg, "tool_call_id")
			toolContent := convertToolResultToAnthropic(toolCallID, content)
			out = append(out, map[string]any{
				"role":    "user",
				"content": toolContent,
			})
		}
	}

	return out, systemContent
}

func convertChatContentToAnthropic(content any) []any {
	switch c := content.(type) {
	case string:
		return []any{
			map[string]any{
				"type": "text",
				"text": c,
			},
		}
	case []any:
		var out []any
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "text":
				textBlock := map[string]any{
					"type": "text",
					"text": getString(p, "text"),
				}
				preserveCacheControl(textBlock, p)
				out = append(out, textBlock)
			case "image_url":
				imageURL := getString(p, "image_url")
				// image_url can be a string or an object with "url"
				var urlStr string
				if imgObj, ok := p["image_url"].(map[string]any); ok {
					urlStr = getString(imgObj, "url")
				} else {
					urlStr = imageURL
				}
				if urlStr != "" {
					// Parse data URI to extract media_type and base64 data
					mediaType, data := parseDataURI(urlStr)
					if mediaType != "" && data != "" {
						out = append(out, map[string]any{
							"type": "image",
							"source": map[string]any{
								"type":       "base64",
								"media_type": mediaType,
								"data":       data,
							},
						})
					} else {
						// Non-data URI: pass URL directly
						out = append(out, map[string]any{
							"type": "image",
							"source": map[string]any{
								"type": "url",
								"url":  urlStr,
							},
						})
					}
				}
			}
		}
		return out
	}
	return []any{
		map[string]any{
			"type": "text",
			"text": fmt.Sprintf("%v", content),
		},
	}
}

func convertChatAssistantContentToAnthropic(msg map[string]any, content any) []any {
	var out []any

	// Reasoning content from history - prepend thinking block
	if reasoningContent := getString(msg, "reasoning_content"); reasoningContent != "" {
		thinkingBlock := map[string]any{
			"type":     "thinking",
			"thinking": reasoningContent,
		}
		if sig := getString(msg, "reasoning_signature"); sig != "" {
			thinkingBlock["signature"] = sig
		}
		out = append(out, thinkingBlock)
	}

	// Text content
	if content != nil {
		switch c := content.(type) {
		case string:
			if c != "" {
				out = append(out, map[string]any{
					"type": "text",
					"text": c,
				})
			}
		case []any:
			for _, part := range c {
				p, ok := part.(map[string]any)
				if !ok {
					continue
				}
				if getString(p, "type") == "text" {
					out = append(out, map[string]any{
						"type": "text",
						"text": getString(p, "text"),
					})
				}
			}
		default:
			if s, ok := content.(string); ok && s != "" {
				out = append(out, map[string]any{
					"type": "text",
					"text": s,
				})
			}
		}
	}

	// Tool calls
	if toolCalls := getSlice(msg, "tool_calls"); len(toolCalls) > 0 {
		for _, tc := range toolCalls {
			tcMap, ok := tc.(map[string]any)
			if !ok {
				continue
			}
			function := getMap(tcMap, "function")
			if function == nil {
				continue
			}
			out = append(out, map[string]any{
				"type":  "tool_use",
				"id":    getString(tcMap, "id"),
				"name":  getString(function, "name"),
				"input": parseJSONString(getString(function, "arguments")),
			})
		}
	}

	return out
}

func convertToolResultToAnthropic(toolCallID string, content any) []any {
	// Anthropic tool_result format
	contentBlocks := convertChatContentToAnthropic(content)
	return []any{
		map[string]any{
			"type":        "tool_result",
			"tool_use_id": toolCallID,
			"content":     contentBlocks,
		},
	}
}

func convertChatToolsToAnthropic(tools []any) []any {
	var out []any
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		// Non-function type (e.g. server tools) - pass through as-is
		toolType := getString(tool, "type")
		if toolType != "" && toolType != "function" {
			out = append(out, tool)
			continue
		}
		function := getMap(tool, "function")
		if function == nil {
			continue
		}
		anthropicTool := map[string]any{
			"name":         getString(function, "name"),
			"description":  getString(function, "description"),
			"input_schema": parseJSONSchema(getString(function, "parameters")),
		}
		preserveCacheControl(anthropicTool, tool)
		out = append(out, anthropicTool)
	}
	return out
}

// --- Chat -> Responses ---

func convertChatToResponses(req map[string]any) map[string]any {
	out := make(map[string]any)

	copyKnownFields(out, req, "model", "temperature", "top_p", "stream", "instructions")
	setIfNotEmpty(out, "max_output_tokens", int64(getFloat64(req, "max_tokens")))

	// Reasoning effort -> reasoning
	if effort := getString(req, "reasoning_effort"); effort != "" {
		out["reasoning"] = map[string]any{
			"effort": effort,
		}
	}

	// Convert messages to Responses input format; also lift any system messages
	// to top-level instructions (Responses has no system role).
	msgs := getSlice(req, "messages")
	if len(msgs) > 0 {
		var systemParts []string
		filtered := make([]any, 0, len(msgs))
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if getString(msg, "role") == "system" {
				if s, ok := msg["content"].(string); ok && s != "" {
					systemParts = append(systemParts, s)
				}
				continue
			}
			filtered = append(filtered, m)
		}
		if len(systemParts) > 0 {
			systemStr := strings.Join(systemParts, "\n\n")
			if existing := getString(out, "instructions"); existing != "" {
				out["instructions"] = existing + "\n\n" + systemStr
			} else {
				out["instructions"] = systemStr
			}
		}
		out["input"] = convertChatMessagesToResponsesInput(filtered)
	}

	// Tools
	if tools := getSlice(req, "tools"); len(tools) > 0 {
		out["tools"] = convertChatToolsToResponses(tools)
	}

	// Tool choice
	if tc := req["tool_choice"]; tc != nil {
		out["tool_choice"] = tc
	}

	return out
}

func convertChatMessagesToResponsesInput(msgs []any) any {
	// If there's only one user message with a simple string, use string input
	if len(msgs) == 1 {
		msg, ok := msgs[0].(map[string]any)
		if ok && getString(msg, "role") == "user" {
			if s, ok := msg["content"].(string); ok {
				return s
			}
		}
	}

	// Otherwise convert to message array
	var out []any
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role := getString(msg, "role")
		content := msg["content"]

		switch role {
		case "system":
			// System messages are lifted to top-level `instructions` by callers
			// (see convertChatToResponses). If any slip through, skip them.
			continue
		case "user":
			responsesContent := convertChatContentToResponses(content)
			out = append(out, map[string]any{
				"role":    "user",
				"content": responsesContent,
			})
		case "assistant":
			responsesContent := convertChatAssistantContentToResponses(msg, content)
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": responsesContent,
			})
		case "tool":
			responsesContent := convertToolResultToResponses(msg, content)
			out = append(out, map[string]any{
				"role":    "user",
				"content": responsesContent,
			})
		}
	}
	return out
}

func convertChatContentToResponses(content any) []any {
	switch c := content.(type) {
	case string:
		return []any{
			map[string]any{
				"type": "input_text",
				"text": c,
			},
		}
	case []any:
		var out []any
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "text":
				out = append(out, map[string]any{
					"type": "input_text",
					"text": getString(p, "text"),
				})
			case "image_url":
				urlStr := extractImageURL(p)
				if urlStr != "" {
					out = append(out, map[string]any{
						"type":      "input_image",
						"image_url": urlStr,
					})
				}
			}
		}
		return out
	}
	return []any{
		map[string]any{
			"type": "input_text",
			"text": fmt.Sprintf("%v", content),
		},
	}
}

func convertChatAssistantContentToResponses(msg map[string]any, content any) []any {
	var out []any

	// Reasoning content from history - prepend reasoning block
	if reasoningContent := getString(msg, "reasoning_content"); reasoningContent != "" {
		out = append(out, map[string]any{
			"type":    "reasoning",
			"id":      "rs_history",
			"status":  "completed",
			"content": []any{map[string]any{"type": "reasoning_text", "text": reasoningContent}},
		})
	}

	// Text content
	if content != nil {
		switch c := content.(type) {
		case string:
			if c != "" {
				out = append(out, map[string]any{
					"type": "input_text",
					"text": c,
				})
			}
		case []any:
			for _, part := range c {
				p, ok := part.(map[string]any)
				if !ok {
					continue
				}
				if getString(p, "type") == "text" {
					out = append(out, map[string]any{
						"type": "input_text",
						"text": getString(p, "text"),
					})
				}
			}
		}
	}

	// Tool calls
	if toolCalls := getSlice(msg, "tool_calls"); len(toolCalls) > 0 {
		for _, tc := range toolCalls {
			tcMap, ok := tc.(map[string]any)
			if !ok {
				continue
			}
			function := getMap(tcMap, "function")
			if function == nil {
				continue
			}
			out = append(out, map[string]any{
				"type":      "function_call",
				"id":        getString(tcMap, "id"),
				"name":      getString(function, "name"),
				"arguments": getString(function, "arguments"),
			})
		}
	}

	return out
}

func convertToolResultToResponses(msg map[string]any, content any) []any {
	toolCallID := getString(msg, "tool_call_id")
	output := ""
	switch c := content.(type) {
	case string:
		output = c
	case []any:
		// Try to extract text from content blocks
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if getString(p, "type") == "text" {
				output = getString(p, "text")
				break
			}
		}
	default:
		output = fmt.Sprintf("%v", content)
	}
	return []any{
		map[string]any{
			"type":    "function_call_output",
			"call_id": toolCallID,
			"output":  output,
		},
	}
}

func convertChatToolsToResponses(tools []any) []any {
	var out []any
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		// Pass through non-function server tools (e.g. web_search, computer_use) unchanged.
		if toolType := getString(tool, "type"); toolType != "" && toolType != "function" {
			out = append(out, tool)
			continue
		}
		function := getMap(tool, "function")
		if function == nil {
			continue
		}
		responsesTool := map[string]any{
			"type":        "function",
			"name":        getString(function, "name"),
			"description": getString(function, "description"),
			"parameters":  parseJSONSchemaRaw(getString(function, "parameters")),
		}
		preserveCacheControl(responsesTool, tool)
		out = append(out, responsesTool)
	}
	return out
}

// --- Anthropic -> Chat ---

func convertAnthropicToChat(req map[string]any) map[string]any {
	out := make(map[string]any)

	copyKnownFields(out, req, "model", "temperature", "top_p", "stream")
	setIfNotEmpty(out, "max_tokens", int64(getFloat64(req, "max_tokens")))

	// Stop sequences -> stop
	if stopSeq := getSlice(req, "stop_sequences"); len(stopSeq) > 0 {
		out["stop"] = stopSeq
	}

	// Thinking -> reasoning_effort
	if thinking := getMap(req, "thinking"); thinking != nil {
		if budget, ok := thinking["budget_tokens"].(float64); ok && budget > 0 {
			out["reasoning_effort"] = mapBudgetToEffort(int64(budget))
		}
	}

	// System prompt - support both string and array forms
	var systemStr string
	if s := getString(req, "system"); s != "" {
		systemStr = s
	} else if sysArr := getSlice(req, "system"); len(sysArr) > 0 {
		var parts []string
		for _, item := range sysArr {
			if itemMap, ok := item.(map[string]any); ok {
				if getString(itemMap, "type") == "text" {
					parts = append(parts, getString(itemMap, "text"))
				}
			}
		}
		systemStr = stringsJoin(parts, "\n")
	}
	if systemStr != "" {
		out["messages"] = []any{
			map[string]any{
				"role":    "system",
				"content": systemStr,
			},
		}
	}

	// Messages
	if msgs := getSlice(req, "messages"); len(msgs) > 0 {
		chatMsgs := convertAnthropicMessagesToChat(msgs)
		if existing, ok := out["messages"].([]any); ok {
			out["messages"] = append(existing, chatMsgs...)
		} else {
			out["messages"] = chatMsgs
		}
	}

	// Tools
	if tools := getSlice(req, "tools"); len(tools) > 0 {
		out["tools"] = convertAnthropicToolsToChat(tools)
	}

	// Tool choice
	if tc := req["tool_choice"]; tc != nil {
		if tcMap, ok := tc.(map[string]any); ok {
			tcType := getString(tcMap, "type")
			switch tcType {
			case "auto":
				out["tool_choice"] = "auto"
			case "any":
				out["tool_choice"] = "auto"
			case "tool":
				if name := getString(tcMap, "name"); name != "" {
					out["tool_choice"] = map[string]any{
						"type": "function",
						"function": map[string]any{
							"name": name,
						},
					}
				}
			case "none":
				out["tool_choice"] = "none"
			}
		}
	}

	return out
}

func convertAnthropicMessagesToChat(msgs []any) []any {
	var out []any
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role := getString(msg, "role")
		content := msg["content"]

		switch role {
		case "user":
			chatContent := convertAnthropicContentToChat(content, false)
			out = append(out, map[string]any{
				"role":    "user",
				"content": chatContent,
			})
		case "assistant":
			chatContent, toolCalls, reasoningContent, reasoningSignature := convertAnthropicAssistantContentToChat(content)
			entry := map[string]any{
				"role": "assistant",
			}
			if chatContent != "" {
				entry["content"] = chatContent
			} else if len(toolCalls) > 0 {
				// content must be null (not "") when tool_calls is present with no text.
				entry["content"] = nil
			}
			if len(toolCalls) > 0 {
				entry["tool_calls"] = toolCalls
			}
			if reasoningContent != "" {
				entry["reasoning_content"] = reasoningContent
			}
			if reasoningSignature != "" {
				entry["reasoning_signature"] = reasoningSignature
			}
			out = append(out, entry)
		}
	}
	return out
}

func convertAnthropicContentToChat(content any, isToolResult bool) any {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var textParts []string
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "text":
				textParts = append(textParts, getString(p, "text"))
			case "image":
				// Convert Anthropic image to OpenAI image_url format
				source := getMap(p, "source")
				if source != nil {
					sourceType := getString(source, "type")
					switch sourceType {
					case "base64":
						mediaType := getString(source, "media_type")
						data := getString(source, "data")
						if mediaType != "" && data != "" {
							return []any{
								map[string]any{
									"type": "image_url",
									"image_url": map[string]any{
										"url": "data:" + mediaType + ";base64," + data,
									},
								},
							}
						}
					case "url":
						url := getString(source, "url")
						if url != "" {
							return []any{
								map[string]any{
									"type": "image_url",
									"image_url": map[string]any{
										"url": url,
									},
								},
							}
						}
					}
				}
			case "tool_result":
				// Nested tool result - flatten content
				toolContent := p["content"]
				if subContent := convertAnthropicContentToChat(toolContent, true); subContent != nil {
					if s, ok := subContent.(string); ok {
						textParts = append(textParts, s)
					}
				}
			case "tool_use":
				// Should not appear in user content; skip
			}
		}
		if len(textParts) == 1 {
			return textParts[0]
		}
		if len(textParts) > 1 {
			return stringsJoin(textParts, "\n")
		}
		return ""
	}
	return ""
}

func convertAnthropicAssistantContentToChat(content any) (string, []any, string, string) {
	var textParts []string
	var toolCalls []any
	var reasoningContent string
	var reasoningSignature string

	switch c := content.(type) {
	case string:
		return c, nil, "", ""
	case []any:
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "text":
				textParts = append(textParts, getString(p, "text"))
			case "thinking":
				if t := getString(p, "thinking"); t != "" {
					if reasoningContent != "" {
						reasoningContent += "\n"
					}
					reasoningContent += t
				}
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

	text := stringsJoin(textParts, "\n")
	return text, toolCalls, reasoningContent, reasoningSignature
}

func convertAnthropicToolsToChat(tools []any) []any {
	var out []any
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		// Server tools (non-function type) - pass through as-is
		toolType := getString(tool, "type")
		if toolType != "" && toolType != "function" {
			out = append(out, tool)
			continue
		}
		chatTool := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        getString(tool, "name"),
				"description": getString(tool, "description"),
				"parameters":  tool["input_schema"],
			},
		}
		preserveCacheControlPrefixed(chatTool, tool)
		out = append(out, chatTool)
	}
	return out
}

// --- Anthropic -> Responses ---

func convertAnthropicToResponses(req map[string]any) map[string]any {
	out := make(map[string]any)

	copyKnownFields(out, req, "model", "temperature", "top_p", "stream")
	setIfNotEmpty(out, "max_output_tokens", int64(getFloat64(req, "max_tokens")))

	// Thinking -> reasoning
	if thinking := getMap(req, "thinking"); thinking != nil {
		if budget, ok := thinking["budget_tokens"].(float64); ok && budget > 0 {
			out["reasoning"] = map[string]any{
				"effort": mapBudgetToEffort(int64(budget)),
			}
		}
	}

	// System prompt - support both string and array forms
	var system string
	if s := getString(req, "system"); s != "" {
		system = s
	} else if sysArr := getSlice(req, "system"); len(sysArr) > 0 {
		var parts []string
		for _, item := range sysArr {
			if itemMap, ok := item.(map[string]any); ok {
				if getString(itemMap, "type") == "text" {
					parts = append(parts, getString(itemMap, "text"))
				}
			}
		}
		system = stringsJoin(parts, "\n")
	}
	if system != "" {
		out["instructions"] = system
	}

	// Messages
	if msgs := getSlice(req, "messages"); len(msgs) > 0 {
		out["input"] = convertAnthropicMessagesToResponsesInput(msgs)
	}
	// If there are no messages but system is set, it's already captured as instructions above.

	// Tools
	if tools := getSlice(req, "tools"); len(tools) > 0 {
		out["tools"] = convertAnthropicToolsToResponses(tools)
	}

	return out
}

func convertAnthropicMessagesToResponsesInput(msgs []any) any {
	var out []any

	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role := getString(msg, "role")
		content := msg["content"]

		switch role {
		case "user":
			responsesContent := convertAnthropicContentToResponses(content)
			out = append(out, map[string]any{
				"role":    "user",
				"content": responsesContent,
			})
		case "assistant":
			responsesContent := convertAnthropicAssistantContentToResponses(content)
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": responsesContent,
			})
		}
	}

	return out
}

func convertAnthropicContentToResponses(content any) []any {
	switch c := content.(type) {
	case string:
		return []any{
			map[string]any{
				"type": "input_text",
				"text": c,
			},
		}
	case []any:
		var out []any
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "text":
				out = append(out, map[string]any{
					"type": "input_text",
					"text": getString(p, "text"),
				})
			case "image":
				source := getMap(p, "source")
				if source != nil {
					sourceType := getString(source, "type")
					switch sourceType {
					case "base64":
						mediaType := getString(source, "media_type")
						data := getString(source, "data")
						if mediaType != "" && data != "" {
							out = append(out, map[string]any{
								"type":      "input_image",
								"image_url": "data:" + mediaType + ";base64," + data,
							})
						}
					case "url":
						url := getString(source, "url")
						if url != "" {
							out = append(out, map[string]any{
								"type":      "input_image",
								"image_url": url,
							})
						}
					}
				}
			case "tool_result":
				// Flatten tool result text
				if tc := p["content"]; tc != nil {
					text := extractTextFromAnthropicContent(tc)
					if text != "" {
						out = append(out, map[string]any{
							"type": "input_text",
							"text": text,
						})
					}
				}
			}
		}
		return out
	}
	return nil
}

func convertAnthropicAssistantContentToResponses(content any) []any {
	var out []any

	switch c := content.(type) {
	case string:
		if c != "" {
			out = append(out, map[string]any{
				"type": "input_text",
				"text": c,
			})
		}
	case []any:
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "text":
				text := getString(p, "text")
				if text != "" {
					out = append(out, map[string]any{
						"type": "input_text",
						"text": text,
					})
				}
			case "thinking":
				thinkingText := getString(p, "thinking")
				if thinkingText != "" {
					reasoningItem := map[string]any{
						"type":    "reasoning",
						"id":      "rs_history",
						"status":  "completed",
						"content": []any{map[string]any{"type": "reasoning_text", "text": thinkingText}},
					}
					if sig := getString(p, "signature"); sig != "" {
						reasoningItem["encrypted_content"] = sig
					}
					out = append(out, reasoningItem)
				}
			case "tool_use":
				out = append(out, map[string]any{
					"type":      "function_call",
					"id":        getString(p, "id"),
					"name":      getString(p, "name"),
					"arguments": mapToJSONString(getMap(p, "input")),
				})
			}
		}
	}

	return out
}

func convertAnthropicToolsToResponses(tools []any) []any {
	var out []any
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		// Server tools (non-function type) - pass through as-is
		toolType := getString(tool, "type")
		if toolType != "" && toolType != "function" {
			out = append(out, tool)
			continue
		}
		responsesTool := map[string]any{
			"type":        "function",
			"name":        getString(tool, "name"),
			"description": getString(tool, "description"),
			"parameters":  tool["input_schema"],
		}
		preserveCacheControl(responsesTool, tool)
		out = append(out, responsesTool)
	}
	return out
}

// --- Responses -> Chat ---

func convertResponsesToChat(req map[string]any) map[string]any {
	out := make(map[string]any)

	copyKnownFields(out, req, "model", "temperature", "top_p", "stream")
	setIfNotEmpty(out, "max_tokens", int64(getFloat64(req, "max_output_tokens")))

	// Reasoning -> reasoning_effort
	if reasoning := getMap(req, "reasoning"); reasoning != nil {
		if effort := getString(reasoning, "effort"); effort != "" {
			out["reasoning_effort"] = effort
		}
	}

	// Instructions -> system message
	instructions := getString(req, "instructions")
	input := req["input"]

	// Build messages
	var msgs []any

	if instructions != "" {
		msgs = append(msgs, map[string]any{
			"role":    "system",
			"content": instructions,
		})
	}

	// Convert input to messages
	if input != nil {
		chatMsgs := convertResponsesInputToChat(input)
		msgs = append(msgs, chatMsgs...)
	}

	if len(msgs) > 0 {
		out["messages"] = msgs
	}

	// Tools
	if tools := getSlice(req, "tools"); len(tools) > 0 {
		out["tools"] = convertResponsesToolsToChat(tools)
	}

	// Tool choice
	if tc := req["tool_choice"]; tc != nil {
		out["tool_choice"] = tc
	}

	return out
}

func convertResponsesInputToChat(input any) []any {
	var msgs []any

	switch inp := input.(type) {
	case string:
		msgs = append(msgs, map[string]any{
			"role":    "user",
			"content": inp,
		})
	case []any:
		for _, item := range inp {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			role := getString(itemMap, "role")
			content := itemMap["content"]

			// Handle top-level function_call_output items (no role field).
			if itemType := getString(itemMap, "type"); itemType == "function_call_output" {
				callID := getString(itemMap, "call_id")
				output := extractOutput(itemMap)
				msgs = append(msgs, map[string]any{
					"role":         "tool",
					"tool_call_id": callID,
					"content":      output,
				})
				continue
			}

			switch role {
			case "user":
				// Separate function_call_output items from other content parts.
				if contentArr, isArr := content.([]any); isArr {
					var userParts []any
					var toolMsgs []any
					for _, part := range contentArr {
						p, ok := part.(map[string]any)
						if !ok {
							userParts = append(userParts, part)
							continue
						}
						if getString(p, "type") == "function_call_output" {
							callID := getString(p, "call_id")
							output := extractOutput(p)
							toolMsgs = append(toolMsgs, map[string]any{
								"role":         "tool",
								"tool_call_id": callID,
								"content":      output,
							})
						} else {
							userParts = append(userParts, part)
						}
					}
					if len(userParts) > 0 {
						chatContent := convertResponsesContentToChat(userParts)
						msgs = append(msgs, map[string]any{
							"role":    "user",
							"content": chatContent,
						})
					}
					msgs = append(msgs, toolMsgs...)
				} else {
					chatContent := convertResponsesContentToChat(content)
					msgs = append(msgs, map[string]any{
						"role":    "user",
						"content": chatContent,
					})
				}
			case "assistant":
				chatContent, toolCalls, reasoningContent, reasoningSignature := convertResponsesAssistantContentToChat(content)
				entry := map[string]any{
					"role": "assistant",
				}
				if chatContent != "" {
					entry["content"] = chatContent
				} else if len(toolCalls) > 0 {
					// content must be null (not "") when tool_calls is present with no text.
					entry["content"] = nil
				}
				if len(toolCalls) > 0 {
					entry["tool_calls"] = toolCalls
				}
				if reasoningContent != "" {
					entry["reasoning_content"] = reasoningContent
				}
				if reasoningSignature != "" {
					entry["reasoning_signature"] = reasoningSignature
				}
				msgs = append(msgs, entry)
			case "system":
				// Convert to system message
				chatContent := convertResponsesContentToChat(content)
				if s, ok := chatContent.(string); ok {
					msgs = append(msgs, map[string]any{
						"role":    "system",
						"content": s,
					})
				}
			}
		}
	}

	return msgs
}

func convertResponsesContentToChat(content any) any {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var textParts []string
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "input_text":
				textParts = append(textParts, getString(p, "text"))
			case "input_image":
				imageURL := getString(p, "image_url")
				if imageURL != "" {
					return []any{
						map[string]any{
							"type": "image_url",
							"image_url": map[string]any{
								"url": imageURL,
							},
						},
					}
				}
			case "function_call_output":
				// Handled at the caller level (extracted into separate tool messages).
			}
		}
		if len(textParts) == 1 {
			return textParts[0]
		}
		if len(textParts) > 1 {
			return stringsJoin(textParts, "\n")
		}
		return ""
	}
	return ""
}

func convertResponsesAssistantContentToChat(content any) (string, []any, string, string) {
	var textParts []string
	var toolCalls []any
	var reasoningContent string
	var reasoningSignature string

	switch c := content.(type) {
	case string:
		return c, nil, "", ""
	case []any:
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "input_text":
				textParts = append(textParts, getString(p, "text"))
			case "reasoning":
				contentArr := getSlice(p, "content")
				if len(contentArr) > 0 {
					if first, ok := contentArr[0].(map[string]any); ok {
						if t := getString(first, "text"); t != "" {
							if reasoningContent != "" {
								reasoningContent += "\n"
							}
							reasoningContent += t
						}
					}
				}
				if sig := getString(p, "encrypted_content"); sig != "" {
					reasoningSignature = sig
				}
			case "function_call":
				toolCalls = append(toolCalls, map[string]any{
					"id":   getString(p, "id"),
					"type": "function",
					"function": map[string]any{
						"name":      getString(p, "name"),
						"arguments": getString(p, "arguments"),
					},
				})
			}
		}
	}

	return stringsJoin(textParts, "\n"), toolCalls, reasoningContent, reasoningSignature
}

func convertResponsesToolsToChat(tools []any) []any {
	var out []any
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		// Non-function type (e.g. server tools) - pass through as-is
		toolType := getString(tool, "type")
		if toolType != "" && toolType != "function" {
			out = append(out, tool)
			continue
		}
		chatTool := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        getString(tool, "name"),
				"description": getString(tool, "description"),
				"parameters":  tool["parameters"],
			},
		}
		restoreCacheControl(chatTool, tool)
		out = append(out, chatTool)
	}
	return out
}

// --- Responses -> Anthropic ---

func convertResponsesToAnthropic(req map[string]any) map[string]any {
	out := make(map[string]any)

	copyKnownFields(out, req, "model", "temperature", "top_p", "stream")
	setIfNotEmpty(out, "max_tokens", int64(getFloat64(req, "max_output_tokens")))

	// Reasoning -> thinking
	if reasoning := getMap(req, "reasoning"); reasoning != nil {
		if effort := getString(reasoning, "effort"); effort != "" {
			out["thinking"] = map[string]any{
				"type":          "enabled",
				"budget_tokens": mapEffortToBudget(effort),
			}
		}
	}

	// Instructions -> system (not fake user prefix)
	instructions := getString(req, "instructions")
	if instructions != "" {
		out["system"] = instructions
	}

	// Input
	input := req["input"]

	// Build messages
	var msgs []any

	if input != nil {
		anthropicMsgs := convertResponsesInputToAnthropic(input)
		msgs = append(msgs, anthropicMsgs...)
	}

	if len(msgs) > 0 {
		out["messages"] = msgs
	}

	// Tools
	if tools := getSlice(req, "tools"); len(tools) > 0 {
		out["tools"] = convertResponsesToolsToAnthropic(tools)
		if _, ok := out["tool_choice"]; !ok {
			out["tool_choice"] = map[string]any{"type": "auto"}
		}
	}

	return out
}

func convertResponsesInputToAnthropic(input any) []any {
	var msgs []any

	switch inp := input.(type) {
	case string:
		msgs = append(msgs, map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "text",
					"text": inp,
				},
			},
		})
	case []any:
		for _, item := range inp {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			role := getString(itemMap, "role")
			content := itemMap["content"]

			// Handle top-level function_call_output items (no role field).
			if itemType := getString(itemMap, "type"); itemType == "function_call_output" {
				callID := getString(itemMap, "call_id")
				output := extractOutput(itemMap)
				msgs = append(msgs, map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{
							"type":        "tool_result",
							"tool_use_id": callID,
							"content":     output,
						},
					},
				})
				continue
			}

			switch role {
			case "user":
				// Separate function_call_output items from other content parts.
				if contentArr, isArr := content.([]any); isArr {
					var userParts []any
					var toolMsgs []any
					for _, part := range contentArr {
						p, ok := part.(map[string]any)
						if !ok {
							userParts = append(userParts, part)
							continue
						}
						if getString(p, "type") == "function_call_output" {
							callID := getString(p, "call_id")
							output := extractOutput(p)
							toolMsgs = append(toolMsgs, map[string]any{
								"role": "user",
								"content": []any{
									map[string]any{
										"type":        "tool_result",
										"tool_use_id": callID,
										"content":     output,
									},
								},
							})
						} else {
							userParts = append(userParts, part)
						}
					}
					if len(userParts) > 0 {
						anthropicContent := convertResponsesContentToAnthropic(userParts)
						msgs = append(msgs, map[string]any{
							"role":    "user",
							"content": anthropicContent,
						})
					}
					msgs = append(msgs, toolMsgs...)
				} else {
					anthropicContent := convertResponsesContentToAnthropic(content)
					msgs = append(msgs, map[string]any{
						"role":    "user",
						"content": anthropicContent,
					})
				}
			case "assistant":
				anthropicContent := convertResponsesAssistantContentToAnthropic(content)
				msgs = append(msgs, map[string]any{
					"role":    "assistant",
					"content": anthropicContent,
				})
			case "system":
				// Convert to user message
				text := extractTextFromResponsesContent(content)
				if text != "" {
					msgs = append(msgs, map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{
								"type": "text",
								"text": "[System]\n" + text,
							},
						},
					})
				}
			}
		}
	}

	return msgs
}

func convertResponsesContentToAnthropic(content any) []any {
	switch c := content.(type) {
	case string:
		return []any{
			map[string]any{
				"type": "text",
				"text": c,
			},
		}
	case []any:
		var out []any
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "input_text":
				out = append(out, map[string]any{
					"type": "text",
					"text": getString(p, "text"),
				})
			case "input_image":
				imageURL := getString(p, "image_url")
				if imageURL != "" {
					// Try to parse data URI
					mediaType, data := parseDataURI(imageURL)
					if mediaType != "" && data != "" {
						out = append(out, map[string]any{
							"type": "image",
							"source": map[string]any{
								"type":       "base64",
								"media_type": mediaType,
								"data":       data,
							},
						})
					} else {
						// Non-data URI: pass URL directly
						out = append(out, map[string]any{
							"type": "image",
							"source": map[string]any{
								"type": "url",
								"url":  imageURL,
							},
						})
					}
				}
			case "function_call_output":
				// Handled at the caller level (extracted into separate tool_result messages).
			}
		}
		return out
	}
	return nil
}

func convertResponsesAssistantContentToAnthropic(content any) []any {
	var out []any

	switch c := content.(type) {
	case string:
		if c != "" {
			out = append(out, map[string]any{
				"type": "text",
				"text": c,
			})
		}
	case []any:
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "input_text":
				text := getString(p, "text")
				if text != "" {
					out = append(out, map[string]any{
						"type": "text",
						"text": text,
					})
				}
			case "reasoning":
				contentArr := getSlice(p, "content")
				if len(contentArr) > 0 {
					if first, ok := contentArr[0].(map[string]any); ok {
						thinkingText := getString(first, "text")
						if thinkingText != "" {
							thinkingBlock := map[string]any{
								"type":     "thinking",
								"thinking": thinkingText,
							}
							if sig := getString(p, "encrypted_content"); sig != "" {
								thinkingBlock["signature"] = sig
							}
							out = append(out, thinkingBlock)
						}
					}
				}
			case "function_call":
				out = append(out, map[string]any{
					"type":  "tool_use",
					"id":    getString(p, "id"),
					"name":  getString(p, "name"),
					"input": parseJSONString(getString(p, "arguments")),
				})
			}
		}
	}

	return out
}

func convertResponsesToolsToAnthropic(tools []any) []any {
	var out []any
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		// Non-function type (e.g. server tools) - pass through as-is
		toolType := getString(tool, "type")
		if toolType != "" && toolType != "function" {
			out = append(out, tool)
			continue
		}
		anthropicTool := map[string]any{
			"name":         getString(tool, "name"),
			"description":  getString(tool, "description"),
			"input_schema": tool["parameters"],
		}
		preserveCacheControl(anthropicTool, tool)
		out = append(out, anthropicTool)
	}
	return out
}

// --- Utility functions ---

// mapEffortToBudget converts OpenAI reasoning_effort levels to Anthropic budget_tokens.
func mapEffortToBudget(effort string) int64 {
	switch effort {
	case "minimal":
		return 512
	case "low":
		return 1024
	case "medium":
		return 4096
	case "high":
		return 16000
	default:
		return 1024
	}
}

// mapBudgetToEffort reverses mapEffortToBudget with sensible bucket boundaries.
func mapBudgetToEffort(budget int64) string {
	switch {
	case budget < 1024:
		return "minimal"
	case budget < 4096:
		return "low"
	case budget < 16000:
		return "medium"
	default:
		return "high"
	}
}

// preserveCacheControl copies cache_control from src to dst if present.
func preserveCacheControl(dst, src map[string]any) {
	if cc, ok := src["cache_control"]; ok {
		dst["cache_control"] = cc
	}
}

// preserveCacheControlPrefixed copies cache_control from src to dst as _cache_control.
func preserveCacheControlPrefixed(dst, src map[string]any) {
	if cc, ok := src["cache_control"]; ok {
		dst["_cache_control"] = cc
	}
}

// restoreCacheControl copies _cache_control from src to dst as cache_control if present.
func restoreCacheControl(dst, src map[string]any) {
	if cc, ok := src["_cache_control"]; ok {
		dst["cache_control"] = cc
	}
}

// extractOutput extracts the "output" field from a function_call_output item.
// If output is not a string, it JSON-encodes the value.
func extractOutput(item map[string]any) string {
	output := getString(item, "output")
	if output == "" {
		if v, ok := item["output"]; ok && v != nil {
			if _, isStr := v.(string); !isStr {
				if b, err := json.Marshal(v); err == nil {
					output = string(b)
				}
			}
		}
	}
	return output
}

func extractImageURL(p map[string]any) string {
	imageURL := p["image_url"]
	switch u := imageURL.(type) {
	case string:
		return u
	case map[string]any:
		return getString(u, "url")
	}
	return ""
}

func parseJSONString(s string) any {
	if s == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return map[string]any{}
}

func parseJSONSchema(s string) any {
	if s == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return map[string]any{}
}

func parseJSONSchemaRaw(s string) any {
	if s == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return s
}

func mapToJSONString(m map[string]any) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func stringsJoin(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}

func extractTextFromAnthropicContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if getString(p, "type") == "text" {
				return getString(p, "text")
			}
		}
	}
	return ""
}

func extractTextFromResponsesContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if getString(p, "type") == "input_text" {
				return getString(p, "text")
			}
		}
	}
	return ""
}

func parseDataURI(uri string) (mediaType string, data string) {
	// Expected format: data:image/png;base64,<data>
	if len(uri) < 5 || uri[:5] != "data:" {
		return "", uri
	}
	rest := uri[5:]
	commaIdx := -1
	for i, c := range rest {
		if c == ',' {
			commaIdx = i
			break
		}
	}
	if commaIdx < 0 {
		return "", uri
	}
	mediaType = rest[:commaIdx]
	data = rest[commaIdx+1:]
	// Strip parameters like ;base64 from media_type
	if semiIdx := strings.Index(mediaType, ";"); semiIdx >= 0 {
		mediaType = mediaType[:semiIdx]
	}
	return mediaType, data
}
