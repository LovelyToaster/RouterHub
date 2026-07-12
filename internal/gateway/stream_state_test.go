package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lovelytoaster94/routerhub/internal/convert"
	"github.com/lovelytoaster94/routerhub/internal/protocol"
	"github.com/lovelytoaster94/routerhub/internal/storage"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type sseEvent struct {
	Event string          // event type (empty for Chat target)
	Data  json.RawMessage // raw JSON payload, or "[DONE]" literal
}

// parseSSE splits a raw SSE body into individual events.
func parseSSE(body string) []sseEvent {
	var events []sseEvent
	blocks := strings.Split(body, "\n\n")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var ev sseEvent
		lines := strings.Split(block, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "event:") {
				ev.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				ev.Data = json.RawMessage(dataStr)
			}
		}
		if len(ev.Data) > 0 {
			events = append(events, ev)
		}
	}
	return events
}

// mustUnmarshal unmarshals JSON and calls t.Fatal on error.
func mustUnmarshal(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

// flushRecorder wraps httptest.ResponseRecorder to implement http.Flusher.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

// fixedStart is a deterministic timestamp used across tests.
var fixedStart = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// testLogEntry returns a minimal RequestLog for testing.
func testLogEntry() *storage.RequestLog {
	return &storage.RequestLog{
		RequestID: "test-req-abcdef12",
	}
}

// ---------------------------------------------------------------------------
// Test 1: Chat → Responses, text only
// ---------------------------------------------------------------------------

func TestStreamState_ChatToResponses_TextOnly(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolResponses,
		protocol.ProtocolChatCompletions,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	// Input 1: first text delta
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
	))

	// Input 2: second text delta
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
	))

	// Set usage before the finish chunk (simulating proxy.go behaviour).
	state.SetUsage(&convert.StreamUsage{
		InputTokens:  10,
		OutputTokens: 2,
		TotalTokens:  12,
	})


	// Input 3: finish chunk with usage
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect exactly 10 events (0..9).
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d\nbody:\n%s", len(events), body)
	}

	// Helper to check sequence_number.
	seqOf := func(idx int) float64 {
		var p map[string]any
		if err := json.Unmarshal(events[idx].Data, &p); err != nil {
			t.Fatalf("event[%d] parse: %v", idx, err)
		}
		sn, _ := p["sequence_number"].(float64)
		return sn
	}

	// Verify sequence_number is strictly 0..9.
	for i := 0; i < 10; i++ {
		if got := seqOf(i); got != float64(i) {
			t.Errorf("event[%d] sequence_number: expected %d, got %v", i, i, got)
		}
	}

	// --- event[0]: response.created ---
	if events[0].Event != "response.created" {
		t.Errorf("event[0]: expected response.created, got %q", events[0].Event)
	}
	var e0 map[string]any
	mustUnmarshal(t, events[0].Data, &e0)
	if e0["type"] != "response.created" {
		t.Errorf("event[0].type: expected response.created, got %v", e0["type"])
	}
	resp0, _ := e0["response"].(map[string]any)
	if resp0 == nil {
		t.Fatal("event[0].response is nil")
	}
	if resp0["id"] != "resp_testreqa" {
		t.Errorf("event[0].response.id: expected resp_testreqa, got %v", resp0["id"])
	}
	if resp0["status"] != "in_progress" {
		t.Errorf("event[0].response.status: expected in_progress, got %v", resp0["status"])
	}

	// --- event[1]: response.in_progress ---
	if events[1].Event != "response.in_progress" {
		t.Errorf("event[1]: expected response.in_progress, got %q", events[1].Event)
	}

	// --- event[2]: response.output_item.added ---
	if events[2].Event != "response.output_item.added" {
		t.Errorf("event[2]: expected response.output_item.added, got %q", events[2].Event)
	}
	var e2 map[string]any
	mustUnmarshal(t, events[2].Data, &e2)
	if e2["output_index"] != float64(0) {
		t.Errorf("event[2].output_index: expected 0, got %v", e2["output_index"])
	}
	item2, _ := e2["item"].(map[string]any)
	if item2["type"] != "message" {
		t.Errorf("event[2].item.type: expected message, got %v", item2["type"])
	}
	if item2["status"] != "in_progress" {
		t.Errorf("event[2].item.status: expected in_progress, got %v", item2["status"])
	}

	// --- event[3]: response.content_part.added ---
	if events[3].Event != "response.content_part.added" {
		t.Errorf("event[3]: expected response.content_part.added, got %q", events[3].Event)
	}

	// --- event[4]: response.output_text.delta (Hello) ---
	if events[4].Event != "response.output_text.delta" {
		t.Errorf("event[4]: expected response.output_text.delta, got %q", events[4].Event)
	}
	var e4 map[string]any
	mustUnmarshal(t, events[4].Data, &e4)
	if e4["delta"] != "Hello" {
		t.Errorf("event[4].delta: expected Hello, got %v", e4["delta"])
	}

	// --- event[5]: response.output_text.delta ( world) ---
	if events[5].Event != "response.output_text.delta" {
		t.Errorf("event[5]: expected response.output_text.delta, got %q", events[5].Event)
	}
	var e5 map[string]any
	mustUnmarshal(t, events[5].Data, &e5)
	if e5["delta"] != " world" {
		t.Errorf("event[5].delta: expected ' world', got %v", e5["delta"])
	}

	// --- event[6]: response.output_text.done ---
	if events[6].Event != "response.output_text.done" {
		t.Errorf("event[6]: expected response.output_text.done, got %q", events[6].Event)
	}
	var e6 map[string]any
	mustUnmarshal(t, events[6].Data, &e6)
	if e6["text"] != "Hello world" {
		t.Errorf("event[6].text: expected 'Hello world', got %v", e6["text"])
	}

	// --- event[7]: response.content_part.done ---
	if events[7].Event != "response.content_part.done" {
		t.Errorf("event[7]: expected response.content_part.done, got %q", events[7].Event)
	}

	// --- event[8]: response.output_item.done ---
	if events[8].Event != "response.output_item.done" {
		t.Errorf("event[8]: expected response.output_item.done, got %q", events[8].Event)
	}
	var e8 map[string]any
	mustUnmarshal(t, events[8].Data, &e8)
	item8, _ := e8["item"].(map[string]any)
	if item8["status"] != "completed" {
		t.Errorf("event[8].item.status: expected completed, got %v", item8["status"])
	}

	// --- event[9]: response.completed ---
	if events[9].Event != "response.completed" {
		t.Errorf("event[9]: expected response.completed, got %q", events[9].Event)
	}
	var e9 map[string]any
	mustUnmarshal(t, events[9].Data, &e9)
	if e9["type"] != "response.completed" {
		t.Errorf("event[9].type: expected response.completed, got %v", e9["type"])
	}
	resp9, _ := e9["response"].(map[string]any)
	if resp9 == nil {
		t.Fatal("event[9].response is nil")
	}
	if resp9["status"] != "completed" {
		t.Errorf("event[9].response.status: expected completed, got %v", resp9["status"])
	}
	usage9, _ := resp9["usage"].(map[string]any)
	if usage9 == nil {
		t.Fatal("event[9].response.usage is nil")
	}
	if usage9["input_tokens"] != float64(10) {
		t.Errorf("event[9].usage.input_tokens: expected 10, got %v", usage9["input_tokens"])
	}
	if usage9["output_tokens"] != float64(2) {
		t.Errorf("event[9].usage.output_tokens: expected 2, got %v", usage9["output_tokens"])
	}

	// Verify output contains the message item.
	output9, _ := resp9["output"].([]any)
	if len(output9) != 1 {
		t.Fatalf("event[9].response.output: expected 1 item, got %d", len(output9))
	}
	msg9, _ := output9[0].(map[string]any)
	if msg9["type"] != "message" {
		t.Errorf("event[9].output[0].type: expected message, got %v", msg9["type"])
	}
	content9, _ := msg9["content"].([]any)
	if len(content9) != 1 {
		t.Fatalf("event[9].output[0].content: expected 1 item, got %d", len(content9))
	}
	text9, _ := content9[0].(map[string]any)
	if text9["text"] != "Hello world" {
		t.Errorf("event[9].output[0].content[0].text: expected 'Hello world', got %v", text9["text"])
	}

	// Verify first token was recorded.
	if logEntry.TimeToFirstTokenMs == nil {
		t.Error("TimeToFirstTokenMs should be set after first emit")
	}
}

