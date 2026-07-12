package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lovelytoaster94/routerhub/internal/convert"
	"github.com/lovelytoaster94/routerhub/internal/protocol"
	"github.com/lovelytoaster94/routerhub/internal/storage"
)

// streamState drives cross-protocol SSE conversion with proper event framing,
// lifecycle events, tool-call fragment accumulation, and stream termination.
// It is stateful and NOT safe for concurrent use—one instance per stream.
type streamState struct {
	inbound   string // downstream protocol
	provider  string // upstream protocol
	requestID string
	model     string
	startTime time.Time
	logEntry  *storage.RequestLog

	// Synthesized IDs (kept stable throughout the stream).
	responseID  string // "resp_<8char>"
	messageID   string // "msg_<8char>"
	chatChunkID string // "chatcmpl-<8char>"

	seq            int  // sequence_number for Responses events
	preludeSent    bool // whether prelude events were sent
	textStarted    bool // whether text content block/item has been announced
	textStopped    bool
	textBuffer     strings.Builder
	closureSent    bool
	doneSent       bool // whether Chat's [DONE] was written
	firstTokenSent bool

	// Tool tracking. Key is upstream identifier:
	//   Chat upstream: strconv.Itoa(tool_calls[].index)
	//   Anthropic upstream: strconv.Itoa(content_block.index)
	//   Responses upstream: strconv.Itoa(output_index)
	tools       map[string]*toolCallState
	toolOrder   []string // preserves arrival order
	nextToolIdx int      // downstream index counter

	// Last known usage (populated by proxy.go via SetUsage).
	lastUsage *convert.StreamUsage

	// chatIncludeUsage controls whether the Chat downstream receives a usage
	// chunk. OpenAI only emits stream usage when the client requests it via
	// stream_options.include_usage; strict clients may reject an unexpected
	// usage block. It is only meaningful when inbound is Chat.
	chatIncludeUsage bool

	// preludeInputTokens captures the input token count advertised by an
	// upstream Anthropic message_start (or set from usage side-channel), so
	// the downstream Anthropic message_start can report a realistic value
	// instead of a zero placeholder.
	preludeInputTokens int64

	// Anthropic pending finish reason (stored from message_delta, used at message_stop).
	pendingFinishReason string

	// Reasoning/Thinking block lifecycle.
	reasoningStarted   bool
	reasoningStopped   bool
	reasoningBuffer    strings.Builder
	reasoningSignature string
	reasoningItemID    string // for Responses "rs_<8char>", derived from requestID
	blockIndexOffset   int    // 0 by default; 1 when reasoning occupies index 0 (for Anthropic/Responses downstream)
}

type toolCallState struct {
	upstreamKey     string
	downstreamIndex int
	id              string // "fc_<...>" (Responses) or upstream id
	callID          string // Responses call_id, may equal id
	name            string
	argsBuffer      strings.Builder
	started         bool
	stopped         bool
}

// newStreamState creates a new streamState for the given conversion.
func newStreamState(inbound, provider, requestID, model string, startTime time.Time, logEntry *storage.RequestLog) *streamState {
	shortID := strings.ReplaceAll(requestID, "-", "")
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	s := &streamState{
		inbound:     inbound,
		provider:    provider,
		requestID:   requestID,
		model:       model,
		startTime:   startTime,
		logEntry:    logEntry,
		responseID:  "resp_" + shortID,
		messageID:   "msg_" + shortID,
		chatChunkID: "chatcmpl-" + shortID,
		tools:       make(map[string]*toolCallState),
	}

	// downstreamIndex assignment depends on target protocol.
	switch inbound {
	case protocol.ProtocolChatCompletions:
		s.nextToolIdx = 0 // 0-based for Chat tool_calls[].index
	case protocol.ProtocolAnthropic:
		s.nextToolIdx = 1 // text block is index 0, tools start at 1
	case protocol.ProtocolResponses:
		s.nextToolIdx = 1 // text output_item is index 0, tools start at 1
	}

	// Reasoning item ID for Responses downstream (uses stable per-request prefix).
	if len(requestID) >= 8 {
		s.reasoningItemID = "rs_" + strings.ReplaceAll(requestID, "-", "")[:8]
	} else {
		s.reasoningItemID = "rs_" + requestID
	}

	return s
}

// SetUsage stores usage parsed by convert.ParseStreamUsage. Applied to closure events.
func (s *streamState) SetUsage(u *convert.StreamUsage) {
	s.lastUsage = u
}

