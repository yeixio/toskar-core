package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/yeixio/toskar-core/internal/auth"
)

// RawPrefix names a model driven directly (#843): raw:<model id>.
const RawPrefix = "raw:"

// maxRequestBytes is the most a chat completion's body may be: room for a
// coding agent's long conversation and pictures sent as data URLs.
const maxRequestBytes = 64 << 20

// Errors Raw returns, for the status a raw request answers with.
var (
	// ErrRawNotHere is a model that isn't installed on this computer: a raw
	// request drives a model here, not on a paired computer.
	ErrRawNotHere = errors.New("that model isn't installed on this computer")
	// ErrRawTooLong is a request no window of the model holds here.
	ErrRawTooLong = errors.New("CONTEXT_TOO_LONG")
)

// rawClient sends raw requests to llama-server; a variable so tests can
// stand in. It has no timeout: an answer streams for as long as it takes,
// and the request's context ends it.
var rawClient = &http.Client{}

// handleRaw serves a raw:<model> request (#843): the model itself, without
// Toskar's assistant, for coding agents and editors that run their own
// tools. The request goes to the model's llama-server as it came, with
// tools, tool_choice, stop, logprobs, and the rest, and the answer comes
// back as llama-server gives it, streamed or not, with tool_calls, named
// raw:<model>. Only a key an Admin allowed it, or a request on this computer
// without a key, may.
func (h *Handler) handleRaw(w http.ResponseWriter, r *http.Request, perms auth.APIKeyPermissions, req chatCompletionRequest, body []byte, maxTokens int) {
	if !perms.Raw || perms.Profile != "" {
		writeError(w, http.StatusForbidden, "RAW_NOT_ALLOWED", "this key can't drive a model directly; an Admin can allow raw model access for it in API Access")
		return
	}
	modelID := strings.TrimPrefix(req.Model, RawPrefix)
	if modelID == "" {
		writeErrorParam(w, http.StatusBadRequest, "INVALID_REQUEST", "name the model after raw:, such as raw:qwen2.5-7b-q4", "model")
		return
	}
	if h.Raw == nil {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "raw model access isn't available")
		return
	}
	least := 0
	if o := req.assistant(); o != nil {
		least = o.ContextWindow
	}
	endpoint, done, err := h.Raw(r.Context(), modelID, max(neededWindow(req, maxTokens), least))
	switch {
	case errors.Is(err, ErrRawNotHere):
		writeErrorParam(w, http.StatusNotFound, "MODEL_NOT_FOUND", modelID+": "+err.Error(), "model")
		return
	case errors.Is(err, ErrRawTooLong):
		writeErrorParam(w, http.StatusBadRequest, "CONTEXT_TOO_LONG", strings.TrimPrefix(err.Error(), "CONTEXT_TOO_LONG: "), "messages")
		return
	case err != nil:
		status, code := h.failure("", err.Error())
		writeError(w, status, code, err.Error())
		return
	}
	defer done()

	forward, err := rawBody(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	resp, err := rawPost(r.Context(), endpoint, forward)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusBadGateway, "RUNTIME_ERROR", "the model didn't answer: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 || !req.Stream {
		raw, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(renamed(raw, req.Model))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if data, ok := bytes.CutPrefix(line, []byte("data: ")); ok && !bytes.Equal(data, []byte("[DONE]")) {
			line = append([]byte("data: "), renamed(data, req.Model)...)
		}
		_, _ = w.Write(append(line, '\n'))
		if len(line) == 0 && flusher != nil {
			flusher.Flush()
		}
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// rawBody is the request as llama-server takes it: as sent, less Toskar's
// own extension object, which is for the assistant.
func rawBody(body []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	delete(m, "toskar")
	delete(m, "yggdrasil")
	return json.Marshal(m)
}

func rawPost(ctx context.Context, endpoint string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return rawClient.Do(req)
}

// renamed is a llama-server answer or chunk with its model named as the
// request named it, raw:<model>; anything else passes as it is.
func renamed(raw []byte, model string) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if _, ok := m["model"]; !ok {
		return raw
	}
	name, _ := json.Marshal(model)
	m["model"] = name
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}
