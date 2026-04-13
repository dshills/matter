// Package planner implements the LLM decision engine with JSON parsing and repair.
package planner

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/dshills/matter/internal/llm"
	"github.com/dshills/matter/pkg/matter"
)

// fenceRe matches markdown code fences (```json ... ``` or ``` ... ```).
var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*\n?(.*?)\\s*```")

// localCleanup applies heuristic repairs to malformed JSON:
//   - strips markdown code fences
//   - trims leading/trailing whitespace
//   - fixes trailing commas before } or ] (string-aware)
//   - appends missing closing braces/brackets
func localCleanup(raw string) string {
	s := raw

	// Strip markdown code fences.
	if m := fenceRe.FindStringSubmatch(s); len(m) > 1 {
		s = m[1]
	}

	s = strings.TrimSpace(s)

	// Close missing delimiters first, then fix trailing commas.
	// This order handles truncated input like `{"key": "val",`
	// which becomes `{"key": "val",}` then `{"key": "val"}`.
	s = closeDelimiters(s)
	s = fixTrailingCommas(s)

	return s
}

// fixTrailingCommas removes commas immediately before } or ] while
// respecting string literals (commas inside strings are not touched).
func fixTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	inString := false
	escape := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if escape {
			b.WriteByte(ch)
			escape = false
			continue
		}
		if ch == '\\' && inString {
			b.WriteByte(ch)
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			b.WriteByte(ch)
			continue
		}
		if inString {
			b.WriteByte(ch)
			continue
		}

		// Outside a string: check for trailing comma before } or ].
		if ch == ',' {
			// Look ahead past whitespace for } or ].
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				// Skip the comma (and whitespace — the closer will be written next iteration).
				continue
			}
		}

		b.WriteByte(ch)
	}

	return b.String()
}

// closeDelimiters uses a stack to track unmatched { and [ delimiters and
// appends their closing counterparts in correct LIFO order.
// Also closes unterminated string literals.
func closeDelimiters(s string) string {
	var stack []byte
	inString := false
	escape := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if escape {
			escape = false
			continue
		}
		if ch == '\\' && inString {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 && stack[len(stack)-1] == ch {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if len(stack) == 0 && !inString {
		return s
	}

	var b strings.Builder
	b.WriteString(s)
	// Close unterminated string literal.
	if inString {
		b.WriteByte('"')
	}
	// Close delimiters in reverse (LIFO) order.
	for i := len(stack) - 1; i >= 0; i-- {
		b.WriteByte(stack[i])
	}
	return b.String()
}

// llmCorrection sends a correction prompt to the LLM to fix malformed JSON.
// Returns the corrected raw string. At most one correction attempt per step.
func llmCorrection(ctx context.Context, client llm.Client, raw string) (string, llm.Response, error) {
	req := llm.Request{
		Messages: []matter.Message{
			{
				Role: matter.RoleSystem,
				Content: "You are a JSON repair assistant. The user will provide malformed JSON " +
					"that should represent a decision object with fields: type (tool|complete|fail), " +
					"reasoning (string), tool_call (object with name and input, required when type=tool), " +
					"final (object with summary, required when type=complete or type=fail). " +
					"Return ONLY the corrected valid JSON with no explanation or markdown.",
			},
			{
				Role:    matter.RoleUser,
				Content: fmt.Sprintf("Fix this malformed JSON:\n%s", raw),
			},
		},
		MaxTokens:   1000,
		Temperature: 0,
	}

	resp, err := client.Complete(ctx, req)
	if err != nil {
		return "", resp, fmt.Errorf("LLM correction failed: %w", err)
	}

	return strings.TrimSpace(resp.Content), resp, nil
}
