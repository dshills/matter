package builtin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

// WebFetchSchema is the JSON Schema for the web_fetch tool.
var WebFetchSchema = []byte(`{
	"type": "object",
	"properties": {
		"url": {
			"type": "string",
			"description": "The URL to fetch via HTTP GET"
		}
	},
	"required": ["url"],
	"additionalProperties": false
}`)

// NewWebFetch creates the web_fetch tool with the given domain allowlist and
// response size limit.
func NewWebFetch(allowedDomains []string, maxResponseBytes int) matter.Tool {
	return matter.Tool{
		Name:        "web_fetch",
		Description: "Fetch a URL via HTTP GET. Only allowed domains are permitted. Large responses are truncated.",
		InputSchema: WebFetchSchema,
		Timeout:     30 * time.Second,
		Safe:        false,
		SideEffect:  false,
		Execute:     webFetchFunc(allowedDomains, maxResponseBytes),
	}
}

// newHTTPClient creates an isolated HTTP client that validates redirect
// targets against the domain allowlist to prevent SSRF via open redirects.
func newHTTPClient(allowedDomains []string) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects (%d)", len(via))
			}
			if !isDomainAllowed(req.URL.Hostname(), allowedDomains) {
				return fmt.Errorf("redirect to disallowed domain %q", req.URL.Hostname())
			}
			return nil
		},
	}
}

func webFetchFunc(allowedDomains []string, maxResponseBytes int) matter.ToolExecuteFunc {
	client := newHTTPClient(allowedDomains)

	return func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
		rawURL, ok := input["url"].(string)
		if !ok || rawURL == "" {
			return matter.ToolResult{Error: "url is required and must be a string"}, nil
		}

		parsed, err := url.Parse(rawURL)
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("invalid URL: %s", err)}, nil
		}

		// Check domain allowlist.
		if !isDomainAllowed(parsed.Hostname(), allowedDomains) {
			return matter.ToolResult{Error: fmt.Sprintf("domain %q is not in the allowed list", parsed.Hostname())}, nil
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("failed to create request: %s", err)}, nil
		}

		resp, err := client.Do(req)
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("fetch failed: %s", err)}, nil
		}
		defer func() { _ = resp.Body.Close() }()

		// Read up to maxResponseBytes + 1 to detect truncation.
		limit := maxResponseBytes + 1
		body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)))
		if err != nil {
			return matter.ToolResult{Error: fmt.Sprintf("failed to read response: %s", err)}, nil
		}

		truncated := len(body) > maxResponseBytes
		if truncated {
			body = body[:maxResponseBytes]
		}

		output := fmt.Sprintf("HTTP %s\n\n%s", resp.Status, string(body))
		if truncated {
			output += fmt.Sprintf("\n[TRUNCATED at %dKB]", maxResponseBytes/1024)
		}

		return matter.ToolResult{Output: output}, nil
	}
}

// isDomainAllowed checks if a hostname is in the allowlist.
// An empty allowlist rejects all requests.
func isDomainAllowed(hostname string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	hostname = strings.ToLower(hostname)
	for _, d := range allowed {
		if strings.ToLower(d) == hostname {
			return true
		}
	}
	return false
}
