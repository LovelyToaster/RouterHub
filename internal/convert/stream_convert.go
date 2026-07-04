package convert

// --- Chat Stream -> Anthropic Stream ---

func convertChatStreamToAnthropic(event map[string]any) map[string]any {
	choices := getSlice(event, "choices")
	if len(choices) == 0 {
		return nil
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil
	}

	delta := getMap(choice, "delta")
	if delta == nil {
		return nil
	}

	finishReason := getString(choice, "finish_reason")

	// Check for tool calls in delta
	if toolCalls := getSlice(delta, "tool_calls"); len(toolCalls) > 0 {
		return convertChatToolCallDeltaToAnthropic(toolCalls, finishReason)
	}

	// Text delta
	content := getString(delta, "content")
	if content == "" && finishReason == "" {
		return nil
	}

	out := make(map[string]any)
	out["type"] = "content_block_delta"
	out["index"] = 0
	deltaBlock := make(map[string]any)
	deltaBlock["type"] = "text_delta"
	deltaBlock["text"] = content
	out["delta"] = deltaBlock

	return out
}

func convertChatToolCallDeltaToAnthropic(toolCalls []any, finishReason string) map[string]any {
	// Anthropic sends tool_use as content_block_start with partial JSON
	// For simplicity, we send the first complete tool call if available
	for _, tc := range toolCalls {
		tcMap, ok := tc.(map[string]any)
		if !ok {
			continue
		}
		function := getMap(tcMap, "function")
		if function == nil {
			continue
		}
		name := getString(function, "name")
		arguments := getString(function, "arguments")

		if name != "" && arguments != "" {
			return map[string]any{
				"type":  "content_block_start",
				"index": 0,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    getString(tcMap, "id"),
					"name":  name,
					"input": parseJSONString(arguments),
				},
			}
		}
	}

	// If finish_reason is tool_calls but no complete tool call, send stop
	if finishReason == "tool_calls" {
		return map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   "tool_use",
				"stop_sequence": nil,
			},
		}
	}

	return nil
}

// --- Anthropic Stream -> Chat Stream ---

func convertAnthropicStreamToChat(event map[string]any) map[string]any {
	eventType := getString(event, "type")

	switch eventType {
	case "content_block_delta":
		delta := getMap(event, "delta")
		if delta == nil {
			return nil
		}
		deltaType := getString(delta, "type")
		switch deltaType {
		case "text_delta":
			text := getString(delta, "text")
			if text == "" {
				return nil
			}
			return buildChatChunk(text, "")
		}

	case "content_block_start":
		contentBlock := getMap(event, "content_block")
		if contentBlock == nil {
			return nil
		}
		if getString(contentBlock, "type") == "tool_use" {
			// Anthropic sends tool_use as a complete block at start
			return buildChatToolCallChunk(
				getString(contentBlock, "id"),
				getString(contentBlock, "name"),
				mapToJSONString(getMap(contentBlock, "input")),
			)
		}

	case "message_delta":
		delta := getMap(event, "delta")
		if delta == nil {
			return nil
		}
		stopReason := getString(delta, "stop_reason")
		if stopReason != "" {
			chatFinishReason := "stop"
			switch stopReason {
			case "end_turn":
				chatFinishReason = "stop"
			case "max_tokens":
				chatFinishReason = "length"
			case "tool_use":
				chatFinishReason = "tool_calls"
			case "stop_sequence":
				chatFinishReason = "stop"
			}
			return buildChatFinishChunk(chatFinishReason)
		}

	case "message_start":
		// Anthropic sends message_start with just the message metadata
		// We can ignore this for chat conversion

	case "message_stop":
		// No-op for chat, the finish chunk already sent
		return nil

	case "ping":
		// Anthropic sends keep-alive pings; ignore
		return nil
	}

	return nil
}

// --- Chat Stream -> Responses Stream ---

func convertChatStreamToResponses(event map[string]any) map[string]any {
	choices := getSlice(event, "choices")
	if len(choices) == 0 {
		return nil
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil
	}

	delta := getMap(choice, "delta")
	if delta == nil {
		return nil
	}

	finishReason := getString(choice, "finish_reason")

	// Tool calls
	if toolCalls := getSlice(delta, "tool_calls"); len(toolCalls) > 0 {
		return convertChatToolCallToResponsesStream(toolCalls)
	}

	// Text delta
	content := getString(delta, "content")
	if content == "" && finishReason == "" {
		return nil
	}

	if content != "" {
		return map[string]any{
			"type":          "response.output_text.delta",
			"delta":         content,
			"item_id":       nil,
			"output_index":  0,
			"content_index": 0,
		}
	}

	// Finish
	if finishReason != "" {
		status := "completed"
		switch finishReason {
		case "stop":
			status = "completed"
		case "length":
			status = "incomplete"
		case "tool_calls":
			status = "in_progress"
		}
		return map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"status": status,
			},
		}
	}

	return nil
}

