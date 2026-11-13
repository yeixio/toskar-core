package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A paired computer passes a raw request to its runtime and the answer back
// as the runtime streamed it, over the same signed link as chat; its own
// refusals come back with their code (Spread, #1135).
func TestRemoteRaw(t *testing.T) {
	srv, _, pm, peer := setupInternalServer(t)
	if err := storeTrustHelper(t, pm, peer); err != nil {
		t.Fatal(err)
	}
	var got RemoteRawRequest
	srv.deps.Raw = func(_ context.Context, req RemoteRawRequest) (*http.Response, error) {
		got = req
		if req.ModelID != "m1" {
			return nil, contracts.NewError("MODEL_NOT_FOUND", nil, errors.New(req.ModelID+" isn't installed on this computer"))
		}
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(string(req.Body), "bad") {
			rec.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(rec, `{"error":{"message":"bad request"}}`)
			return rec.Result(), nil
		}
		_, _ = io.WriteString(rec, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
		return rec.Result(), nil
	}
	// The routes are wired when the server is made.
	srv = NewInternalServer(srv.deps)
	ts := httptest.NewServer(srv.http.Handler)
	defer ts.Close()
	client := NewClient(ts.URL, peer, "local-node")

	body := json.RawMessage(`{"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"read"}}],"stream":true}`)
	resp, err := client.Raw(context.Background(), RemoteRawRequest{ModelID: "m1", Need: 4096, Body: body, RequesterNodeID: "peer-node"})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" || !strings.Contains(string(out), `"content":"hi"`) || !strings.Contains(string(out), "[DONE]") {
		t.Fatalf("answer: %d %q", resp.StatusCode, out)
	}
	if string(got.Body) != string(body) || got.Need != 4096 {
		t.Fatalf("sent: %+v", got)
	}

	// The runtime's own error passes as it gave it.
	resp, err = client.Raw(context.Background(), RemoteRawRequest{ModelID: "m1", Body: json.RawMessage(`{"messages":"bad"}`)})
	if err != nil {
		t.Fatal(err)
	}
	out, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(out), "bad request") {
		t.Fatalf("runtime error: %d %s", resp.StatusCode, out)
	}

	// A model it doesn't have is the paired computer's refusal, with a code.
	_, err = client.Raw(context.Background(), RemoteRawRequest{ModelID: "m2", Body: body})
	if code, _ := contracts.ErrorCode(err); code != "MODEL_NOT_FOUND" {
		t.Fatalf("missing model: %v (%q)", err, code)
	}

	// Unsigned, it's refused like any other node call.
	plain, _ := http.Post(ts.URL+"/internal/v1/raw", "application/json", strings.NewReader(`{}`))
	if plain.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned: %d", plain.StatusCode)
	}
	plain.Body.Close()
}
