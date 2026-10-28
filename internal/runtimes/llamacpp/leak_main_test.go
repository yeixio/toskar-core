package llamacpp

import (
	"os"
	"testing"

	"github.com/yeixio/toskar-core/internal/leakcheck"
)

// The test binary doubles as a fake llama-server (see reap_unix_test.go).
// Goroutines the tests start must be gone when they end (#231).
func TestMain(m *testing.M) {
	if os.Getenv(fakeLlamaEnv) != "" {
		runFakeLlama()
		return
	}
	// No test reads the canary's real hold from GitHub; hold_test.go sets its own.
	holdURL = ""
	leakcheck.Main(m)
}
