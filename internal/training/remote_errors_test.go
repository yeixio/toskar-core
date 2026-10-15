package training

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A computer's refusal carries its code beside the text, which computers
// from before codes still read, and the asking computer keeps it (#588).
func TestRemoteTrainingErrorsAreCoded(t *testing.T) {
	rr := httptest.NewRecorder()
	remoteErr(rr, http.StatusConflict, "REMOTE_TRAINING_FAILED", contracts.NewError("GPU_BUSY", map[string]any{"run": "r1"}, errors.New("the graphics card is busy")))
	err := decodePeer(rr.Result(), nil)
	code, details := contracts.ErrorCode(err)
	if code != "GPU_BUSY" || details["run"] != "r1" || err.Error() != "the graphics card is busy" {
		t.Fatalf("read as %q %s %v", err, code, details)
	}
	rr = httptest.NewRecorder()
	remoteErr(rr, http.StatusNotFound, "REMOTE_TRAINING_FAILED", errors.New("run not found"))
	if code, _ := contracts.ErrorCode(decodePeer(rr.Result(), nil)); code != "REMOTE_TRAINING_FAILED" {
		t.Fatalf("fallback code: %s", code)
	}
}
