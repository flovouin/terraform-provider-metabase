package provider

import (
	"strings"
	"testing"
)

func TestSummarizeBody(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 600)

	tests := map[string]struct {
		body     string
		expected string
	}{
		"cause is preferred": {
			body:     `{"message":"outer","cause":"inner","via":[{"data":{"password":"secret"}}]}`,
			expected: "inner",
		},
		"message": {
			body:     `{"message":"Assert failed: (every? some? [a b])","via":[{"data":{"password":"secret"}}]}`,
			expected: "Assert failed: (every? some? [a b])",
		},
		"short plain text": {
			body:     "Bad gateway",
			expected: "Bad gateway",
		},
		"long plain text is truncated": {
			body:     long,
			expected: long[:500] + "...(truncated)",
		},
		"json without message is truncated": {
			body:     `{"errors":"` + long + `"}`,
			expected: (`{"errors":"` + long)[:500] + "...(truncated)",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if actual := summarizeBody(test.body); actual != test.expected {
				t.Fatalf("Unexpected summary: got %q, expected %q", actual, test.expected)
			}
		})
	}
}
