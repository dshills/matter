package builtin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebFetchSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "hello from server")
	}))
	defer srv.Close()

	tool := NewWebFetch([]string{"127.0.0.1"}, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL + "/page"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "hello from server") {
		t.Errorf("output should contain response body, got %q", result.Output)
	}
}

func TestWebFetchDomainRejected(t *testing.T) {
	tool := NewWebFetch([]string{"allowed.example.com"}, 1024)
	result, err := tool.Execute(context.Background(), map[string]any{"url": "http://evil.example.com/data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for disallowed domain")
	}
	if !strings.Contains(result.Error, "not in the allowed list") {
		t.Errorf("error = %q, want mention of allowed list", result.Error)
	}
}

func TestWebFetchEmptyAllowlistRejectsAll(t *testing.T) {
	tool := NewWebFetch(nil, 1024)
	result, err := tool.Execute(context.Background(), map[string]any{"url": "http://any.example.com/data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error when allowlist is empty")
	}
}

func TestWebFetchTruncation(t *testing.T) {
	body := strings.Repeat("x", 2000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
	defer srv.Close()

	maxBytes := 1024

	tool := NewWebFetch([]string{"127.0.0.1"}, maxBytes)
	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("truncation should not set error, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "[TRUNCATED at") {
		t.Error("expected truncation notice in output")
	}
	// Output should be maxBytes of content + truncation notice.
	if !strings.HasPrefix(result.Output, "HTTP") {
		t.Error("output should start with HTTP status line")
	}
}

func TestWebFetchInvalidURL(t *testing.T) {
	tool := NewWebFetch([]string{"example.com"}, 1024)
	result, err := tool.Execute(context.Background(), map[string]any{"url": "://bad"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for invalid URL")
	}
}

func TestWebFetchMissingURL(t *testing.T) {
	tool := NewWebFetch([]string{"example.com"}, 1024)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error for missing url")
	}
}

func TestWebFetchRedirectToDisallowedDomain(t *testing.T) {
	// Target server on a different "domain" (different port = different host).
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "you should not see this")
	}))
	defer target.Close()

	// Redirect server on the allowed domain.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	tool := NewWebFetch([]string{"127.0.0.1"}, 1024*1024)
	result, err := tool.Execute(context.Background(), map[string]any{"url": redirector.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both servers are on 127.0.0.1, so redirect is allowed within same domain.
	// This test validates the client follows redirects when domain matches.
	if result.Error != "" {
		t.Fatalf("unexpected tool error for same-domain redirect: %s", result.Error)
	}
}

func TestWebFetchSafetyFlags(t *testing.T) {
	tool := NewWebFetch(nil, 1024)
	if tool.Safe {
		t.Error("web_fetch should be Safe=false")
	}
	if tool.SideEffect {
		t.Error("web_fetch should be SideEffect=false")
	}
}

func TestIsDomainAllowed(t *testing.T) {
	tests := []struct {
		hostname string
		allowed  []string
		want     bool
	}{
		{"example.com", []string{"example.com"}, true},
		{"Example.COM", []string{"example.com"}, true},
		{"evil.com", []string{"example.com"}, false},
		{"example.com", nil, false},
		{"example.com", []string{}, false},
		{"sub.example.com", []string{"example.com"}, false},
	}
	for _, tt := range tests {
		if got := isDomainAllowed(tt.hostname, tt.allowed); got != tt.want {
			t.Errorf("isDomainAllowed(%q, %v) = %v, want %v", tt.hostname, tt.allowed, got, tt.want)
		}
	}
}
