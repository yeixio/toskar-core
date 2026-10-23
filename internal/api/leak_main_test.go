package api

import (
	"fmt"
	"os"
	"testing"

	"github.com/yeixio/toskar-core/internal/leakcheck"
)

// Goroutines the tests start must be gone when they end (#231), and every
// response they see must be one api/openapi.yaml describes (#666).
func TestMain(m *testing.M) {
	if err := watchOpenAPIResponses(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := leakcheck.Run(m)
	if code == 0 {
		code = checkOpenAPIDrift()
	}
	os.Exit(code)
}
