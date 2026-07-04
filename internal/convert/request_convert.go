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

	// Messages
	if msgs := getSlice(req, "messages"); len(msgs) > 0 {
		anthropicMsgs := convertChatMessagesToAnthropic(msgs)
		if len(anthropicMsgs) > 0 {
			out["messages"] = anthropicMsgs
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

func convertChatMessagesToAnthropic(msgs []any) []any {
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
			// Accumulate system content to prepend as user message
			if s, ok := content.(string); ok {
				if systemContent != "" {
					systemContent += "\n" + s
				} else {
					systemContent = s
				}
			}
			// Anthropic doesn't have system role in messages array;
			// we'll prepend it as a user message with a prefix.
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

	// Prepend system content as first user message
	if systemContent != "" {
		prefix := map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "text",
					"text": "[System instruction]\n" + systemContent,
				},
			},
		}
		out = append([]any{prefix}, out...)
	}

	return out
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
				out = append(out, map[string]any{
					"type": "text",
					"text": getString(p, "text"),
				})
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
		function := getMap(tool, "function")
		if function == nil {
			continue
		}
		out = append(out, map[string]any{
			"name":         getString(function, "name"),
			"description":  getString(function, "description"),
			"input_schema": parseJSONSchema(getString(function, "parameters")),
		})
	}
	return out
}

// --- Chat -> Responses ---

func convertChatToResponses(req map[string]any) map[string]any {
	out := make(map[string]any)

	copyKnownFields(out, req, "model", "temperature", "top_p", "stream", "instructions")
	setIfNotEmpty(out, "max_output_tokens", int64(getFloat64(req, "max_tokens")))

	// Convert messages to Responses input format
	if msgs := getSlice(req, "messages"); len(msgs) > 0 {
		out["input"] = convertChatMessagesToResponsesInput(msgs)
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
			// Responses doesn't have system role; convert to developer instruction
			// or prepend as user message
			if s, ok := content.(string); ok {
				out = append(out, map[string]any{
					"role":    "user",
					"content": "[System]\n" + s,
				})
			}
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
		function := getMap(tool, "function")
		if function == nil {
			continue
		}
		out = append(out, map[string]any{
			"type":        "function",
			"name":        getString(function, "name"),
			"description": getString(function, "description"),
			"parameters":  parseJSONSchemaRaw(getString(function, "parameters")),
		})
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

	// System prompt
	if system := getString(req, "system"); system != "" {
		out["messages"] = []any{
			map[string]any{
				"role":    "system",
				"content": system,
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
			chatContent, toolCalls := convertAnthropicAssistantContentToChat(content)
			entry := map[string]any{
				"role":    "assistant",
				"content": chatContent,
			}
			if len(toolCalls) > 0 {
				entry["tool_calls"] = toolCalls
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
					// We'll create a data URI
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

func convertAnthropicAssistantContentToChat(content any) (string, []any) {
	var textParts []string
	var toolCalls []any

	switch c := content.(type) {
	case string:
		return c, nil
	case []any:
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "text":
				textParts = append(textParts, getString(p, "text"))
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
	return text, toolCalls
}

func convertAnthropicToolsToChat(tools []any) []any {
	var out []any
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        getString(tool, "name"),
				"description": getString(tool, "description"),
				"parameters":  tool["input_schema"],
			},
		})
	}
	return out
}

// --- Anthropic -> Responses ---

func convertAnthropicToResponses(req map[string]any) map[string]any {
	out := make(map[string]any)

	copyKnownFields(out, req, "model", "temperature", "top_p", "stream")
	setIfNotEmpty(out, "max_output_tokens", int64(getFloat64(req, "max_tokens")))

	// System prompt
	var system string
	if s := getString(req, "system"); s != "" {
		system = s
	}

	// Messages
	if msgs := getSlice(req, "messages"); len(msgs) > 0 {
		out["input"] = convertAnthropicMessagesToResponsesInput(msgs, system)
	} else if system != "" {
		out["input"] = "[System]\n" + system
	}

	// Tools
	if tools := getSlice(req, "tools"); len(tools) > 0 {
		out["tools"] = convertAnthropicToolsToResponses(tools)
	}

	return out
}

func convertAnthropicMessagesToResponsesInput(msgs []any, system string) any {
	var out []any

	if system != "" {
		out = append(out, map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "input_text",
					"text": "[System]\n" + system,
				},
			},
		})
	}

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
					mediaType := getString(source, "media_type")
					data := getString(source, "data")
					if mediaType != "" && data != "" {
						out = append(out, map[string]any{
							"type":      "input_image",
							"image_url": "data:" + mediaType + ";base64," + data,
						})
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
		out = append(out, map[string]any{
			"type":        "function",
			"name":        getString(tool, "name"),
			"description": getString(tool, "description"),
			"parameters":  tool["input_schema"],
		})
	}
	return out
}

// --- Responses -> Chat ---

func convertResponsesToChat(req map[string]any) map[string]any {
	out := make(map[string]any)

	copyKnownFields(out, req, "model", "temperature", "top_p", "stream")
	setIfNotEmpty(out, "max_tokens", int64(getFloat64(req, "max_output_tokens")))

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

			switch role {
			case "user":
				chatContent := convertResponsesContentToChat(content)
				msgs = append(msgs, map[string]any{
					"role":    "user",
					"content": chatContent,
				})
			case "assistant":
				chatContent, toolCalls := convertResponsesAssistantContentToChat(content)
				entry := map[string]any{
					"role":    "assistant",
					"content": chatContent,
				}
				if len(toolCalls) > 0 {
					entry["tool_calls"] = toolCalls
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
				textParts = append(textParts, getString(p, "output"))
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

func convertResponsesAssistantContentToChat(content any) (string, []any) {
	var textParts []string
	var toolCalls []any

	switch c := content.(type) {
	case string:
		return c, nil
	case []any:
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "input_text":
				textParts = append(textParts, getString(p, "text"))
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

	return stringsJoin(textParts, "\n"), toolCalls
}

func convertResponsesToolsToChat(tools []any) []any {
	var out []any
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        getString(tool, "name"),
				"description": getString(tool, "description"),
				"parameters":  tool["parameters"],
			},
		})
	}
	return out
}

// --- Responses -> Anthropic ---

func convertResponsesToAnthropic(req map[string]any) map[string]any {
	out := make(map[string]any)

	copyKnownFields(out, req, "model", "temperature", "top_p", "stream")
	setIfNotEmpty(out, "max_tokens", int64(getFloat64(req, "max_output_tokens")))

	// Instructions
	instructions := getString(req, "instructions")

	// Input
	input := req["input"]

	// Build messages
	var msgs []any

	if instructions != "" {
		msgs = append(msgs, map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "text",
					"text": "[System]\n" + instructions,
				},
			},
		})
	}

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

			switch role {
			case "user":
				anthropicContent := convertResponsesContentToAnthropic(content)
				msgs = append(msgs, map[string]any{
					"role":    "user",
					"content": anthropicContent,
				})
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
					}
				}
			case "function_call_output":
				out = append(out, map[string]any{
					"type": "text",
					"text": getString(p, "output"),
				})
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
		out = append(out, map[string]any{
			"name":         getString(tool, "name"),
			"description":  getString(tool, "description"),
			"input_schema": tool["parameters"],
		})
	}
	return out
}

// --- Utility functions ---

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