// processUpstreamData parses a single upstream SSE data payload (JSON) and
// drives emit() to output 0..N downstream events. Called for every "data:" line
// (except "[DONE]"—that is handled by writeStreamEnd).
func (s *streamState) processUpstreamData(w http.ResponseWriter, flusher http.Flusher, data []byte) {
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return
	}

	switch s.provider {
	case protocol.ProtocolChatCompletions:
		s.handleUpstreamChat(w, flusher, event)
	case protocol.ProtocolAnthropic:
		s.handleUpstreamAnthropic(w, flusher, event)
	case protocol.ProtocolResponses:
		s.handleUpstreamResponses(w, flusher, event)
	}
}

// writeStreamEnd is called when the upstream scanner finishes (successfully or
// with error). It ensures closure events and Chat's [DONE] sentinel are emitted
// exactly once. Idempotent.
func (s *streamState) writeStreamEnd(w http.ResponseWriter, flusher http.Flusher) {
	if !s.closureSent {
		s.writeClosure(w, flusher, "stop")
	}
	s.writeChatDone(w, flusher)
}

// ---------------------------------------------------------------------------
// emit — write a single downstream SSE event
// ---------------------------------------------------------------------------

func (s *streamState) emit(w http.ResponseWriter, flusher http.Flusher, eventType string, payload map[string]any) {
	// Track first token time on first emit.
	if !s.firstTokenSent {
		ttft := time.Since(s.startTime).Milliseconds()
		s.logEntry.TimeToFirstTokenMs = &ttft
		s.firstTokenSent = true
	}

	// For Responses, inject sequence_number.
	if s.inbound == protocol.ProtocolResponses {
		payload["sequence_number"] = s.seq
		s.seq++
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	switch s.inbound {
	case protocol.ProtocolChatCompletions:
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(data))
	case protocol.ProtocolAnthropic, protocol.ProtocolResponses:
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(data))
	}

	if flusher != nil {
		flusher.Flush()
	}
}

// ---------------------------------------------------------------------------
// Prelude — lifecycle events sent before any content
// ---------------------------------------------------------------------------

func (s *streamState) writePrelude(w http.ResponseWriter, flusher http.Flusher) {
	if s.preludeSent {
		return
	}
	s.preludeSent = true

	switch s.inbound {
	case protocol.ProtocolAnthropic:
		s.emit(w, flusher, "message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":             s.messageID,
				"type":           "message",
				"role":           "assistant",
				"model":          s.model,
				"content":        []any{},
				"stop_reason":    nil,
				"stop_sequence":  nil,
				"usage": map[string]any{
					"input_tokens":  s.preludeInputTokens,
					"output_tokens": 0,
				},
			},
		})

	case protocol.ProtocolResponses:
		baseResp := map[string]any{
			"id":                 s.responseID,
			"object":             "response",
			"created_at":         s.startTime.Unix(),
			"status":             "in_progress",
			"model":              s.model,
			"output":             []any{},
			"parallel_tool_calls": true,
			"tool_choice":        "auto",
			"tools":              []any{},
		}
		s.emit(w, flusher, "response.created", map[string]any{
			"type":     "response.created",
			"response": baseResp,
		})
		s.emit(w, flusher, "response.in_progress", map[string]any{
			"type":     "response.in_progress",
			"response": baseResp,
		})
	}
}

// ---------------------------------------------------------------------------
// Reasoning/Thinking block lifecycle
// ---------------------------------------------------------------------------

func (s *streamState) ensureReasoningStarted(w http.ResponseWriter, flusher http.Flusher) {
	if s.reasoningStarted {
		return
	}
	s.writePrelude(w, flusher)
	s.reasoningStarted = true

	// For Anthropic/Responses downstream, reasoning occupies index 0,
	// shifting text to index 1 and tools to index 2+. Reasoning is expected
	// to appear before any tool call in a well-formed upstream stream.
	if s.inbound == protocol.ProtocolAnthropic || s.inbound == protocol.ProtocolResponses {
		s.blockIndexOffset = 1
		if len(s.tools) == 0 {
			s.nextToolIdx++
		}
	}

	switch s.inbound {
	case protocol.ProtocolChatCompletions:
		// Chat downstream: no separate start event; reasoning is just a delta field.

	case protocol.ProtocolAnthropic:
		s.emit(w, flusher, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type":     "thinking",
				"thinking": "",
			},
		})

	case protocol.ProtocolResponses:
		s.emit(w, flusher, "response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": 0,
			"item": map[string]any{
				"id":      s.reasoningItemID,
				"type":    "reasoning",
				"status":  "in_progress",
				"content": []any{},
			},
		})
		s.emit(w, flusher, "response.content_part.added", map[string]any{
			"type":          "response.content_part.added",
			"item_id":       s.reasoningItemID,
			"output_index":  0,
			"content_index": 0,
			"part": map[string]any{
				"type": "reasoning_text",
				"text": "",
			},
		})
	}
}

