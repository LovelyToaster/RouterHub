package convert

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
		for _, part := range c {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch getString(p, "type") {
			case "text":
				text += getString(p, "text")
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
		if text != "" {
			output = append([]any{map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": text,
					},
				},
			}}, output...)
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
			}
		}
	}

	message["content"] = text
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
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
		if len(anthropicUsage) > 0 {
			out["usage"] = anthropicUsage
		}
	}

	return out
}