// ---------------------------------------------------------------------------
// Test 2: Chat → Anthropic, text only
// ---------------------------------------------------------------------------

func TestStreamState_ChatToAnthropic_TextOnly(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolAnthropic,
		protocol.ProtocolChatCompletions,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	// Input 1: text delta
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`,
	))

	// Input 2: finish chunk
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect exactly 6 events.
	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d\nbody:\n%s", len(events), body)
	}

	// --- event[0]: message_start ---
	if events[0].Event != "message_start" {
		t.Errorf("event[0]: expected message_start, got %q", events[0].Event)
	}
	var e0 map[string]any
	mustUnmarshal(t, events[0].Data, &e0)
	if e0["type"] != "message_start" {
		t.Errorf("event[0].type: expected message_start, got %v", e0["type"])
	}
	msg0, _ := e0["message"].(map[string]any)
	if msg0["id"] != "msg_testreqa" {
		t.Errorf("event[0].message.id: expected msg_testreqa, got %v", msg0["id"])
	}
	if msg0["role"] != "assistant" {
		t.Errorf("event[0].message.role: expected assistant, got %v", msg0["role"])
	}
	if msg0["model"] != "test-model" {
		t.Errorf("event[0].message.model: expected test-model, got %v", msg0["model"])
	}

	// --- event[1]: content_block_start (text) ---
	if events[1].Event != "content_block_start" {
		t.Errorf("event[1]: expected content_block_start, got %q", events[1].Event)
	}
	var e1 map[string]any
	mustUnmarshal(t, events[1].Data, &e1)
	if e1["index"] != float64(0) {
		t.Errorf("event[1].index: expected 0, got %v", e1["index"])
	}
	cb1, _ := e1["content_block"].(map[string]any)
	if cb1["type"] != "text" {
		t.Errorf("event[1].content_block.type: expected text, got %v", cb1["type"])
	}

	// --- event[2]: content_block_delta (text_delta "Hi") ---
	if events[2].Event != "content_block_delta" {
		t.Errorf("event[2]: expected content_block_delta, got %q", events[2].Event)
	}
	var e2 map[string]any
	mustUnmarshal(t, events[2].Data, &e2)
	if e2["index"] != float64(0) {
		t.Errorf("event[2].index: expected 0, got %v", e2["index"])
	}
	delta2, _ := e2["delta"].(map[string]any)
	if delta2["type"] != "text_delta" {
		t.Errorf("event[2].delta.type: expected text_delta, got %v", delta2["type"])
	}
	if delta2["text"] != "Hi" {
		t.Errorf("event[2].delta.text: expected Hi, got %v", delta2["text"])
	}

	// --- event[3]: content_block_stop ---
	if events[3].Event != "content_block_stop" {
		t.Errorf("event[3]: expected content_block_stop, got %q", events[3].Event)
	}
	var e3 map[string]any
	mustUnmarshal(t, events[3].Data, &e3)
	if e3["index"] != float64(0) {
		t.Errorf("event[3].index: expected 0, got %v", e3["index"])
	}

	// --- event[4]: message_delta ---
	if events[4].Event != "message_delta" {
		t.Errorf("event[4]: expected message_delta, got %q", events[4].Event)
	}
	var e4 map[string]any
	mustUnmarshal(t, events[4].Data, &e4)
	delta4, _ := e4["delta"].(map[string]any)
	if delta4["stop_reason"] != "end_turn" {
		t.Errorf("event[4].delta.stop_reason: expected end_turn, got %v", delta4["stop_reason"])
	}

	// --- event[5]: message_stop ---
	if events[5].Event != "message_stop" {
		t.Errorf("event[5]: expected message_stop, got %q", events[5].Event)
	}
	var e5 map[string]any
	mustUnmarshal(t, events[5].Data, &e5)
	if e5["type"] != "message_stop" {
		t.Errorf("event[5].type: expected message_stop, got %v", e5["type"])
	}
}

// ---------------------------------------------------------------------------
// Test 3: Chat → Anthropic, tool call fragments
// ---------------------------------------------------------------------------

func TestStreamState_ChatToAnthropic_ToolCallFragments(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolAnthropic,
		protocol.ProtocolChatCompletions,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	// Input 1: tool_call first fragment (name + empty args)
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
	))

	// Input 2: tool_call args fragment 1
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"loc"}}]},"finish_reason":null}]}`,
	))

	// Input 3: tool_call args fragment 2
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ation\":\"SF\"}"}}]},"finish_reason":null}]}`,
	))

	// Input 4: finish with tool_calls reason
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect exactly 7 events.
	if len(events) != 7 {
		t.Fatalf("expected 7 events, got %d\nbody:\n%s", len(events), body)
	}

	// --- event[0]: message_start ---
	if events[0].Event != "message_start" {
		t.Errorf("event[0]: expected message_start, got %q", events[0].Event)
	}

	// --- event[1]: content_block_start (tool_use at index=1) ---
	// Note: Anthropic target reserves index 0 for text; tools start at index 1.
	if events[1].Event != "content_block_start" {
		t.Errorf("event[1]: expected content_block_start, got %q", events[1].Event)
	}
	var e1 map[string]any
	mustUnmarshal(t, events[1].Data, &e1)
	if e1["index"] != float64(1) {
		t.Errorf("event[1].index: expected 1 (tool block), got %v", e1["index"])
	}
	cb1, _ := e1["content_block"].(map[string]any)
	if cb1["type"] != "tool_use" {
		t.Errorf("event[1].content_block.type: expected tool_use, got %v", cb1["type"])
	}
	if cb1["id"] != "call_abc" {
		t.Errorf("event[1].content_block.id: expected call_abc, got %v", cb1["id"])
	}
	if cb1["name"] != "get_weather" {
		t.Errorf("event[1].content_block.name: expected get_weather, got %v", cb1["name"])
	}

	// --- event[2]: content_block_delta (input_json_delta, fragment 1) ---
	if events[2].Event != "content_block_delta" {
		t.Errorf("event[2]: expected content_block_delta, got %q", events[2].Event)
	}
	var e2 map[string]any
	mustUnmarshal(t, events[2].Data, &e2)
	if e2["index"] != float64(1) {
		t.Errorf("event[2].index: expected 1, got %v", e2["index"])
	}
	delta2, _ := e2["delta"].(map[string]any)
	if delta2["type"] != "input_json_delta" {
		t.Errorf("event[2].delta.type: expected input_json_delta, got %v", delta2["type"])
	}
	if delta2["partial_json"] != `{"loc` {
		t.Errorf("event[2].delta.partial_json: expected '{\"loc', got %v", delta2["partial_json"])
	}

	// --- event[3]: content_block_delta (input_json_delta, fragment 2) ---
	if events[3].Event != "content_block_delta" {
		t.Errorf("event[3]: expected content_block_delta, got %q", events[3].Event)
	}
	var e3 map[string]any
	mustUnmarshal(t, events[3].Data, &e3)
	delta3, _ := e3["delta"].(map[string]any)
	if delta3["partial_json"] != `ation":"SF"}` {
		t.Errorf("event[3].delta.partial_json: expected 'ation\":\"SF\"}', got %v", delta3["partial_json"])
	}

	// --- event[4]: content_block_stop ---
	if events[4].Event != "content_block_stop" {
		t.Errorf("event[4]: expected content_block_stop, got %q", events[4].Event)
	}
	var e4 map[string]any
	mustUnmarshal(t, events[4].Data, &e4)
	if e4["index"] != float64(1) {
		t.Errorf("event[4].index: expected 1, got %v", e4["index"])
	}

	// --- event[5]: message_delta (stop_reason=tool_use) ---
	if events[5].Event != "message_delta" {
		t.Errorf("event[5]: expected message_delta, got %q", events[5].Event)
	}
	var e5 map[string]any
	mustUnmarshal(t, events[5].Data, &e5)
	delta5, _ := e5["delta"].(map[string]any)
	if delta5["stop_reason"] != "tool_use" {
		t.Errorf("event[5].delta.stop_reason: expected tool_use, got %v", delta5["stop_reason"])
	}

	// --- event[6]: message_stop ---
	if events[6].Event != "message_stop" {
		t.Errorf("event[6]: expected message_stop, got %q", events[6].Event)
	}
}