func (s *streamState) appendReasoningDelta(w http.ResponseWriter, flusher http.Flusher, text string) {
	if text == "" {
		return
	}
	s.ensureReasoningStarted(w, flusher)
	s.reasoningBuffer.WriteString(text)

	switch s.inbound {
	case protocol.ProtocolChatCompletions:
		s.emit(w, flusher, "", map[string]any{
			"id":      s.chatChunkID,
			"object":  "chat.completion.chunk",
			"created": s.startTime.Unix(),
			"model":   s.model,
			"choices": []any{
				map[string]any{
					"index":         0,
					"delta":         map[string]any{"reasoning_content": text},
					"finish_reason": nil,
				},
			},
		})

	case protocol.ProtocolAnthropic:
		s.emit(w, flusher, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type":     "thinking_delta",
				"thinking": text,
			},
		})

	case protocol.ProtocolResponses:
		s.emit(w, flusher, "response.reasoning_text.delta", map[string]any{
			"type":          "response.reasoning_text.delta",
			"item_id":       s.reasoningItemID,
			"output_index":  0,
			"content_index": 0,
			"delta":         text,
		})
	}
}

func (s *streamState) ensureReasoningStopped(w http.ResponseWriter, flusher http.Flusher) {
	if !s.reasoningStarted || s.reasoningStopped {
		return
	}
	s.reasoningStopped = true

	switch s.inbound {
	case protocol.ProtocolChatCompletions:
		// Chat downstream: no separate stop event.

	case protocol.ProtocolAnthropic:
		if s.reasoningSignature != "" {
			s.emit(w, flusher, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{
					"type":      "signature_delta",
					"signature": s.reasoningSignature,
				},
			})
		}
		s.emit(w, flusher, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})

	case protocol.ProtocolResponses:
		finalText := s.reasoningBuffer.String()
		s.emit(w, flusher, "response.reasoning_text.done", map[string]any{
			"type":          "response.reasoning_text.done",
			"item_id":       s.reasoningItemID,
			"output_index":  0,
			"content_index": 0,
			"text":          finalText,
		})
		s.emit(w, flusher, "response.content_part.done", map[string]any{
			"type":          "response.content_part.done",
			"item_id":       s.reasoningItemID,
			"output_index":  0,
			"content_index": 0,
			"part": map[string]any{
				"type": "reasoning_text",
				"text": finalText,
			},
		})
		doneItem := map[string]any{
			"id":   s.reasoningItemID,
			"type": "reasoning",
			"status": "completed",
			"content": []any{
				map[string]any{"type": "reasoning_text", "text": finalText},
			},
		}
		if s.reasoningSignature != "" {
			doneItem["encrypted_content"] = s.reasoningSignature
		}
		s.emit(w, flusher, "response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": 0,
			"item":         doneItem,
		})
	}
}

// ---------------------------------------------------------------------------
// Text block lifecycle
// ---------------------------------------------------------------------------

func (s *streamState) ensureTextStarted(w http.ResponseWriter, flusher http.Flusher) {
	if s.textStarted {
		return
	}
	s.ensureReasoningStopped(w, flusher)
	s.textStarted = true

	switch s.inbound {
	case protocol.ProtocolAnthropic:
		s.emit(w, flusher, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": s.blockIndexOffset,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		})

	case protocol.ProtocolResponses:
		s.emit(w, flusher, "response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": s.blockIndexOffset,
			"item": map[string]any{
				"id":      s.messageID,
				"type":    "message",
				"status":  "in_progress",
				"role":    "assistant",
				"content": []any{},
			},
		})
		s.emit(w, flusher, "response.content_part.added", map[string]any{
			"type":          "response.content_part.added",
			"output_index":  s.blockIndexOffset,
			"item_id":       s.messageID,
			"content_index": 0,
			"part": map[string]any{
				"type":        "output_text",
				"text":        "",
				"annotations": []any{},
			},
		})
	}
}

func (s *streamState) appendTextDelta(w http.ResponseWriter, flusher http.Flusher, text string) {
	s.writePrelude(w, flusher)
	s.ensureTextStarted(w, flusher)
	s.textBuffer.WriteString(text)

	switch s.inbound {
	case protocol.ProtocolChatCompletions:
		s.emit(w, flusher, "", map[string]any{
			"id":      s.chatChunkID,
			"object":  "chat.completion.chunk",
			"created": s.startTime.Unix(),
			"model":   s.model,
			"choices": []any{
				map[string]any{
					"index":         0,
					"delta":         map[string]any{"content": text},
					"finish_reason": nil,
				},
			},
		})

	case protocol.ProtocolAnthropic:
		s.emit(w, flusher, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": s.blockIndexOffset,
			"delta": map[string]any{
				"type": "text_delta",
				"text": text,
			},
		})

	case protocol.ProtocolResponses:
		s.emit(w, flusher, "response.output_text.delta", map[string]any{
			"type":          "response.output_text.delta",
			"output_index":  s.blockIndexOffset,
			"item_id":       s.messageID,
			"content_index": 0,
			"delta":         text,
			"logprobs":      []any{},
		})
	}
}

