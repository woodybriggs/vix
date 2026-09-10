package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/get-vix/vix/e2e/harness"
)

// TestSessionHeaderOnRequest proves the full session-header contract end-to-end:
// a provider with session_header configured in providers.json produces an
// outbound LLM request that carries the session ID as an HTTP header. The
// harness overlays the Opencode provider to inject session_header, runs a
// normal turn, and asserts the mock server received x-opencode-session.
//
// This covers the chain: providers.json → InferenceSpec.Resolve() →
// NewFromModel → PluginConfig.SessionHeader → headerStripperTransport →
// HTTP request → mock server.
func TestSessionHeaderOnRequest(t *testing.T) {
	h := harness.Start(t, harness.Meta{
		Category:    "session",
		Subcategory: "session.header_request",
		Description: "session_header from provider config lands as an HTTP header on outbound LLM requests",
		Wire:        harness.WireMessages,
	}, harness.WithProviders(`{
		"schema_version": 1,
		"providers": [
			{
				"id": "opencode",
				"inference": {
					"session_header": "x-opencode-session"
				}
			}
		]
	}`))

	h.UI.WaitStable(500 * time.Millisecond)
	h.UI.Shot("initial")

	h.Mock.Enqueue(harness.Text("Session header received."))
	h.UI.Type("say hello")
	h.UI.Enter()
	h.UI.WaitFor("Session header received.")
	h.UI.WaitStable(300 * time.Millisecond)
	h.UI.Shot("after-turn")

	// Assert: at least one request arrived at the mock with the session header.
	reqs := h.Mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no mock requests recorded")
	}

	found := false
	for _, r := range reqs {
		if sid := r.Headers.Get("x-opencode-session"); sid != "" {
			found = true
			// Session ID must be a UUID (the thread ID).
			if !strings.Contains(sid, "-") {
				t.Errorf("session header value %q does not look like a UUID", sid)
			}
			break
		}
	}
	if !found {
		var got []string
		for _, r := range reqs {
			got = append(got, r.Headers.Get("x-opencode-session"))
		}
		t.Fatalf("no request carried x-opencode-session header; got %v", got)
	}
}
