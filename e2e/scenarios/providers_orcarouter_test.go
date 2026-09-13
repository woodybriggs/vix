package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/get-vix/vix/e2e/harness"
)

// TestOrcaRouterBuiltinProviderConfigured verifies the OrcaRouter provider that
// now ships built-in (internal/providers/providers.json) is wired all the way
// into the TUI Models tab. With ORCAROUTER_API_KEY set, its env-var credential
// method must resolve and place "OrcaRouter" under the "Logged in:" group — no
// providers.json overlay required, proving the shipped entry is picked up out of
// the box. Its base_url (https://api.orcarouter.ai/v1) is never contacted; the
// scenario exercises credential availability, not a request.
func TestOrcaRouterBuiltinProviderConfigured(t *testing.T) {
	meta := harness.Meta{
		Category:    "providers",
		Subcategory: "providers.orcarouter",
		Description: "built-in OrcaRouter provider resolves its env-var key and lists under \"Logged in:\" in the Models tab",
		Wire:        harness.WireMessages,
	}
	h := harness.Start(t, meta,
		harness.WithEnv("ORCAROUTER_API_KEY", "orca-test-secret"),
	)
	h.UI.WaitStable(400 * time.Millisecond)

	// Open the Models tab and let the credential status load.
	h.UI.Key("f3")
	h.UI.WaitFor("OrcaRouter")
	h.UI.WaitStable(400 * time.Millisecond)
	h.UI.Shot("models-tab")

	s := h.UI.Snapshot()
	iLoggedIn := strings.Index(s, "Logged in:")
	iAvailable := strings.Index(s, "Available:")
	iOrca := strings.Index(s, "OrcaRouter")
	if iLoggedIn < 0 || iAvailable < 0 || iOrca < 0 {
		t.Fatalf("Models tab missing expected sections; screen:\n%s", s)
	}
	// OrcaRouter must sit in the "Logged in:" group (env var resolved a
	// credential), i.e. between the two group headers — not under "Available:".
	if !(iLoggedIn < iOrca && iOrca < iAvailable) {
		t.Fatalf("OrcaRouter not under \"Logged in:\" (loggedIn=%d orca=%d available=%d); screen:\n%s",
			iLoggedIn, iOrca, iAvailable, s)
	}
}