func (s *streamState) ensureTextStopped(w http.ResponseWriter, flusher http.Flusher) {
	if s.textStopped || !s.textStarted {
		return
	}
	s.textStopped = true
	fullText := s.textBuffer.String()

	switch s.inbound {
	case protocol.ProtocolAnthropic:
		s.emit(w, flusher, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": s.blockIndexOffset,
		})

	case protocol.ProtocolResponses:
		s.emit(w, flusher, "response.output_text.done", map[string]any{
			"type":          "response.output_text.done",
			"output_index":  s.blockIndexOffset,
			"item_id":       s.messageID,
			"content_index": 0,
			"text":          fullText,
			"logprobs":      []any{},
		})
		s.emit(w, flusher, "response.content_part.done", map[string]any{
			"type":          "response.content_part.done",
			"output_index":  s.blockIndexOffset,
			"item_id":       s.messageID,
			"content_index": 0,
			"part": map[string]any{
				"type":        "output_text",
				"text":        fullText,
				"annotations": []any{},
			},
		})
		s.emit(w, flusher, "response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": s.blockIndexOffset,
			"item": map[string]any{
				"id":      s.messageID,
				"type":    "message",
				"status":  "completed",
				"role":    "assistant",
				"content": []any{},
			},
		})
	}
}

// ---------------------------------------------------------------------------
// Tool call lifecycle
// ---------------------------------------------------------------------------

// ensureToolStarted ensures a tool call block is started. Returns the tool state.
// If name is empty, the start event is deferred until name is set.
func (s *streamState) ensureToolStarted(w http.ResponseWriter, flusher http.Flusher, upstreamKey, id, name, callID string) *toolCallState {
	s.ensureReasoningStopped(w, flusher)
	tc, exists := s.tools[upstreamKey]
	if !exists {
		downstreamIdx := s.nextToolIdx
		s.nextToolIdx++

		// Synthesize ID if not provided.
		if id == "" {
			shortID := strings.ReplaceAll(s.requestID, "-", "")
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			id = "call_" + shortID + "_" + strconv.Itoa(downstreamIdx)
		}
		if callID == "" {
			callID = id
		}

		tc = &toolCallState{
			upstreamKey:     upstreamKey,
			downstreamIndex: downstreamIdx,
			id:              id,
			callID:          callID,
		}
		s.tools[upstreamKey] = tc
		s.toolOrder = append(s.toolOrder, upstreamKey)
	}

	// Update name if provided.
	if name != "" {
		tc.name = name
	}

	// Emit start event only if name is known and not already started.
	if tc.name != "" && !tc.started {
		tc.started = true
		s.writePrelude(w, flusher)

		switch s.inbound {
		case protocol.ProtocolChatCompletions:
			toolEntry := map[string]any{
				"index": tc.downstreamIndex,
				"id":    tc.id,
				"type":  "function",
				"function": map[string]any{
					"name":      tc.name,
					"arguments": tc.argsBuffer.String(),
				},
			}
			s.emit(w, flusher, "", map[string]any{
				"id":      s.chatChunkID,
				"object":  "chat.completion.chunk",
				"created": s.startTime.Unix(),
				"model":   s.model,
				"choices": []any{
					map[string]any{
						"index":         0,
						"delta":         map[string]any{"tool_calls": []any{toolEntry}},
						"finish_reason": nil,
					},
				},
			})

		case protocol.ProtocolAnthropic:
			s.emit(w, flusher, "content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": tc.downstreamIndex,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    tc.id,
					"name":  tc.name,
					"input": map[string]any{},
				},
			})

		case protocol.ProtocolResponses:
			// Build a stable fc_ id for Responses.
			shortID := strings.ReplaceAll(s.requestID, "-", "")
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			fcID := "fc_" + strconv.Itoa(tc.downstreamIndex) + "_" + shortID

			s.emit(w, flusher, "response.output_item.added", map[string]any{
				"type":         "response.output_item.added",
				"output_index": tc.downstreamIndex,
				"item": map[string]any{
					"id":        fcID,
					"type":      "function_call",
					"status":    "in_progress",
					"call_id":   tc.callID,
					"name":      tc.name,
					"arguments": tc.argsBuffer.String(),
				},
			})
		}
	}

	return tc
}