func convertChatToolCallToResponsesStream(toolCalls []any) map[string]any {
	for _, tc := range toolCalls {
		tcMap, ok := tc.(map[string]any)
		if !ok {
			continue
		}
		function := getMap(tcMap, "function")
		if function == nil {
			continue
		}
		return map[string]any{
			"type":          "response.function_call_arguments.delta",
			"delta":         getString(function, "arguments"),
			"item_id":       getString(tcMap, "id"),
			"output_index":  0,
			"content_index": 0,
		}
	}
	return nil
}

// --- Anthropic Stream -> Responses Stream ---

func convertAnthropicStreamToResponses(event map[string]any) map[string]any {
	eventType := getString(event, "type")

	switch eventType {
	case "content_block_delta":
		delta := getMap(event, "delta")
		if delta == nil {
			return nil
		}
		deltaType := getString(delta, "type")
		switch deltaType {
		case "text_delta":
			text := getString(delta, "text")
			if text == "" {
				return nil
			}
			return map[string]any{
				"type":          "response.output_text.delta",
				"delta":         text,
				"item_id":       nil,
				"output_index":  0,
				"content_index": 0,
			}
		}

	case "content_block_start":
		contentBlock := getMap(event, "content_block")
		if contentBlock == nil {
			return nil
		}
		if getString(contentBlock, "type") == "tool_use" {
			return map[string]any{
				"type":          "response.function_call_arguments.delta",
				"delta":         mapToJSONString(getMap(contentBlock, "input")),
				"item_id":       getString(contentBlock, "id"),
				"output_index":  0,
				"content_index": 0,
			}
		}

	case "message_delta":
		delta := getMap(event, "delta")
		if delta == nil {
			return nil
		}
		stopReason := getString(delta, "stop_reason")
		if stopReason != "" {
			status := "completed"
			switch stopReason {
			case "end_turn":
				status = "completed"
			case "max_tokens":
				status = "incomplete"
			case "tool_use":
				status = "in_progress"
			}
			return map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"status": status,
				},
			}
		}

	case "message_start", "message_stop", "ping":
		return nil
	}

	return nil
}

// --- Responses Stream -> Chat Stream ---

func convertResponsesStreamToChat(event map[string]any) map[string]any {
	eventType := getString(event, "type")

	switch eventType {
	case "response.output_text.delta":
		delta := getString(event, "delta")
		if delta == "" {
			return nil
		}
		return buildChatChunk(delta, "")

	case "response.function_call_arguments.delta":
		delta := getString(event, "delta")
		if delta == "" {
			return nil
		}
		return buildChatToolCallChunk(
			getString(event, "item_id"),
			"", // name not available in delta
			delta,
		)

	case "response.completed":
		response := getMap(event, "response")
		if response == nil {
			return nil
		}
		status := getString(response, "status")
		finishReason := "stop"
		switch status {
		case "completed":
			finishReason = "stop"
		case "incomplete":
			finishReason = "length"
		case "in_progress":
			finishReason = "tool_calls"
		}
		return buildChatFinishChunk(finishReason)

	case "response.in_progress":
		// Ignore
		return nil
	}

	return nil
}

// --- Responses Stream -> Anthropic Stream ---

func convertResponsesStreamToAnthropic(event map[string]any) map[string]any {
	eventType := getString(event, "type")

	switch eventType {
	case "response.output_text.delta":
		delta := getString(event, "delta")
		if delta == "" {
			return nil
		}
		return map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": delta,
			},
		}

	case "response.function_call_arguments.delta":
		delta := getString(event, "delta")
		if delta == "" {
			return nil
		}
		// Anthropic expects tool_use as content_block_start with complete input
		// We'll send it as a content_block_start
		return map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    getString(event, "item_id"),
				"name":  "", // name not available in delta
				"input": parseJSONString(delta),
			},
		}

	case "response.completed":
		response := getMap(event, "response")
		if response == nil {
			return nil
		}
		status := getString(response, "status")
		stopReason := "end_turn"
		switch status {
		case "completed":
			stopReason = "end_turn"
		case "incomplete":
			stopReason = "max_tokens"
		case "in_progress":
			stopReason = "tool_use"
		}
		return map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   stopReason,
				"stop_sequence": nil,
			},
		}

	case "response.in_progress":
		return nil
	}

	return nil
}

// --- Chat chunk builders ---

func buildChatChunk(text, finishReason string) map[string]any {
	out := make(map[string]any)
	out["object"] = "chat.completion.chunk"
	out["choices"] = []any{
		map[string]any{
			"index": 0,
			"delta": map[string]any{
				"content": text,
			},
			"finish_reason": nil,
		},
	}
	return out
}

func buildChatToolCallChunk(id, name, arguments string) map[string]any {
	toolCall := map[string]any{
		"function": map[string]any{
			"arguments": arguments,
		},
	}
	if id != "" {
		toolCall["id"] = id
	}
	if name != "" {
		toolCall["function"].(map[string]any)["name"] = name
	}

	return map[string]any{
		"object": "chat.completion.chunk",
		"choices": []any{
			map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{toolCall},
				},
				"finish_reason": nil,
			},
		},
	}
}

func buildChatFinishChunk(finishReason string) map[string]any {
	return map[string]any{
		"object": "chat.completion.chunk",
		"choices": []any{
			map[string]any{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": finishReason,
			},
		},
	}
}
