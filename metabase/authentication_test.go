package metabase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testExtraHeaderName = "CF-Access-Client-Id"
const testExtraHeaderValue = "client-id"

// Returns a test server standing in for Metabase, along with the headers of every request it receives.
func makeRecordingServer(t *testing.T) (*httptest.Server, *[]http.Header) {
	t.Helper()

	var received []http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Header.Clone())
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/session" {
			_, _ = w.Write([]byte(`{"id":"session-id"}`))
			return
		}

		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	return server, &received
}

// Sets the extra header a proxy in front of Metabase would expect.
func setTestExtraHeader(ctx context.Context, req *http.Request) error {
	req.Header.Set(testExtraHeaderName, testExtraHeaderValue)
	return nil
}

func TestMakeAuthenticatedClientWithApiKeyAppliesOptions(t *testing.T) {
	server, received := makeRecordingServer(t)

	client, err := MakeAuthenticatedClientWithApiKey(context.Background(), server.URL, "api-key", WithRequestEditorFn(setTestExtraHeader))
	if err != nil {
		t.Fatalf("failed to create the client: %v", err)
	}

	// Only the headers of the request matter here, not the response, which the test server does not model.
	_, _ = client.ListTablesWithResponse(context.Background())

	if len(*received) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*received))
	}
	if got := (*received)[0].Get(testExtraHeaderName); got != testExtraHeaderValue {
		t.Errorf("expected the extra header to be set, got %q", got)
	}
	if got := (*received)[0].Get("X-Api-Key"); got != "api-key" {
		t.Errorf("expected the API key header to be preserved, got %q", got)
	}
}

func TestMakeAuthenticatedClientWithUsernameAndPasswordAppliesOptions(t *testing.T) {
	server, received := makeRecordingServer(t)

	client, err := MakeAuthenticatedClientWithUsernameAndPassword(context.Background(), server.URL, "user", "password", WithRequestEditorFn(setTestExtraHeader))
	if err != nil {
		t.Fatalf("failed to create the client: %v", err)
	}

	_, _ = client.ListTablesWithResponse(context.Background())

	if len(*received) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(*received))
	}
	// The session request is made before the client is authenticated. A proxy in front of Metabase has to see the extra
	// headers on that request too, otherwise authentication never gets through.
	for i, headers := range *received {
		if got := headers.Get(testExtraHeaderName); got != testExtraHeaderValue {
			t.Errorf("expected the extra header on request %d, got %q", i, got)
		}
	}
	if got := (*received)[1].Get("X-Metabase-Session"); got != "session-id" {
		t.Errorf("expected the session header to be preserved, got %q", got)
	}
}