func (s *streamState) appendToolArgs(w http.ResponseWriter, flusher http.Flusher, upstreamKey, fragment string) {
	tc := s.tools[upstreamKey]
	if tc == nil {
		// Tool not yet announced; create a placeholder.
		tc = s.ensureToolStarted(w, flusher, upstreamKey, "", "", "")
	}

	tc.argsBuffer.WriteString(fragment)

	// If the tool hasn't been started yet (name missing), the fragment is
	// buffered and will be emitted when start happens.
	if !tc.started {
		return
	}

	switch s.inbound {
	case protocol.ProtocolChatCompletions:
		toolEntry := map[string]any{
			"index": tc.downstreamIndex,
			"function": map[string]any{
				"arguments": fragment,
			},
		}
		s.emit(w, flusher, "", map[string]any{
			"id":      s.chatChunkID,
			"object":  "chat.completion.chunk",
			"created": s.startTime.Unix(),
			"model":   s.model,
			"choices": []any{
				map[string]any{
					"index":         0,
					"delta":         map[string]any{"tool_calls": []any{toolEntry}},
					"finish_reason": nil,
				},
			},
		})

	case protocol.ProtocolAnthropic:
		s.emit(w, flusher, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": tc.downstreamIndex,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": fragment,
			},
		})

	case protocol.ProtocolResponses:
		shortID := strings.ReplaceAll(s.requestID, "-", "")
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		fcID := "fc_" + strconv.Itoa(tc.downstreamIndex) + "_" + shortID

		s.emit(w, flusher, "response.function_call_arguments.delta", map[string]any{
			"type":         "response.function_call_arguments.delta",
			"output_index": tc.downstreamIndex,
			"item_id":      fcID,
			"delta":        fragment,
		})
	}
}

func (s *streamState) ensureToolStopped(w http.ResponseWriter, flusher http.Flusher, key string) {
	tc := s.tools[key]
	if tc == nil || tc.stopped {
		return
	}
	tc.stopped = true

	// Ensure tool is started (name must be known to emit stop).
	if !tc.started {
		return
	}

	fullArgs := tc.argsBuffer.String()

	switch s.inbound {
	case protocol.ProtocolAnthropic:
		s.emit(w, flusher, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": tc.downstreamIndex,
		})

	case protocol.ProtocolResponses:
		shortID := strings.ReplaceAll(s.requestID, "-", "")
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		fcID := "fc_" + strconv.Itoa(tc.downstreamIndex) + "_" + shortID

		s.emit(w, flusher, "response.function_call_arguments.done", map[string]any{
			"type":         "response.function_call_arguments.done",
			"output_index": tc.downstreamIndex,
			"item_id":      fcID,
			"name":         tc.name,
			"arguments":    fullArgs,
		})
		s.emit(w, flusher, "response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": tc.downstreamIndex,
			"item": map[string]any{
				"id":        fcID,
				"type":      "function_call",
				"status":    "completed",
				"call_id":   tc.callID,
				"name":      tc.name,
				"arguments": fullArgs,
			},
		})
	}
}

// ---------------------------------------------------------------------------
// Closure — stream termination
// ---------------------------------------------------------------------------

