package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
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

// A private state storing its values in memory.
type fakePrivateState map[string][]byte

func (f fakePrivateState) GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics) {
	return f[key], nil
}

func (f fakePrivateState) SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics {
	f[key] = value
	return nil
}

func TestPrivateState(t *testing.T) {
	t.Parallel()

	type value struct {
		Name string `json:"name"`
	}
	private := fakePrivateState{"invalid": []byte(`{`)}
	key := privateStateKey[value]("key")
	invalidKey := privateStateKey[value]("invalid")

	missing, diags := readPrivateState(context.Background(), private, key)
	if diags.HasError() || missing != nil {
		t.Fatalf("Expected no value, got %v (%v).", missing, diags)
	}

	if diags := writePrivateState(context.Background(), private, key, value{Name: "stored"}); diags.HasError() {
		t.Fatalf("Unexpected error: %v", diags)
	}
	stored, diags := readPrivateState(context.Background(), private, key)
	if diags.HasError() || stored == nil || stored.Name != "stored" {
		t.Errorf("Expected the stored value, got %v (%v).", stored, diags)
	}

	if _, diags := readPrivateState(context.Background(), private, invalidKey); !diags.HasError() {
		t.Errorf("Expected an error for an invalid value.")
	}
}