// ---------------------------------------------------------------------------
// Test 4: Responses → Chat, tool call
// ---------------------------------------------------------------------------

func TestStreamState_ResponsesToChat_ToolCall(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolChatCompletions,
		protocol.ProtocolResponses,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)
	// Opt in to stream usage, as a real Chat client would.
	state.chatIncludeUsage = true

	// Input 1: response.created (prelude — no-op for Chat target)
	state.processUpstreamData(w, w, []byte(
		`{"type":"response.created","sequence_number":0,"response":{"id":"resp_x","model":"m","status":"in_progress","output":[]}}`,
	))

	// Input 2: output_item.added (function_call)
	state.processUpstreamData(w, w, []byte(
		`{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"do_it","arguments":""}}`,
	))

	// Input 3: function_call_arguments.delta
	state.processUpstreamData(w, w, []byte(
		`{"type":"response.function_call_arguments.delta","sequence_number":2,"output_index":0,"item_id":"fc_1","delta":"{\"x\":1}"}`,
	))

	// Input 4: function_call_arguments.done
	state.processUpstreamData(w, w, []byte(
		`{"type":"response.function_call_arguments.done","sequence_number":3,"output_index":0,"item_id":"fc_1","name":"do_it","arguments":"{\"x\":1}"}`,
	))

	// Input 5: output_item.done (function_call)
	state.processUpstreamData(w, w, []byte(
		`{"type":"response.output_item.done","sequence_number":4,"output_index":0,"item":{"id":"fc_1","type":"function_call","status":"completed","call_id":"call_1","name":"do_it","arguments":"{\"x\":1}"}}`,
	))

	// Input 6: response.completed
	state.processUpstreamData(w, w, []byte(
		`{"type":"response.completed","sequence_number":5,"response":{"id":"resp_x","model":"m","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect 5 events: tool_calls first chunk, delta chunk, finish chunk,
	// usage chunk, [DONE].
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d\nbody:\n%s", len(events), body)
	}

	// --- event[0]: tool_calls first chunk ---
	// Chat target: no event header.
	if events[0].Event != "" {
		t.Errorf("event[0]: expected empty event (Chat target), got %q", events[0].Event)
	}
	var e0 map[string]any
	mustUnmarshal(t, events[0].Data, &e0)
	choices0, _ := e0["choices"].([]any)
	if len(choices0) != 1 {
		t.Fatalf("event[0].choices: expected 1, got %d", len(choices0))
	}
	ch0, _ := choices0[0].(map[string]any)
	delta0, _ := ch0["delta"].(map[string]any)
	tc0, _ := delta0["tool_calls"].([]any)
	if len(tc0) != 1 {
		t.Fatalf("event[0].delta.tool_calls: expected 1, got %d", len(tc0))
	}
	tc0map, _ := tc0[0].(map[string]any)
	if tc0map["index"] != float64(0) {
		t.Errorf("event[0].tool_calls[0].index: expected 0, got %v", tc0map["index"])
	}
	if tc0map["id"] != "fc_1" {
		t.Errorf("event[0].tool_calls[0].id: expected fc_1, got %v", tc0map["id"])
	}
	if tc0map["type"] != "function" {
		t.Errorf("event[0].tool_calls[0].type: expected function, got %v", tc0map["type"])
	}
	func0, _ := tc0map["function"].(map[string]any)
	if func0["name"] != "do_it" {
		t.Errorf("event[0].tool_calls[0].function.name: expected do_it, got %v", func0["name"])
	}
	if func0["arguments"] != "" {
		t.Errorf("event[0].tool_calls[0].function.arguments: expected empty, got %v", func0["arguments"])
	}

	// --- event[1]: tool_calls delta chunk ---
	if events[1].Event != "" {
		t.Errorf("event[1]: expected empty event, got %q", events[1].Event)
	}
	var e1 map[string]any
	mustUnmarshal(t, events[1].Data, &e1)
	choices1, _ := e1["choices"].([]any)
	ch1, _ := choices1[0].(map[string]any)
	delta1, _ := ch1["delta"].(map[string]any)
	tc1, _ := delta1["tool_calls"].([]any)
	tc1map, _ := tc1[0].(map[string]any)
	func1, _ := tc1map["function"].(map[string]any)
	if func1["arguments"] != `{"x":1}` {
		t.Errorf("event[1].tool_calls[0].function.arguments: expected '{\"x\":1}', got %v", func1["arguments"])
	}

	// --- event[2]: finish chunk ---
	if events[2].Event != "" {
		t.Errorf("event[2]: expected empty event, got %q", events[2].Event)
	}
	var e2 map[string]any
	mustUnmarshal(t, events[2].Data, &e2)
	choices2, _ := e2["choices"].([]any)
	ch2, _ := choices2[0].(map[string]any)
	if ch2["finish_reason"] != "tool_calls" {
		// When tool_calls were observed during the stream, the state machine
		// maps response.completed to Chat's finish_reason:"tool_calls" so
		// clients know a function invocation is pending.
		t.Errorf("event[2].finish_reason: expected 'tool_calls', got %v", ch2["finish_reason"])
	}

	// --- event[3]: usage chunk ---
	if events[3].Event != "" {
		t.Errorf("event[3]: expected empty event (Chat target), got %q", events[3].Event)
	}
	var e3 map[string]any
	mustUnmarshal(t, events[3].Data, &e3)
	choices3, _ := e3["choices"].([]any)
	ch3, _ := choices3[0].(map[string]any)
	if ch3["finish_reason"] != nil {
		t.Errorf("event[3].finish_reason: expected null, got %v", ch3["finish_reason"])
	}
	usage3, _ := e3["usage"].(map[string]any)
	if usage3 == nil {
		t.Fatal("event[3].usage is nil")
	}
	if usage3["prompt_tokens"] != float64(5) {
		t.Errorf("event[3].usage.prompt_tokens: expected 5, got %v", usage3["prompt_tokens"])
	}
	if usage3["completion_tokens"] != float64(3) {
		t.Errorf("event[3].usage.completion_tokens: expected 3, got %v", usage3["completion_tokens"])
	}
	if usage3["total_tokens"] != float64(8) {
		t.Errorf("event[3].usage.total_tokens: expected 8, got %v", usage3["total_tokens"])
	}

	// --- event[4]: [DONE] ---
	if events[4].Event != "" {
		t.Errorf("event[4]: expected empty event, got %q", events[4].Event)
	}
	if string(events[4].Data) != "[DONE]" {
		t.Errorf("event[4].data: expected [DONE], got %s", string(events[4].Data))
	}
}

// ---------------------------------------------------------------------------
// Test 5: Anthropic → Chat, text only
// ---------------------------------------------------------------------------

func TestStreamState_AnthropicToChat_TextOnly(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolChatCompletions,
		protocol.ProtocolAnthropic,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	// Input 1: message_start
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_start","message":{"id":"msg_x","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":8,"output_tokens":0}}}`,
	))

	// Input 2: content_block_start (text)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	))

	// Input 3: content_block_delta (text_delta "Hello")
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
	))

	// Input 4: content_block_stop
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_stop","index":0}`,
	))

	// Input 5: message_delta
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
	))

	// Input 6: message_stop
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_stop"}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect 3 events: text chunk, finish chunk, [DONE].
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d\nbody:\n%s", len(events), body)
	}

	// --- event[0]: text chunk ---
	if events[0].Event != "" {
		t.Errorf("event[0]: expected empty event (Chat target), got %q", events[0].Event)
	}
	var e0 map[string]any
	mustUnmarshal(t, events[0].Data, &e0)
	choices0, _ := e0["choices"].([]any)
	if len(choices0) != 1 {
		t.Fatalf("event[0].choices: expected 1, got %d", len(choices0))
	}
	ch0, _ := choices0[0].(map[string]any)
	delta0, _ := ch0["delta"].(map[string]any)
	if delta0["content"] != "Hello" {
		t.Errorf("event[0].delta.content: expected Hello, got %v", delta0["content"])
	}

	// --- event[1]: finish chunk ---
	if events[1].Event != "" {
		t.Errorf("event[1]: expected empty event, got %q", events[1].Event)
	}
	var e1 map[string]any
	mustUnmarshal(t, events[1].Data, &e1)
	choices1, _ := e1["choices"].([]any)
	ch1, _ := choices1[0].(map[string]any)
	if ch1["finish_reason"] != "stop" {
		t.Errorf("event[1].finish_reason: expected stop, got %v", ch1["finish_reason"])
	}

	// --- event[2]: [DONE] ---
	if events[2].Event != "" {
		t.Errorf("event[2]: expected empty event, got %q", events[2].Event)
	}
	if string(events[2].Data) != "[DONE]" {
		t.Errorf("event[2].data: expected [DONE], got %s", string(events[2].Data))
	}
}