func (s *streamState) writeClosure(w http.ResponseWriter, flusher http.Flusher, finishReason string) {
	if s.closureSent {
		return
	}
	s.closureSent = true

	// Ensure prelude is sent before closure events.
	s.writePrelude(w, flusher)

	// Close any open reasoning/text/tool blocks.
	s.ensureReasoningStopped(w, flusher)
	if s.textStarted && !s.textStopped {
		s.ensureTextStopped(w, flusher)
	}
	for _, key := range s.toolOrder {
		if tc := s.tools[key]; tc != nil && !tc.stopped {
			s.ensureToolStopped(w, flusher, key)
		}
	}

	switch s.inbound {
	case protocol.ProtocolChatCompletions:
		mappedReason := finishReason
		s.emit(w, flusher, "", map[string]any{
			"id":      s.chatChunkID,
			"object":  "chat.completion.chunk",
			"created": s.startTime.Unix(),
			"model":   s.model,
			"choices": []any{
				map[string]any{
					"index":         0,
					"delta":         map[string]any{},
					"finish_reason": mappedReason,
				},
			},
		})

		// Inject usage as a final, OpenAI-conventional chunk: empty delta,
		// null finish_reason, and the usage at the top level. OpenAI only
		// reports stream usage when the client explicitly opts in via
		// stream_options.include_usage, so honour that to stay compatible with
		// strict clients. anthropic/responses downstreams always include usage
		// (no opt-in exists), so they are unaffected by this flag.
		if s.lastUsage != nil && s.chatIncludeUsage {
			total := s.lastUsage.TotalTokens
			if total == 0 {
				total = s.lastUsage.InputTokens + s.lastUsage.OutputTokens
			}
			usageMap := map[string]any{
				"prompt_tokens":     s.lastUsage.InputTokens,
				"completion_tokens": s.lastUsage.OutputTokens,
				"total_tokens":      total,
			}
			if s.lastUsage.CachedTokens > 0 {
				usageMap["prompt_tokens_details"] = map[string]any{
					"cached_tokens": s.lastUsage.CachedTokens,
				}
			}
			s.emit(w, flusher, "", map[string]any{
				"id":      s.chatChunkID,
				"object":  "chat.completion.chunk",
				"created": s.startTime.Unix(),
				"model":   s.model,
				"choices": []any{
					map[string]any{
						"index":         0,
						"delta":         map[string]any{},
						"finish_reason": nil,
					},
				},
				"usage": usageMap,
			})
		}

	case protocol.ProtocolAnthropic:
		stopReason := "end_turn"
		switch finishReason {
		case "stop":
			stopReason = "end_turn"
		case "length":
			stopReason = "max_tokens"
		case "tool_calls":
			stopReason = "tool_use"
		}

		usageMap := map[string]any{
			"input_tokens":  0,
			"output_tokens": 0,
		}
		if s.lastUsage != nil {
			// Anthropic reports input_tokens as the NOT-cached portion;
			// cache_read_input_tokens / cache_creation_input_tokens are
			// separate counters that, together with input_tokens, sum to the
			// total input. Our StreamUsage.InputTokens is the gateway-normalised
			// TOTAL (includes cache), so derive the raw uncached input here to
			// avoid double-counting cache tokens.
			rawInput := s.lastUsage.InputTokens - s.lastUsage.CachedTokens - s.lastUsage.CacheWriteTokens
			if rawInput < 0 {
				rawInput = 0
			}
			usageMap = map[string]any{
				"input_tokens":  rawInput,
				"output_tokens": s.lastUsage.OutputTokens,
			}
			if s.lastUsage.CachedTokens > 0 {
				usageMap["cache_read_input_tokens"] = s.lastUsage.CachedTokens
			}
			if s.lastUsage.CacheWriteTokens > 0 {
				usageMap["cache_creation_input_tokens"] = s.lastUsage.CacheWriteTokens
			}
		}

		s.emit(w, flusher, "message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   stopReason,
				"stop_sequence": nil,
			},
			"usage": usageMap,
		})
		s.emit(w, flusher, "message_stop", map[string]any{
			"type": "message_stop",
		})

	case protocol.ProtocolResponses:
		var status string
		switch finishReason {
		case "length":
			status = "incomplete"
		case "stop", "tool_calls":
			status = "completed"
		default:
			status = "completed"
		}
		output := s.buildOutputItems(status)
		resp := map[string]any{
			"id":                 s.responseID,
			"object":             "response",
			"created_at":         s.startTime.Unix(),
			"status":             status,
			"model":              s.model,
			"output":             output,
			"parallel_tool_calls": true,
			"tool_choice":        "auto",
			"tools":              []any{},
		}
		if s.lastUsage != nil {
			usageMap := map[string]any{
				"input_tokens":  s.lastUsage.InputTokens,
				"output_tokens": s.lastUsage.OutputTokens,
				"total_tokens":  s.lastUsage.TotalTokens,
			}
			if s.lastUsage.TotalTokens == 0 {
				usageMap["total_tokens"] = s.lastUsage.InputTokens + s.lastUsage.OutputTokens
			}
			if s.lastUsage.CachedTokens > 0 {
				usageMap["input_tokens_details"] = map[string]any{
					"cached_tokens": s.lastUsage.CachedTokens,
				}
			}
			resp["usage"] = usageMap
		}
		s.emit(w, flusher, "response.completed", map[string]any{
			"type":     "response.completed",
			"response": resp,
		})
	}
}

