package convert

// parseChatStreamUsage attempts to extract token usage from OpenAI chat stream events.
// Usage may appear in the final chunk or in a separate event.
func parseChatStreamUsage(event map[string]any) *StreamUsage {
	// Check for usage field directly on the event (some implementations include it)
	if usage := getMap(event, "usage"); usage != nil {
		u := &StreamUsage{}
		if v := getFloat64(usage, "prompt_tokens"); v > 0 {
			u.InputTokens = int64(v)
		}
		if v := getFloat64(usage, "completion_tokens"); v > 0 {
			u.OutputTokens = int64(v)
		}
		if v := getFloat64(usage, "total_tokens"); v > 0 {
			u.TotalTokens = int64(v)
		}
		if u.InputTokens > 0 || u.OutputTokens > 0 {
			if u.TotalTokens == 0 {
				u.TotalTokens = u.InputTokens + u.OutputTokens
			}
			return u
		}
	}

	// Some implementations put usage inside choices[0]
	if choices := getSlice(event, "choices"); len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if usage := getMap(choice, "usage"); usage != nil {
				u := &StreamUsage{}
				if v := getFloat64(usage, "prompt_tokens"); v > 0 {
					u.InputTokens = int64(v)
				}
				if v := getFloat64(usage, "completion_tokens"); v > 0 {
					u.OutputTokens = int64(v)
				}
				if v := getFloat64(usage, "total_tokens"); v > 0 {
					u.TotalTokens = int64(v)
				}
				if u.InputTokens > 0 || u.OutputTokens > 0 {
					if u.TotalTokens == 0 {
						u.TotalTokens = u.InputTokens + u.OutputTokens
					}
					return u
				}
			}
		}
	}

	return nil
}

// parseResponsesStreamUsage attempts to extract token usage from OpenAI Responses stream events.
// Usage typically appears in the response.completed event.
func parseResponsesStreamUsage(event map[string]any) *StreamUsage {
	eventType := getString(event, "type")

	// Usage may be in response.completed
	if eventType == "response.completed" {
		if response := getMap(event, "response"); response != nil {
			if usage := getMap(response, "usage"); usage != nil {
				u := &StreamUsage{}
				if v := getFloat64(usage, "input_tokens"); v > 0 {
					u.InputTokens = int64(v)
				}
				if v := getFloat64(usage, "output_tokens"); v > 0 {
					u.OutputTokens = int64(v)
				}
				if v := getFloat64(usage, "total_tokens"); v > 0 {
					u.TotalTokens = int64(v)
				}
				if u.InputTokens > 0 || u.OutputTokens > 0 {
					if u.TotalTokens == 0 {
						u.TotalTokens = u.InputTokens + u.OutputTokens
					}
					return u
				}
			}
		}
	}

	// Some implementations may include usage in other events
	if usage := getMap(event, "usage"); usage != nil {
		u := &StreamUsage{}
		if v := getFloat64(usage, "input_tokens"); v > 0 {
			u.InputTokens = int64(v)
		}
		if v := getFloat64(usage, "output_tokens"); v > 0 {
			u.OutputTokens = int64(v)
		}
		if v := getFloat64(usage, "total_tokens"); v > 0 {
			u.TotalTokens = int64(v)
		}
		if u.InputTokens > 0 || u.OutputTokens > 0 {
			if u.TotalTokens == 0 {
				u.TotalTokens = u.InputTokens + u.OutputTokens
			}
			return u
		}
	}

	return nil
}

// parseAnthropicStreamUsage attempts to extract token usage from Anthropic Messages stream events.
// Usage appears in message_delta event.
func parseAnthropicStreamUsage(event map[string]any) *StreamUsage {
	eventType := getString(event, "type")

	if eventType == "message_delta" {
		if usage := getMap(event, "usage"); usage != nil {
			u := &StreamUsage{}
			if v := getFloat64(usage, "input_tokens"); v > 0 {
				u.InputTokens = int64(v)
			}
			if v := getFloat64(usage, "output_tokens"); v > 0 {
				u.OutputTokens = int64(v)
			}
			if u.InputTokens > 0 || u.OutputTokens > 0 {
				u.TotalTokens = u.InputTokens + u.OutputTokens
				return u
			}
		}
	}

	// Also check message_start for initial token counts
	if eventType == "message_start" {
		if message := getMap(event, "message"); message != nil {
			if usage := getMap(message, "usage"); usage != nil {
				u := &StreamUsage{}
				if v := getFloat64(usage, "input_tokens"); v > 0 {
					u.InputTokens = int64(v)
				}
				if v := getFloat64(usage, "output_tokens"); v > 0 {
					u.OutputTokens = int64(v)
				}
				if u.InputTokens > 0 || u.OutputTokens > 0 {
					u.TotalTokens = u.InputTokens + u.OutputTokens
					return u
				}
			}
		}
	}

	return nil
}