// ---------------------------------------------------------------------------
// Test 6: Chat → Anthropic, reasoning then text
// ---------------------------------------------------------------------------

func TestStreamState_ChatToAnthropic_ReasoningThenText(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolAnthropic,
		protocol.ProtocolChatCompletions,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	// Input 1: reasoning delta
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"reasoning_content":"想"},"finish_reason":null}]}`,
	))

	// Input 2: second reasoning delta
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"reasoning_content":"啊"},"finish_reason":null}]}`,
	))

	// Input 3: text delta
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"content":"回答"},"finish_reason":null}]}`,
	))

	// Input 4: finish chunk
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect exactly 10 events.
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d\nbody:\n%s", len(events), body)
	}

	// --- event[0]: message_start ---
	if events[0].Event != "message_start" {
		t.Errorf("event[0]: expected message_start, got %q", events[0].Event)
	}

	// --- event[1]: content_block_start (thinking at index=0) ---
	if events[1].Event != "content_block_start" {
		t.Errorf("event[1]: expected content_block_start, got %q", events[1].Event)
	}
	var e1 map[string]any
	mustUnmarshal(t, events[1].Data, &e1)
	if e1["index"] != float64(0) {
		t.Errorf("event[1].index: expected 0, got %v", e1["index"])
	}
	cb1, _ := e1["content_block"].(map[string]any)
	if cb1["type"] != "thinking" {
		t.Errorf("event[1].content_block.type: expected thinking, got %v", cb1["type"])
	}

	// --- event[2]: content_block_delta (thinking_delta "想") ---
	if events[2].Event != "content_block_delta" {
		t.Errorf("event[2]: expected content_block_delta, got %q", events[2].Event)
	}
	var e2 map[string]any
	mustUnmarshal(t, events[2].Data, &e2)
	if e2["index"] != float64(0) {
		t.Errorf("event[2].index: expected 0, got %v", e2["index"])
	}
	delta2, _ := e2["delta"].(map[string]any)
	if delta2["type"] != "thinking_delta" {
		t.Errorf("event[2].delta.type: expected thinking_delta, got %v", delta2["type"])
	}
	if delta2["thinking"] != "想" {
		t.Errorf("event[2].delta.thinking: expected '想', got %v", delta2["thinking"])
	}

	// --- event[3]: content_block_delta (thinking_delta "啊") ---
	if events[3].Event != "content_block_delta" {
		t.Errorf("event[3]: expected content_block_delta, got %q", events[3].Event)
	}
	var e3 map[string]any
	mustUnmarshal(t, events[3].Data, &e3)
	delta3, _ := e3["delta"].(map[string]any)
	if delta3["thinking"] != "啊" {
		t.Errorf("event[3].delta.thinking: expected '啊', got %v", delta3["thinking"])
	}

	// --- event[4]: content_block_stop (thinking at index=0) ---
	if events[4].Event != "content_block_stop" {
		t.Errorf("event[4]: expected content_block_stop, got %q", events[4].Event)
	}
	var e4 map[string]any
	mustUnmarshal(t, events[4].Data, &e4)
	if e4["index"] != float64(0) {
		t.Errorf("event[4].index: expected 0, got %v", e4["index"])
	}

	// --- event[5]: content_block_start (text at index=1) ---
	if events[5].Event != "content_block_start" {
		t.Errorf("event[5]: expected content_block_start, got %q", events[5].Event)
	}
	var e5 map[string]any
	mustUnmarshal(t, events[5].Data, &e5)
	if e5["index"] != float64(1) {
		t.Errorf("event[5].index: expected 1, got %v", e5["index"])
	}
	cb5, _ := e5["content_block"].(map[string]any)
	if cb5["type"] != "text" {
		t.Errorf("event[5].content_block.type: expected text, got %v", cb5["type"])
	}

	// --- event[6]: content_block_delta (text_delta "回答") ---
	if events[6].Event != "content_block_delta" {
		t.Errorf("event[6]: expected content_block_delta, got %q", events[6].Event)
	}
	var e6 map[string]any
	mustUnmarshal(t, events[6].Data, &e6)
	if e6["index"] != float64(1) {
		t.Errorf("event[6].index: expected 1, got %v", e6["index"])
	}
	delta6, _ := e6["delta"].(map[string]any)
	if delta6["type"] != "text_delta" {
		t.Errorf("event[6].delta.type: expected text_delta, got %v", delta6["type"])
	}
	if delta6["text"] != "回答" {
		t.Errorf("event[6].delta.text: expected '回答', got %v", delta6["text"])
	}

	// --- event[7]: content_block_stop (text at index=1) ---
	if events[7].Event != "content_block_stop" {
		t.Errorf("event[7]: expected content_block_stop, got %q", events[7].Event)
	}
	var e7 map[string]any
	mustUnmarshal(t, events[7].Data, &e7)
	if e7["index"] != float64(1) {
		t.Errorf("event[7].index: expected 1, got %v", e7["index"])
	}

	// --- event[8]: message_delta ---
	if events[8].Event != "message_delta" {
		t.Errorf("event[8]: expected message_delta, got %q", events[8].Event)
	}
	var e8 map[string]any
	mustUnmarshal(t, events[8].Data, &e8)
	delta8, _ := e8["delta"].(map[string]any)
	if delta8["stop_reason"] != "end_turn" {
		t.Errorf("event[8].delta.stop_reason: expected end_turn, got %v", delta8["stop_reason"])
	}

	// --- event[9]: message_stop ---
	if events[9].Event != "message_stop" {
		t.Errorf("event[9]: expected message_stop, got %q", events[9].Event)
	}
}

// ---------------------------------------------------------------------------
// Test 7: Anthropic → Chat, thinking with signature
// ---------------------------------------------------------------------------

func TestStreamState_AnthropicToChat_ThinkingWithSignature(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolChatCompletions,
		protocol.ProtocolAnthropic,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	// Input 1: message_start
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_start","message":{"id":"msg_x","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":8,"output_tokens":0}}}`,
	))

	// Input 2: content_block_start (thinking)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
	))

	// Input 3: content_block_delta (thinking_delta)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"想啊"}}`,
	))

	// Input 4: content_block_delta (signature_delta)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig123"}}`,
	))

	// Input 5: content_block_stop (thinking)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_stop","index":0}`,
	))

	// Input 6: content_block_start (text)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
	))

	// Input 7: content_block_delta (text_delta)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"回答"}}`,
	))

	// Input 8: content_block_stop (text)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_stop","index":1}`,
	))

	// Input 9: message_delta
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
	))

	// Input 10: message_stop
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_stop"}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect 4 events: reasoning_content chunk, text chunk, finish chunk, [DONE].
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d\nbody:\n%s", len(events), body)
	}

	// --- event[0]: reasoning_content chunk ---
	if events[0].Event != "" {
		t.Errorf("event[0]: expected empty event (Chat target), got %q", events[0].Event)
	}
	var e0 map[string]any
	mustUnmarshal(t, events[0].Data, &e0)
	choices0, _ := e0["choices"].([]any)
	if len(choices0) != 1 {
		t.Fatalf("event[0].choices: expected 1, got %d", len(choices0))
	}
	ch0, _ := choices0[0].(map[string]any)
	delta0, _ := ch0["delta"].(map[string]any)
	if delta0["reasoning_content"] != "想啊" {
		t.Errorf("event[0].delta.reasoning_content: expected '想啊', got %v", delta0["reasoning_content"])
	}

	// --- event[1]: text chunk ---
	if events[1].Event != "" {
		t.Errorf("event[1]: expected empty event (Chat target), got %q", events[1].Event)
	}
	var e1 map[string]any
	mustUnmarshal(t, events[1].Data, &e1)
	choices1, _ := e1["choices"].([]any)
	ch1, _ := choices1[0].(map[string]any)
	delta1, _ := ch1["delta"].(map[string]any)
	if delta1["content"] != "回答" {
		t.Errorf("event[1].delta.content: expected '回答', got %v", delta1["content"])
	}

	// --- event[2]: finish chunk ---
	if events[2].Event != "" {
		t.Errorf("event[2]: expected empty event (Chat target), got %q", events[2].Event)
	}
	var e2 map[string]any
	mustUnmarshal(t, events[2].Data, &e2)
	choices2, _ := e2["choices"].([]any)
	ch2, _ := choices2[0].(map[string]any)
	if ch2["finish_reason"] != "stop" {
		t.Errorf("event[2].finish_reason: expected stop, got %v", ch2["finish_reason"])
	}

	// --- event[3]: [DONE] ---
	if events[3].Event != "" {
		t.Errorf("event[3]: expected empty event (Chat target), got %q", events[3].Event)
	}
	if string(events[3].Data) != "[DONE]" {
		t.Errorf("event[3].data: expected [DONE], got %s", string(events[3].Data))
	}
}