func (s *streamState) writeChatDone(w http.ResponseWriter, flusher http.Flusher) {
	if s.doneSent {
		return
	}
	s.doneSent = true

	if s.inbound == protocol.ProtocolChatCompletions {
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// buildOutputItems builds the final output array for Responses response.completed.
// The status parameter controls the message item's status (e.g. "completed" or "incomplete").
func (s *streamState) buildOutputItems(status string) []any {
	var items []any

	if s.reasoningStarted {
		reasoningItem := map[string]any{
			"id":   s.reasoningItemID,
			"type": "reasoning",
			"status": status,
			"content": []any{
				map[string]any{"type": "reasoning_text", "text": s.reasoningBuffer.String()},
			},
		}
		if s.reasoningSignature != "" {
			reasoningItem["encrypted_content"] = s.reasoningSignature
		}
		items = append(items, reasoningItem)
	}

	if s.textStarted {
		fullText := s.textBuffer.String()
		msgItem := map[string]any{
			"id":     s.messageID,
			"type":   "message",
			"status": status,
			"role":   "assistant",
			"content": []any{
				map[string]any{
					"type":        "output_text",
					"text":        fullText,
					"annotations": []any{},
				},
			},
		}
		items = append(items, msgItem)
	}

	for _, key := range s.toolOrder {
		tc := s.tools[key]
		if tc == nil {
			continue
		}
		// Skip tools that never received a name or were never started.
		if tc.name == "" || !tc.started {
			continue
		}
		fcItem := map[string]any{
			"id":        tc.id,
			"type":      "function_call",
			"status":    "completed",
			"call_id":   tc.callID,
			"name":      tc.name,
			"arguments": tc.argsBuffer.String(),
		}
		items = append(items, fcItem)
	}

	return items
}

// ---------------------------------------------------------------------------
// Upstream event handlers
// ---------------------------------------------------------------------------

// handleUpstreamChat processes an upstream Chat SSE data event.
func (s *streamState) handleUpstreamChat(w http.ResponseWriter, flusher http.Flusher, event map[string]any) {
	choices := getSlice(event, "choices")
	if len(choices) == 0 {
		return
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return
	}

	delta := getMap(choice, "delta")
	if delta == nil {
		return
	}

	finishReason := getString(choice, "finish_reason")

	// Reasoning content delta (e.g. DeepSeek R1).
	reasoning := getString(delta, "reasoning_content")
	if reasoning != "" {
		s.appendReasoningDelta(w, flusher, reasoning)
	}

	// Text delta.
	content := getString(delta, "content")
	if content != "" {
		s.ensureReasoningStopped(w, flusher)
		s.appendTextDelta(w, flusher, content)
	}

	// Tool calls.
	if toolCalls := getSlice(delta, "tool_calls"); len(toolCalls) > 0 {
		for _, tc := range toolCalls {
			tcMap, _ := tc.(map[string]any)
			if tcMap == nil {
				continue
			}
			upstreamKey := strconv.Itoa(int(getFloat64(tcMap, "index")))
			id := getString(tcMap, "id")
			function := getMap(tcMap, "function")

			if function != nil {
				name := getString(function, "name")
				args := getString(function, "arguments")

				if name != "" {
					s.writePrelude(w, flusher)
					_ = s.ensureToolStarted(w, flusher, upstreamKey, id, name, id)
				}
				if args != "" {
					s.appendToolArgs(w, flusher, upstreamKey, args)
				}
			}
		}
	}

	// Finish reason.
	if finishReason != "" {
		s.writeClosure(w, flusher, finishReason)
	}
}

// handleUpstreamAnthropic processes an upstream Anthropic SSE data event.
func (s *streamState) handleUpstreamAnthropic(w http.ResponseWriter, flusher http.Flusher, event map[string]any) {
	eventType := getString(event, "type")

	switch eventType {
	case "message_start":
		// Capture the input token count from the upstream message_start so
		// the downstream Anthropic message_start can report a realistic value.
		if msg := getMap(event, "message"); msg != nil {
			if u := getMap(msg, "usage"); u != nil {
				if v := int64(getFloat64(u, "input_tokens")); v > 0 {
					s.preludeInputTokens = v
				}
			}
		}
		s.writePrelude(w, flusher)

	case "content_block_start":
		index := int(getFloat64(event, "index"))
		contentBlock := getMap(event, "content_block")
		if contentBlock == nil {
			return
		}
		blockType := getString(contentBlock, "type")

		switch blockType {
		case "text":
			s.ensureTextStarted(w, flusher)

		case "thinking":
			s.ensureReasoningStarted(w, flusher)

		case "tool_use":
			id := getString(contentBlock, "id")
			name := getString(contentBlock, "name")
			key := strconv.Itoa(index)
			s.writePrelude(w, flusher)
			_ = s.ensureToolStarted(w, flusher, key, id, name, id)

			// Anthropic may include complete input in content_block_start.
			if input := getMap(contentBlock, "input"); input != nil {
				inputJSON, err := json.Marshal(input)
				if err == nil && string(inputJSON) != "{}" {
					s.appendToolArgs(w, flusher, key, string(inputJSON))
				}
			}
		}

	case "content_block_delta":
		index := int(getFloat64(event, "index"))
		delta := getMap(event, "delta")
		if delta == nil {
			return
		}
		deltaType := getString(delta, "type")

		switch deltaType {
		case "text_delta":
			text := getString(delta, "text")
			if text != "" {
				s.appendTextDelta(w, flusher, text)
			}

		case "thinking_delta":
			if t := getString(delta, "thinking"); t != "" {
				s.appendReasoningDelta(w, flusher, t)
			}

		case "signature_delta":
			if sig := getString(delta, "signature"); sig != "" {
				s.reasoningSignature = sig
			}

		case "input_json_delta":
			partialJSON := getString(delta, "partial_json")
			if partialJSON != "" {
				key := strconv.Itoa(index)
				s.appendToolArgs(w, flusher, key, partialJSON)
			}
		}

	case "content_block_stop":
		index := int(getFloat64(event, "index"))
		key := strconv.Itoa(index)
		switch {
		case s.reasoningStarted && !s.reasoningStopped && s.tools[key] == nil:
			// Upstream is closing the thinking block (still in reasoning phase, and no tool at this index).
			s.ensureReasoningStopped(w, flusher)
		case s.tools[key] != nil:
			s.ensureToolStopped(w, flusher, key)
		default:
			// Anything else is treated as the text block close.
			s.ensureTextStopped(w, flusher)
		}

	case "message_delta":
		delta := getMap(event, "delta")
		if delta == nil {
			return
		}
		stopReason := getString(delta, "stop_reason")
		if stopReason != "" {
			mapped := "stop"
			switch stopReason {
			case "end_turn":
				mapped = "stop"
			case "max_tokens":
				mapped = "length"
			case "tool_use":
				mapped = "tool_calls"
			}
			s.pendingFinishReason = mapped
		}

	case "message_stop":
		reason := s.pendingFinishReason
		if reason == "" {
			reason = "stop"
		}
		s.writeClosure(w, flusher, reason)

	case "ping":
		// Ignore keep-alive pings.
	}
}

// handleUpstreamResponses processes an upstream Responses SSE data event.
func (s *streamState) handleUpstreamResponses(w http.ResponseWriter, flusher http.Flusher, event map[string]any) {
	eventType := getString(event, "type")

	switch eventType {
	case "response.created", "response.in_progress":
		s.writePrelude(w, flusher)

	case "response.output_item.added":
		item := getMap(event, "item")
		if item == nil {
			return
		}
		itemType := getString(item, "type")
		if itemType == "function_call" {
			outputIndex := int(getFloat64(event, "output_index"))
			key := strconv.Itoa(outputIndex)
			id := getString(item, "id")
			name := getString(item, "name")
			callID := getString(item, "call_id")
			s.writePrelude(w, flusher)
			_ = s.ensureToolStarted(w, flusher, key, id, name, callID)
		} else if itemType == "reasoning" {
			if id := getString(item, "id"); id != "" {
				s.reasoningItemID = id
			}
			s.ensureReasoningStarted(w, flusher)
			return
		}
		// "message" type is ignored; we manage text lazily.

	case "response.content_part.added", "response.content_part.done":
		// Ignored; managed internally.

	case "response.output_text.delta":
		delta := getString(event, "delta")
		if delta != "" {
			s.appendTextDelta(w, flusher, delta)
		}

	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		if d := getString(event, "delta"); d != "" {
			s.appendReasoningDelta(w, flusher, d)
		}

	case "response.reasoning_text.done", "response.reasoning_summary_text.done":
		s.ensureReasoningStopped(w, flusher)

	case "response.output_text.done":
		s.ensureTextStopped(w, flusher)

	case "response.function_call_arguments.delta":
		outputIndex := int(getFloat64(event, "output_index"))
		delta := getString(event, "delta")
		if delta != "" {
			key := strconv.Itoa(outputIndex)
			s.appendToolArgs(w, flusher, key, delta)
		}

	case "response.function_call_arguments.done":
		outputIndex := int(getFloat64(event, "output_index"))
		key := strconv.Itoa(outputIndex)
		s.ensureToolStopped(w, flusher, key)

	case "response.output_item.done":
		item := getMap(event, "item")
		if item == nil {
			return
		}
		itemType := getString(item, "type")
		if itemType == "function_call" {
			outputIndex := int(getFloat64(event, "output_index"))
			key := strconv.Itoa(outputIndex)
			s.ensureToolStopped(w, flusher, key)
		} else if itemType == "reasoning" {
			s.ensureReasoningStopped(w, flusher)
		}

	case "response.completed":
		response := getMap(event, "response")
		if response != nil {
			if usage := getMap(response, "usage"); usage != nil {
				u := &convert.StreamUsage{
					InputTokens:  int64(getFloat64(usage, "input_tokens")),
					OutputTokens: int64(getFloat64(usage, "output_tokens")),
					TotalTokens:  int64(getFloat64(usage, "total_tokens")),
				}
				s.SetUsage(u)
			}
		}
		// If any tool call was seen during this stream, prefer "tool_calls" so
		// Chat clients wire up the tool invocation on their side. Falls back to
		// "stop" for plain text completions.
		finishReason := "stop"
		if len(s.toolOrder) > 0 {
			finishReason = "tool_calls"
		}
		s.writeClosure(w, flusher, finishReason)

	case "response.incomplete", "response.failed":
		s.writeClosure(w, flusher, "length")

	case "response.error":
		s.writeClosure(w, flusher, "stop")
	}
}

// ---------------------------------------------------------------------------
// JSON helpers (mirrored from internal/convert to avoid export changes)
// ---------------------------------------------------------------------------

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
