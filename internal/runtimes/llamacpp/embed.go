package llamacpp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// supportHTTP calls embedding and reranking servers. A batch of passages
// takes seconds on a CPU, so the timeout is generous.
var supportHTTP = &http.Client{Timeout: 2 * time.Minute}

// Embed returns one vector per input from a llama-server started with
// --embedding, in input order.
func Embed(ctx context.Context, endpoint string, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := postJSON(ctx, endpoint+"/v1/embeddings", map[string]any{"input": inputs}, &out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(inputs))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) || len(d.Embedding) == 0 {
			return nil, fmt.Errorf("llama-server returned an embedding for input %d of %d", d.Index, len(vecs))
		}
		vecs[d.Index] = d.Embedding
	}
	for i, v := range vecs {
		if v == nil {
			return nil, fmt.Errorf("llama-server returned no embedding for input %d", i)
		}
	}
	return vecs, nil
}

// Rerank scores each document's relevance to the query with a llama-server
// started with --reranking. Higher is more relevant; scores are in document
// order.
func Rerank(ctx context.Context, endpoint, query string, docs []string) ([]float64, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	var out struct {
		Results []struct {
			Index int     `json:"index"`
			Score float64 `json:"relevance_score"`
		} `json:"results"`
	}
	body := map[string]any{"query": query, "documents": docs, "top_n": len(docs)}
	if err := postJSON(ctx, endpoint+"/v1/rerank", body, &out); err != nil {
		return nil, err
	}
	scores := make([]float64, len(docs))
	seen := make([]bool, len(docs))
	for _, r := range out.Results {
		if r.Index < 0 || r.Index >= len(docs) {
			return nil, fmt.Errorf("llama-server returned a score for document %d of %d", r.Index, len(docs))
		}
		scores[r.Index], seen[r.Index] = r.Score, true
	}
	for i, ok := range seen {
		if !ok {
			return nil, fmt.Errorf("llama-server returned no score for document %d", i)
		}
	}
	return scores, nil
}

func postJSON(ctx context.Context, url string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(url, "/"), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := supportHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("llama-server error %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