// ---------------------------------------------------------------------------
// Test 8: Chat → Responses, reasoning then text
// ---------------------------------------------------------------------------

func TestStreamState_ChatToResponses_ReasoningThenText(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolResponses,
		protocol.ProtocolChatCompletions,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	// Input 1: reasoning delta
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"reasoning_content":"想"},"finish_reason":null}]}`,
	))

	// Input 2: second reasoning delta
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"reasoning_content":"啊"},"finish_reason":null}]}`,
	))

	// Input 3: text delta
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"content":"回答"},"finish_reason":null}]}`,
	))

	// Input 4: finish chunk
	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect exactly 16 events.
	if len(events) != 16 {
		t.Fatalf("expected 16 events, got %d\nbody:\n%s", len(events), body)
	}

	// --- event[0]: response.created ---
	if events[0].Event != "response.created" {
		t.Errorf("event[0]: expected response.created, got %q", events[0].Event)
	}

	// --- event[1]: response.in_progress ---
	if events[1].Event != "response.in_progress" {
		t.Errorf("event[1]: expected response.in_progress, got %q", events[1].Event)
	}

	// --- event[2]: response.output_item.added (reasoning at output_index=0) ---
	if events[2].Event != "response.output_item.added" {
		t.Errorf("event[2]: expected response.output_item.added, got %q", events[2].Event)
	}
	var e2 map[string]any
	mustUnmarshal(t, events[2].Data, &e2)
	if e2["output_index"] != float64(0) {
		t.Errorf("event[2].output_index: expected 0, got %v", e2["output_index"])
	}
	item2, _ := e2["item"].(map[string]any)
	if item2["type"] != "reasoning" {
		t.Errorf("event[2].item.type: expected reasoning, got %v", item2["type"])
	}
	if item2["status"] != "in_progress" {
		t.Errorf("event[2].item.status: expected in_progress, got %v", item2["status"])
	}

	// --- event[3]: response.content_part.added ---
	if events[3].Event != "response.content_part.added" {
		t.Errorf("event[3]: expected response.content_part.added, got %q", events[3].Event)
	}

	// --- event[4]: response.reasoning_text.delta (想) ---
	if events[4].Event != "response.reasoning_text.delta" {
		t.Errorf("event[4]: expected response.reasoning_text.delta, got %q", events[4].Event)
	}
	var e4 map[string]any
	mustUnmarshal(t, events[4].Data, &e4)
	if e4["delta"] != "想" {
		t.Errorf("event[4].delta: expected '想', got %v", e4["delta"])
	}

	// --- event[5]: response.reasoning_text.delta (啊) ---
	if events[5].Event != "response.reasoning_text.delta" {
		t.Errorf("event[5]: expected response.reasoning_text.delta, got %q", events[5].Event)
	}
	var e5 map[string]any
	mustUnmarshal(t, events[5].Data, &e5)
	if e5["delta"] != "啊" {
		t.Errorf("event[5].delta: expected '啊', got %v", e5["delta"])
	}

	// --- event[6]: response.reasoning_text.done ---
	if events[6].Event != "response.reasoning_text.done" {
		t.Errorf("event[6]: expected response.reasoning_text.done, got %q", events[6].Event)
	}
	var e6 map[string]any
	mustUnmarshal(t, events[6].Data, &e6)
	if e6["text"] != "想啊" {
		t.Errorf("event[6].text: expected '想啊', got %v", e6["text"])
	}

	// --- event[7]: response.content_part.done ---
	if events[7].Event != "response.content_part.done" {
		t.Errorf("event[7]: expected response.content_part.done, got %q", events[7].Event)
	}

	// --- event[8]: response.output_item.done (reasoning at output_index=0) ---
	if events[8].Event != "response.output_item.done" {
		t.Errorf("event[8]: expected response.output_item.done, got %q", events[8].Event)
	}
	var e8 map[string]any
	mustUnmarshal(t, events[8].Data, &e8)
	if e8["output_index"] != float64(0) {
		t.Errorf("event[8].output_index: expected 0, got %v", e8["output_index"])
	}
	item8, _ := e8["item"].(map[string]any)
	if item8["type"] != "reasoning" {
		t.Errorf("event[8].item.type: expected reasoning, got %v", item8["type"])
	}
	if item8["status"] != "completed" {
		t.Errorf("event[8].item.status: expected completed, got %v", item8["status"])
	}

	// --- event[9]: response.output_item.added (message at output_index=1) ---
	if events[9].Event != "response.output_item.added" {
		t.Errorf("event[9]: expected response.output_item.added, got %q", events[9].Event)
	}
	var e9 map[string]any
	mustUnmarshal(t, events[9].Data, &e9)
	if e9["output_index"] != float64(1) {
		t.Errorf("event[9].output_index: expected 1, got %v", e9["output_index"])
	}
	item9, _ := e9["item"].(map[string]any)
	if item9["type"] != "message" {
		t.Errorf("event[9].item.type: expected message, got %v", item9["type"])
	}

	// --- event[10]: response.content_part.added ---
	if events[10].Event != "response.content_part.added" {
		t.Errorf("event[10]: expected response.content_part.added, got %q", events[10].Event)
	}

	// --- event[11]: response.output_text.delta (回答) ---
	if events[11].Event != "response.output_text.delta" {
		t.Errorf("event[11]: expected response.output_text.delta, got %q", events[11].Event)
	}
	var e11 map[string]any
	mustUnmarshal(t, events[11].Data, &e11)
	if e11["delta"] != "回答" {
		t.Errorf("event[11].delta: expected '回答', got %v", e11["delta"])
	}

	// --- event[12]: response.output_text.done ---
	if events[12].Event != "response.output_text.done" {
		t.Errorf("event[12]: expected response.output_text.done, got %q", events[12].Event)
	}
	var e12 map[string]any
	mustUnmarshal(t, events[12].Data, &e12)
	if e12["text"] != "回答" {
		t.Errorf("event[12].text: expected '回答', got %v", e12["text"])
	}

	// --- event[13]: response.content_part.done ---
	if events[13].Event != "response.content_part.done" {
		t.Errorf("event[13]: expected response.content_part.done, got %q", events[13].Event)
	}

	// --- event[14]: response.output_item.done (message at output_index=1) ---
	if events[14].Event != "response.output_item.done" {
		t.Errorf("event[14]: expected response.output_item.done, got %q", events[14].Event)
	}
	var e14 map[string]any
	mustUnmarshal(t, events[14].Data, &e14)
	if e14["output_index"] != float64(1) {
		t.Errorf("event[14].output_index: expected 1, got %v", e14["output_index"])
	}
	item14, _ := e14["item"].(map[string]any)
	if item14["type"] != "message" {
		t.Errorf("event[14].item.type: expected message, got %v", item14["type"])
	}

	// --- event[15]: response.completed ---
	if events[15].Event != "response.completed" {
		t.Errorf("event[15]: expected response.completed, got %q", events[15].Event)
	}
	var e15 map[string]any
	mustUnmarshal(t, events[15].Data, &e15)
	resp15, _ := e15["response"].(map[string]any)
	if resp15 == nil {
		t.Fatal("event[15].response is nil")
	}
	if resp15["status"] != "completed" {
		t.Errorf("event[15].response.status: expected completed, got %v", resp15["status"])
	}
	output15, _ := resp15["output"].([]any)
	if len(output15) != 2 {
		t.Fatalf("event[15].response.output: expected 2 items (reasoning + message), got %d", len(output15))
	}
	// First output item: reasoning
	reasoningItem, _ := output15[0].(map[string]any)
	if reasoningItem["type"] != "reasoning" {
		t.Errorf("event[15].output[0].type: expected reasoning, got %v", reasoningItem["type"])
	}
	// Second output item: message
	msgItem, _ := output15[1].(map[string]any)
	if msgItem["type"] != "message" {
		t.Errorf("event[15].output[1].type: expected message, got %v", msgItem["type"])
	}
}

// ---------------------------------------------------------------------------
// Test 9: Anthropic → Responses, thinking with signature
// ---------------------------------------------------------------------------

func TestStreamState_AnthropicToResponses_ThinkingWithSignature(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolResponses,
		protocol.ProtocolAnthropic,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	// Input 1: message_start
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_start","message":{"id":"msg_x","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":8,"output_tokens":0}}}`,
	))

	// Input 2: content_block_start (thinking)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
	))

	// Input 3: content_block_delta (thinking_delta)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"想啊"}}`,
	))

	// Input 4: content_block_delta (signature_delta)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig123"}}`,
	))

	// Input 5: content_block_stop (thinking)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_stop","index":0}`,
	))

	// Input 6: content_block_start (text)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
	))

	// Input 7: content_block_delta (text_delta)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"回答"}}`,
	))

	// Input 8: content_block_stop (text)
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_stop","index":1}`,
	))

	// Input 9: message_delta
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
	))

	// Input 10: message_stop
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_stop"}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect approximately 16 events (response.created, response.in_progress,
	// reasoning output_item.added, content_part.added, reasoning_text.delta,
	// reasoning_text.done, content_part.done, output_item.done,
	// text output_item.added, content_part.added, output_text.delta,
	// output_text.done, content_part.done, output_item.done,
	// response.completed).
	if len(events) < 14 || len(events) > 18 {
		t.Fatalf("expected ~16 events, got %d\nbody:\n%s", len(events), body)
	}

	// Find key events.
	var reasoningDoneIdx, textAddedIdx, completedIdx int
	reasoningDoneIdx = -1
	textAddedIdx = -1
	completedIdx = -1

	for i, ev := range events {
		var p map[string]any
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			continue
		}
		if ev.Event == "response.output_item.done" {
			item, _ := p["item"].(map[string]any)
			if item != nil && getString(item, "type") == "reasoning" {
				reasoningDoneIdx = i
				// Verify encrypted_content is present
				if getString(item, "encrypted_content") != "sig123" {
					t.Errorf("reasoning output_item.done encrypted_content: expected 'sig123', got '%s'", getString(item, "encrypted_content"))
				}
			}
		}
		if ev.Event == "response.output_item.added" {
			item, _ := p["item"].(map[string]any)
			if item != nil && getString(item, "type") == "message" {
				textAddedIdx = i
				// Verify output_index is 1 (not 0)
				oi, _ := p["output_index"].(float64)
				if int(oi) != 1 {
					t.Errorf("text output_item.added output_index: expected 1, got %v", oi)
				}
			}
		}
		if ev.Event == "response.completed" {
			completedIdx = i
		}
	}

	if reasoningDoneIdx < 0 {
		t.Error("expected reasoning output_item.done event")
	}
	if textAddedIdx < 0 {
		t.Error("expected text output_item.added event")
	}
	if completedIdx < 0 {
		t.Error("expected response.completed event")
	}

	// response.completed must be the last event.
	if completedIdx >= 0 && completedIdx != len(events)-1 {
		t.Errorf("response.completed should be the last event, got index %d, len %d", completedIdx, len(events))
	}

	// reasoning done must come before text added.
	if reasoningDoneIdx >= 0 && textAddedIdx >= 0 && reasoningDoneIdx > textAddedIdx {
		t.Errorf("reasoning output_item.done (idx %d) should come before text output_item.added (idx %d)", reasoningDoneIdx, textAddedIdx)
	}
}

