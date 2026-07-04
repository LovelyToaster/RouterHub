package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDistEmbedded(t *testing.T) {
	// Verify that the embedded dist contains expected files
	subFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatalf("fs.Sub(dist, \"dist\") failed: %v", err)
	}

	// Check index.html exists
	_, err = subFS.Open("index.html")
	if err != nil {
		t.Fatalf("index.html not found in embedded dist: %v", err)
	}

	// Check assets directory exists
	_, err = subFS.Open("assets")
	if err != nil {
		t.Fatalf("assets directory not found in embedded dist: %v", err)
	}
}

func TestStaticHandler_ServesIndexHtml(t *testing.T) {
	handler := StaticHandler()

	// Request root path
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Fatal("expected non-empty body")
	}
}

func TestStaticHandler_SpaFallback(t *testing.T) {
	handler := StaticHandler()

	// SPA routes should return index.html content
	routes := []string{"/setup", "/login", "/app/dashboard", "/app/providers", "/unknown/path"}
	for _, route := range routes {
		req := httptest.NewRequest("GET", route, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("SPA route %s: expected 200 OK, got %d", route, w.Code)
		}
		if len(w.Body.String()) == 0 {
			t.Errorf("SPA route %s: expected non-empty body", route)
		}
	}
}

func TestStaticHandler_ServesStaticFiles(t *testing.T) {
	handler := StaticHandler()

	// Static asset files should be served directly
	req := httptest.NewRequest("GET", "/vite.svg", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for vite.svg, got %d", w.Code)
	}

	// Check Content-Type header
	ct := w.Header().Get("Content-Type")
	if ct == "" {
		t.Fatal("expected Content-Type header")
	}
}

func TestStaticHandler_ApiRoutesNotHandled(t *testing.T) {
	// API routes should not be handled by the static handler
	// (they are handled by chi routes before NotFound)
	handler := StaticHandler()

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// The static handler should serve index.html (SPA fallback) for /api/health
	// because it doesn't know about API routes - the chi router handles them first
	// This test just verifies the handler doesn't crash
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK (SPA fallback), got %d", w.Code)
	}
}
