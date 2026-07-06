package gateway

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lovelytoaster94/routerhub/internal/protocol"
	"github.com/lovelytoaster94/routerhub/internal/storage"
)

// TestProxyRequest_NonStreaming_GzippedUpstream is a regression test for the
// tokens=0 bug. The scenario:
//   - Client sends Accept-Encoding: gzip (as most SDKs do by default).
//   - Upstream honours it and returns a gzipped JSON body.
//
// Prior to the Accept-Encoding fix, we forwarded the client's header verbatim,
// so Go's transport did NOT auto-decompress the response body. The bytes fed
// into parseUsageFromResponse were still gzipped, json.Unmarshal silently
// failed, and every non-streaming request landed in the log with tokens=0.
//
// After the fix we strip Accept-Encoding on the outbound request, Go's
// transport transparently handles the gzip round trip, and usage parsing works.
func TestProxyRequest_NonStreaming_GzippedUpstream(t *testing.T) {
	// Build a gzipped JSON body advertised as Content-Encoding: gzip. This
	// mirrors what a real upstream (e.g. Anthropic behind CloudFront) returns.
	respBody := `{"usage":{"prompt_tokens":123,"completion_tokens":45,"total_tokens":168,"prompt_tokens_details":{"cached_tokens":50}}}`
	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	if _, err := gzw.Write([]byte(respBody)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	compressed := gzBuf.Bytes()

	// The upstream must both:
	//   1) refuse to compress when the request lacks Accept-Encoding: gzip
	//      (proving that the header is what triggers compression); and
	//   2) send gzip when the header is present.
	// Real upstreams behave this way; we mimic it so the test would fail on
	// the old code (which forwarded the client header) and pass on the new
	// code (where we strip it, letting Go's transport add its own).
	var upstreamSawAE string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamSawAE = r.Header.Get("Accept-Encoding")
		// Go's transport auto-negotiates gzip when the caller has NOT set
		// Accept-Encoding itself. Regardless of what value we see, only
		// respond with gzip when the token appears somewhere in the header,
		// because that is the exact condition under which the old code
		// mishandled the body.
		if strings.Contains(upstreamSawAE, "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(compressed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respBody))
	}))
	defer upstream.Close()

	selected := &SelectedProvider{
		Provider: storage.Provider{
			Name:    "test",
			Type:    protocol.ProtocolChatCompletions,
			BaseURL: upstream.URL,
			APIKey:  "sk-test",
		},
		ProviderModel: "gpt-test",
	}

	// Build a client request that carries Accept-Encoding: gzip, exactly as
	// most SDKs do. The body just needs to be a JSON object containing a
	// "model" field so replaceModelInBody can round-trip it.
	inBody, _ := json.Marshal(map[string]any{
		"model":    "gpt-test",
		"messages": []any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(inBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	rr := httptest.NewRecorder()
	logEntry := &storage.RequestLog{RequestID: "test-req-1"}

	ProxyRequest(rr, req, selected, protocol.ProtocolChatCompletions, logEntry, false)

	// Sanity: our upstream saw the gzip token (Go's transport still adds
	// "gzip" itself when the caller didn't set Accept-Encoding), and returned
	// a gzipped body.
	if !strings.Contains(upstreamSawAE, "gzip") {
		t.Fatalf("upstream should have seen an Accept-Encoding advertising gzip, got %q", upstreamSawAE)
	}

	// The critical assertion: usage was parsed. This fails on the pre-fix
	// code because parseUsageFromResponse would have received gzipped bytes.
	if logEntry.InputTokens != 123 || logEntry.OutputTokens != 45 || logEntry.CachedTokens != 50 || logEntry.TotalTokens != 168 {
		t.Fatalf("usage parsing broken: input=%d output=%d cached=%d total=%d",
			logEntry.InputTokens, logEntry.OutputTokens, logEntry.CachedTokens, logEntry.TotalTokens)
	}
	if logEntry.Status != "success" {
		t.Fatalf("expected success status, got %q", logEntry.Status)
	}

	// And the client-visible body must be the plaintext JSON (Go's transport
	// stripped Content-Encoding when it auto-decompressed).
	if got := rr.Body.String(); !strings.Contains(got, `"prompt_tokens":123`) {
		t.Fatalf("client body should be decompressed JSON, got %q", got)
	}
	if ce := rr.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("Content-Encoding should have been stripped by Go's transport, got %q", ce)
	}
}

// TestProxyRequest_StripsClientAcceptEncoding directly verifies that the
// outbound request does not carry the client's Accept-Encoding header. Even
// if a future refactor rearranged the header-copy loop, this test would catch
// a regression before it reaches production.
func TestProxyRequest_StripsClientAcceptEncoding(t *testing.T) {
	var outboundAE string
	var outboundAECount int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read raw header, not the canonical accessor, so we see exactly what
		// arrived on the wire (Go still auto-adds one on our behalf, but the
		// client value "gzip, deflate, br, zstd" must not appear).
		outboundAE = r.Header.Get("Accept-Encoding")
		outboundAECount = len(r.Header.Values("Accept-Encoding"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()

	selected := &SelectedProvider{
		Provider: storage.Provider{
			Name: "test", Type: protocol.ProtocolChatCompletions,
			BaseURL: upstream.URL, APIKey: "sk-test",
		},
		ProviderModel: "gpt-test",
	}
	inBody, _ := json.Marshal(map[string]any{"model": "gpt-test"})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(inBody))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately exotic value the client "wants" — none of these should be
	// negotiated end-to-end.
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")

	rr := httptest.NewRecorder()
	ProxyRequest(rr, req, selected, protocol.ProtocolChatCompletions, &storage.RequestLog{RequestID: "test-req-2"}, false)

	// The upstream should have received AT MOST one Accept-Encoding value,
	// and that value must be whatever Go's transport chose (currently "gzip"),
	// never the client's multi-value string.
	if outboundAECount > 1 {
		t.Fatalf("upstream saw multiple Accept-Encoding headers (%d); the client's must have been forwarded", outboundAECount)
	}
	if strings.Contains(outboundAE, "br") || strings.Contains(outboundAE, "zstd") || strings.Contains(outboundAE, "deflate") {
		t.Fatalf("outbound Accept-Encoding leaked client codings: %q", outboundAE)
	}
}