// ---------------------------------------------------------------------------
// Test 10: Responses → Chat, usage with cached_tokens injected
// ---------------------------------------------------------------------------

func TestStreamState_ResponsesToChat_UsageWithCache(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolChatCompletions,
		protocol.ProtocolResponses,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)
	// Opt in to stream usage, as a real Chat client would.
	state.chatIncludeUsage = true

	// Text delta.
	state.processUpstreamData(w, w, []byte(
		`{"type":"response.output_text.delta","sequence_number":0,"output_index":0,"item_id":"msg_x","content_index":0,"delta":"Hi"}`,
	))

	// Provide usage with cached tokens (simulating proxy.go ParseStreamUsage).
	state.SetUsage(&convert.StreamUsage{
		InputTokens:  20,
		OutputTokens: 4,
		TotalTokens:  24,
		CachedTokens: 15,
	})

	// Finish.
	state.processUpstreamData(w, w, []byte(
		`{"type":"response.completed","sequence_number":1,"response":{"id":"resp_x","model":"m","status":"completed","output":[]}}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect: text chunk, finish chunk, usage chunk, [DONE].
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d\nbody:\n%s", len(events), body)
	}

	// --- event[2]: usage chunk ---
	var e2 map[string]any
	mustUnmarshal(t, events[2].Data, &e2)
	choices2, _ := e2["choices"].([]any)
	ch2, _ := choices2[0].(map[string]any)
	if ch2["finish_reason"] != nil {
		t.Errorf("event[2].finish_reason: expected null, got %v", ch2["finish_reason"])
	}
	usage2, _ := e2["usage"].(map[string]any)
	if usage2 == nil {
		t.Fatal("event[2].usage is nil")
	}
	if usage2["prompt_tokens"] != float64(20) {
		t.Errorf("event[2].usage.prompt_tokens: expected 20, got %v", usage2["prompt_tokens"])
	}
	if usage2["completion_tokens"] != float64(4) {
		t.Errorf("event[2].usage.completion_tokens: expected 4, got %v", usage2["completion_tokens"])
	}
	if usage2["total_tokens"] != float64(24) {
		t.Errorf("event[2].usage.total_tokens: expected 24, got %v", usage2["total_tokens"])
	}
	details2, _ := usage2["prompt_tokens_details"].(map[string]any)
	if details2 == nil {
		t.Fatal("event[2].usage.prompt_tokens_details is nil")
	}
	if details2["cached_tokens"] != float64(15) {
		t.Errorf("event[2].usage.prompt_tokens_details.cached_tokens: expected 15, got %v", details2["cached_tokens"])
	}

	// --- event[3]: [DONE] ---
	if string(events[3].Data) != "[DONE]" {
		t.Errorf("event[3].data: expected [DONE], got %s", string(events[3].Data))
	}
}

// ---------------------------------------------------------------------------
// Test 11: Chat → Anthropic, cache tokens surfaced in message_delta usage
// ---------------------------------------------------------------------------

func TestStreamState_ChatToAnthropic_UsageWithCache(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolAnthropic,
		protocol.ProtocolChatCompletions,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`,
	))

	// Provide usage before the finish chunk (proxy.go sets it via
	// ParseStreamUsage before processUpstreamData on the same line).
	// Gateway-normalised: InputTokens is the TOTAL (raw+cache). Realistic
	// values: raw=78, cache_read=15, cache_creation=7 -> total=100.
	state.SetUsage(&convert.StreamUsage{
		InputTokens:      100,
		OutputTokens:     4,
		TotalTokens:      104,
		CachedTokens:     15,
		CacheWriteTokens: 7,
	})

	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Find message_delta.
	var md map[string]any
	for _, ev := range events {
		if ev.Event != "message_delta" {
			continue
		}
		mustUnmarshal(t, ev.Data, &md)
	}
	if md == nil {
		t.Fatal("message_delta event not found")
	}
	usage, _ := md["usage"].(map[string]any)
	if usage == nil {
		t.Fatal("message_delta.usage is nil")
	}
	if usage["input_tokens"] != float64(78) {
		t.Errorf("usage.input_tokens: expected 78 (raw = total - cache_read - cache_creation), got %v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(4) {
		t.Errorf("usage.output_tokens: expected 4, got %v", usage["output_tokens"])
	}
	if usage["cache_read_input_tokens"] != float64(15) {
		t.Errorf("usage.cache_read_input_tokens: expected 15, got %v", usage["cache_read_input_tokens"])
	}
	if usage["cache_creation_input_tokens"] != float64(7) {
		t.Errorf("usage.cache_creation_input_tokens: expected 7, got %v", usage["cache_creation_input_tokens"])
	}
}

