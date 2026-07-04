package gateway

import (
	"testing"

	"github.com/lovelytoaster94/routerhub/internal/storage"
)

func TestParseUsageFromResponse_ChatCompletions(t *testing.T) {
	body := []byte(`{
		"usage": {
			"prompt_tokens": 1843,
			"completion_tokens": 20,
			"total_tokens": 1863,
			"prompt_tokens_details": {"cached_tokens": 1024, "audio_tokens": 0}
		}
	}`)
	le := &storage.RequestLog{}
	parseUsageFromResponse(body, le, "openai-chat-completions")
	if le.InputTokens != 1843 || le.OutputTokens != 20 || le.CachedTokens != 1024 || le.TotalTokens != 1863 {
		t.Fatalf("chat: got input=%d output=%d cached=%d total=%d",
			le.InputTokens, le.OutputTokens, le.CachedTokens, le.TotalTokens)
	}
}

func TestParseUsageFromResponse_ChatCompletions_NoCache(t *testing.T) {
	body := []byte(`{
		"usage": {
			"prompt_tokens": 100,
			"completion_tokens": 20,
			"total_tokens": 120,
			"prompt_tokens_details": {"cached_tokens": 0, "audio_tokens": 0}
		}
	}`)
	le := &storage.RequestLog{}
	parseUsageFromResponse(body, le, "openai-chat-completions")
	if le.CachedTokens != 0 {
		t.Fatalf("expected cached=0 got %d", le.CachedTokens)
	}
}

func TestParseUsageFromResponse_Responses(t *testing.T) {
	body := []byte(`{
		"usage": {
			"input_tokens": 500,
			"output_tokens": 50,
			"total_tokens": 550,
			"input_tokens_details": {"cached_tokens": 200}
		}
	}`)
	le := &storage.RequestLog{}
	parseUsageFromResponse(body, le, "openai-responses")
	if le.InputTokens != 500 || le.OutputTokens != 50 || le.TotalTokens != 550 {
		t.Fatalf("responses core mismatch input=%d output=%d total=%d",
			le.InputTokens, le.OutputTokens, le.TotalTokens)
	}
	if le.CachedTokens != 200 {
		t.Fatalf("responses cached expected 200 got %d (bug: cached not parsed for Responses protocol)", le.CachedTokens)
	}
}

func TestParseUsageFromResponse_Anthropic(t *testing.T) {
	body := []byte(`{
		"usage": {
			"input_tokens": 100,
			"output_tokens": 20,
			"cache_read_input_tokens": 800,
			"cache_creation_input_tokens": 300
		}
	}`)
	le := &storage.RequestLog{}
	parseUsageFromResponse(body, le, "anthropic-messages")
	if le.InputTokens != 100 || le.OutputTokens != 20 || le.CachedTokens != 800 || le.CacheWriteTokens != 300 || le.TotalTokens != 120 {
		t.Fatalf("anthropic mismatch input=%d output=%d cached=%d cache_write=%d total=%d",
			le.InputTokens, le.OutputTokens, le.CachedTokens, le.CacheWriteTokens, le.TotalTokens)
	}
}

func TestParseUsageFromResponse_Anthropic_NoWrite(t *testing.T) {
	body := []byte(`{
		"usage": {
			"input_tokens": 50,
			"output_tokens": 10,
			"cache_read_input_tokens": 0,
			"cache_creation_input_tokens": 0
		}
	}`)
	le := &storage.RequestLog{}
	parseUsageFromResponse(body, le, "anthropic-messages")
	if le.CachedTokens != 0 || le.CacheWriteTokens != 0 {
		t.Fatalf("expected 0/0 got cached=%d cache_write=%d", le.CachedTokens, le.CacheWriteTokens)
	}
}

func TestParseUsageFromResponse_OpenAI_NoCacheWrite(t *testing.T) {
	// Sanity: OpenAI protocols never populate CacheWriteTokens.
	chatBody := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":8}}}`)
	le := &storage.RequestLog{}
	parseUsageFromResponse(chatBody, le, "openai-chat-completions")
	if le.CacheWriteTokens != 0 {
		t.Fatalf("chat cache_write should stay 0 got %d", le.CacheWriteTokens)
	}
	respBody := []byte(`{"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":8}}}`)
	le2 := &storage.RequestLog{}
	parseUsageFromResponse(respBody, le2, "openai-responses")
	if le2.CacheWriteTokens != 0 {
		t.Fatalf("responses cache_write should stay 0 got %d", le2.CacheWriteTokens)
	}
}