// ---------------------------------------------------------------------------
// Test 12: Chat → Responses, cached_tokens surfaced in input_tokens_details
// ---------------------------------------------------------------------------

func TestStreamState_ChatToResponses_UsageWithCache(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolResponses,
		protocol.ProtocolChatCompletions,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`,
	))

	// Provide usage before the finish chunk (proxy.go sets it via
	// ParseStreamUsage before processUpstreamData on the same line).
	state.SetUsage(&convert.StreamUsage{
		InputTokens:  20,
		OutputTokens: 4,
		TotalTokens:  24,
		CachedTokens: 15,
	})

	state.processUpstreamData(w, w, []byte(
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	var completed map[string]any
	for _, ev := range events {
		if ev.Event != "response.completed" {
			continue
		}
		mustUnmarshal(t, ev.Data, &completed)
	}
	if completed == nil {
		t.Fatal("response.completed event not found")
	}
	resp, _ := completed["response"].(map[string]any)
	usage, _ := resp["usage"].(map[string]any)
	if usage == nil {
		t.Fatal("response.usage is nil")
	}
	if usage["input_tokens"] != float64(20) {
		t.Errorf("usage.input_tokens: expected 20, got %v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(4) {
		t.Errorf("usage.output_tokens: expected 4, got %v", usage["output_tokens"])
	}
	if usage["total_tokens"] != float64(24) {
		t.Errorf("usage.total_tokens: expected 24, got %v", usage["total_tokens"])
	}
	details, _ := usage["input_tokens_details"].(map[string]any)
	if details == nil {
		t.Fatal("usage.input_tokens_details is nil")
	}
	if details["cached_tokens"] != float64(15) {
		t.Errorf("usage.input_tokens_details.cached_tokens: expected 15, got %v", details["cached_tokens"])
	}
}

// ---------------------------------------------------------------------------
// Test 13: Anthropic → Chat, usage (incl. cache) surfaced in Chat usage chunk
// ---------------------------------------------------------------------------

func TestStreamState_AnthropicToChat_UsageWithCache(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolChatCompletions,
		protocol.ProtocolAnthropic,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)
	// Opt in to stream usage, as a real Chat client would.
	state.chatIncludeUsage = true

	// Upstream Anthropic stream events.
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_start","message":{"id":"msg_x","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":78,"output_tokens":0}}}`,
	))
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	))
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
	))
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_stop","index":0}`,
	))
	// message_delta carries output + cache counters (Anthropic convention).
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":4,"cache_read_input_tokens":15,"cache_creation_input_tokens":7}}`,
	))

	// The gateway normalises InputTokens to the TOTAL (raw + cache), matching
	// what proxy.go's ParseStreamUsage would store. It is set before the
	// message_stop event that triggers writeClosure.
	state.SetUsage(&convert.StreamUsage{
		InputTokens:      100,
		OutputTokens:     4,
		TotalTokens:      104,
		CachedTokens:     15,
		CacheWriteTokens: 7,
	})

	state.processUpstreamData(w, w, []byte(
		`{"type":"message_stop"}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Find the usage chunk (the data chunk carrying "usage").
	var usageChunk map[string]any
	for _, ev := range events {
		if ev.Event != "" {
			continue
		}
		var p map[string]any
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			continue
		}
		if _, ok := p["usage"]; ok {
			usageChunk = p
		}
	}
	if usageChunk == nil {
		t.Fatal("Chat usage chunk not found")
	}
	usage, _ := usageChunk["usage"].(map[string]any)
	if usage == nil {
		t.Fatal("usage is nil")
	}
	// OpenAI Chat prompt_tokens is the TOTAL including cache.
	if usage["prompt_tokens"] != float64(100) {
		t.Errorf("usage.prompt_tokens: expected 100 (total incl cache), got %v", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != float64(4) {
		t.Errorf("usage.completion_tokens: expected 4, got %v", usage["completion_tokens"])
	}
	if usage["total_tokens"] != float64(104) {
		t.Errorf("usage.total_tokens: expected 104, got %v", usage["total_tokens"])
	}
	details, _ := usage["prompt_tokens_details"].(map[string]any)
	if details == nil {
		t.Fatal("usage.prompt_tokens_details is nil")
	}
	if details["cached_tokens"] != float64(15) {
		t.Errorf("usage.prompt_tokens_details.cached_tokens: expected 15, got %v", details["cached_tokens"])
	}
}

// ---------------------------------------------------------------------------
// Test 14: Anthropic → Responses, usage (incl. cache) surfaced in response.completed
// ---------------------------------------------------------------------------

func TestStreamState_AnthropicToResponses_UsageWithCache(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolResponses,
		protocol.ProtocolAnthropic,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	state.processUpstreamData(w, w, []byte(
		`{"type":"message_start","message":{"id":"msg_x","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":78,"output_tokens":0}}}`,
	))
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	))
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
	))
	state.processUpstreamData(w, w, []byte(
		`{"type":"content_block_stop","index":0}`,
	))
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":4,"cache_read_input_tokens":15,"cache_creation_input_tokens":7}}`,
	))

	// Gateway-normalised TOTAL (raw + cache), set before message_stop.
	state.SetUsage(&convert.StreamUsage{
		InputTokens:  100,
		OutputTokens: 4,
		TotalTokens:  104,
		CachedTokens: 15,
	})

	state.processUpstreamData(w, w, []byte(
		`{"type":"message_stop"}`,
	))

	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	var completed map[string]any
	for _, ev := range events {
		if ev.Event != "response.completed" {
			continue
		}
		mustUnmarshal(t, ev.Data, &completed)
	}
	if completed == nil {
		t.Fatal("response.completed event not found")
	}
	resp, _ := completed["response"].(map[string]any)
	usage, _ := resp["usage"].(map[string]any)
	if usage == nil {
		t.Fatal("response.usage is nil")
	}
	// Responses input_tokens is the TOTAL including cache.
	if usage["input_tokens"] != float64(100) {
		t.Errorf("usage.input_tokens: expected 100 (total incl cache), got %v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(4) {
		t.Errorf("usage.output_tokens: expected 4, got %v", usage["output_tokens"])
	}
	if usage["total_tokens"] != float64(104) {
		t.Errorf("usage.total_tokens: expected 104, got %v", usage["total_tokens"])
	}
	details, _ := usage["input_tokens_details"].(map[string]any)
	if details == nil {
		t.Fatal("usage.input_tokens_details is nil")
	}
	if details["cached_tokens"] != float64(15) {
		t.Errorf("usage.input_tokens_details.cached_tokens: expected 15, got %v", details["cached_tokens"])
	}
}

// ---------------------------------------------------------------------------
// Test 15: Responses → Chat, NO usage chunk when client did not opt in
// ---------------------------------------------------------------------------

func TestStreamState_ResponsesToChat_NoUsageWhenNotRequested(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolChatCompletions,
		protocol.ProtocolResponses,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)
	// chatIncludeUsage left at its default (false): client did not set
	// stream_options.include_usage, so no usage block should be emitted.

	state.processUpstreamData(w, w, []byte(
		`{"type":"response.output_text.delta","sequence_number":0,"output_index":0,"item_id":"msg_x","content_index":0,"delta":"Hi"}`,
	))
	state.SetUsage(&convert.StreamUsage{
		InputTokens:  20,
		OutputTokens: 4,
		TotalTokens:  24,
		CachedTokens: 15,
	})
	state.processUpstreamData(w, w, []byte(
		`{"type":"response.completed","sequence_number":1,"response":{"id":"resp_x","model":"m","status":"completed","output":[]}}`,
	))
	state.writeStreamEnd(w, w)

	body := rr.Body.String()
	events := parseSSE(body)

	// Expect: text chunk, finish chunk, [DONE] (no usage chunk).
	if len(events) != 3 {
		t.Fatalf("expected 3 events (no usage), got %d\nbody:\n%s", len(events), body)
	}
	for _, ev := range events {
		var p map[string]any
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			continue
		}
		if _, ok := p["usage"]; ok {
			t.Errorf("unexpected usage chunk emitted without include_usage opt-in: %s", string(ev.Data))
		}
	}
	if string(events[len(events)-1].Data) != "[DONE]" {
		t.Errorf("last event: expected [DONE], got %s", string(events[len(events)-1].Data))
	}
}

// ---------------------------------------------------------------------------
// Test 16: Anthropic → Anthropic, message_start carries real input_tokens
// ---------------------------------------------------------------------------

func TestStreamState_AnthropicToAnthropic_PreludeInputTokens(t *testing.T) {
	rr := httptest.NewRecorder()
	w := &flushRecorder{rr}
	logEntry := testLogEntry()

	state := newStreamState(
		protocol.ProtocolAnthropic,
		protocol.ProtocolAnthropic,
		logEntry.RequestID, "test-model",
		fixedStart, logEntry,
	)

	// Upstream message_start advertises input_tokens; the downstream
	// message_start should surface it instead of a zero placeholder.
	state.processUpstreamData(w, w, []byte(
		`{"type":"message_start","message":{"id":"msg_x","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":78,"output_tokens":0}}}`,
	))

	body := rr.Body.String()
	events := parseSSE(body)

	var ms map[string]any
	for _, ev := range events {
		if ev.Event != "message_start" {
			continue
		}
		mustUnmarshal(t, ev.Data, &ms)
	}
	if ms == nil {
		t.Fatal("message_start event not found")
	}
	msg, _ := ms["message"].(map[string]any)
	if msg == nil {
		t.Fatal("message_start.message is nil")
	}
	usage, _ := msg["usage"].(map[string]any)
	if usage == nil {
		t.Fatal("message_start.message.usage is nil")
	}
	if usage["input_tokens"] != float64(78) {
		t.Errorf("message_start usage.input_tokens: expected 78, got %v", usage["input_tokens"])
	}
}
