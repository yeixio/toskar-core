package nodes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Client calls authenticated internal node RPC.
type Client struct {
	baseURL  string
	http     *http.Client
	identity *auth.NodeIdentity
	peerID   string // the paired computer's node ID, each token's audience
}

// NewClient calls the paired computer peerID at baseURL, signing each
// request with identity.
func NewClient(baseURL string, identity *auth.NodeIdentity, peerID string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		identity: identity,
		peerID:   peerID,
		http:     &http.Client{Timeout: 120 * time.Second},
	}
}

// NewProbeClient is a short-timeout client for pairing / discovery probes.
// It encrypts when the other computer speaks TLS, without checking its key,
// which the pairing steps check by signature themselves.
func NewProbeClient(baseURL string) *Client {
	base := strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: base,
		http:    &http.Client{Timeout: 5 * time.Second, Transport: newLinkTransport("probe "+hostOf(base), Link{})},
	}
}

// Secure reaches the paired computer over TLS checked against its key, or
// over plain HTTP as link allows (#175).
func (c *Client) Secure(link Link) *Client {
	hc := *c.http
	hc.Transport = newLinkTransport(c.peerID, link)
	c.http = &hc
	return c
}

// LinkClient is an HTTP client for a computer's Bifrost address that speaks
// TLS checked against link's pin, falling back as link allows, such as for
// the one-line join, which knows the issuer's fingerprint.
func LinkClient(key string, link Link, timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: newLinkTransport(key, link)}
}

func hostOf(base string) string {
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Host
	}
	return base
}

type NodeInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	CertPEM string `json:"cert_pem"`
}

func (c *Client) Health(ctx context.Context) error {
	_, err := c.HealthInfo(ctx)
	return err
}

// HealthInfo is the computer's health answer, including whether it is
// training.
func (c *Client) HealthInfo(ctx context.Context) (Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/v1/health", nil)
	if err != nil {
		return Health{}, err
	}
	c.applyAuth(req)
	resp, err := c.send(req)
	if err != nil {
		return Health{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("health HTTP %d", resp.StatusCode)
	}
	var h Health
	// An older computer answers without training; that reads as not training.
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&h)
	return h, nil
}

func (c *Client) NodeInfo(ctx context.Context) (NodeInfo, error) {
	var info NodeInfo
	if err := c.getJSON(ctx, "/internal/v1/node", &info, false); err != nil {
		return info, err
	}
	return info, nil
}

func (c *Client) Hardware(ctx context.Context) (contracts.HardwareInventory, error) {
	var inv contracts.HardwareInventory
	if err := c.getJSON(ctx, "/internal/v1/hardware", &inv, true); err != nil {
		return inv, err
	}
	return inv, nil
}

// Live is a paired computer's live CPU, memory, and GPU figures (#317).
func (c *Client) Live(ctx context.Context) (contracts.LiveFigures, error) {
	var out contracts.LiveFigures
	err := c.getJSON(ctx, "/internal/v1/live", &out, true)
	return out, err
}

func (c *Client) ListModels(ctx context.Context) ([]contracts.Model, error) {
	var out []contracts.Model
	if err := c.getJSON(ctx, "/internal/v1/models", &out, true); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListRunning(ctx context.Context) ([]contracts.RunningModelView, error) {
	var out []contracts.RunningModelView
	if err := c.getJSON(ctx, "/internal/v1/models/running", &out, true); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) InstallModel(ctx context.Context, modelID string, wait bool) error {
	body, _ := json.Marshal(map[string]any{"model_id": modelID, "wait": wait})
	return c.postJSON(ctx, "/internal/v1/models/install", body, nil, true)
}

func (c *Client) InstallFromURL(ctx context.Context, req contracts.InstallFromURLRequest) (string, error) {
	body, _ := json.Marshal(req)
	var out struct {
		ModelID string `json:"model_id"`
	}
	if err := c.postJSON(ctx, "/internal/v1/models/install-from-url", body, &out, true); err != nil {
		return "", err
	}
	return out.ModelID, nil
}

func (c *Client) DeleteModel(ctx context.Context, modelID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/internal/v1/models/"+modelID, nil)
	if err != nil {
		return err
	}
	c.applyAuth(req)
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return responseError(resp.StatusCode, b, false)
	}
	return nil
}

func (c *Client) StartModel(ctx context.Context, modelID string) (contracts.RunningModelView, error) {
	var out contracts.RunningModelView
	if err := c.postJSON(ctx, "/internal/v1/models/"+modelID+"/start", []byte("{}"), &out, true); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) StopInstance(ctx context.Context, instanceID string) error {
	return c.postJSON(ctx, "/internal/v1/models/instances/"+instanceID+"/stop", []byte("{}"), nil, true)
}

func (c *Client) SendPairingOffer(ctx context.Context, offer auth.PairingOffer) error {
	body, _ := json.Marshal(offer)
	return c.postJSON(ctx, "/internal/v1/pairing/offer", body, nil, false)
}

// SendControlPairingOffer posts an offer to the peer control-plane API (port 7331).
func (c *Client) SendControlPairingOffer(ctx context.Context, offer auth.PairingOffer) error {
	body, _ := json.Marshal(offer)
	return c.postJSON(ctx, "/api/v1/nodes/pairing/offer", body, nil, false)
}

// FetchOutboundOffer pulls a pending outbound offer by code from a peer.
func (c *Client) FetchOutboundOffer(ctx context.Context, code string) (auth.PairingOffer, error) {
	var offer auth.PairingOffer
	if err := c.getJSON(ctx, "/internal/v1/pairing/outbound/"+code, &offer, false); err != nil {
		return offer, err
	}
	return offer, nil
}

// FetchOutboundOfferControl pulls an outbound offer via the peer control API.
func (c *Client) FetchOutboundOfferControl(ctx context.Context, code string) (auth.PairingOffer, error) {
	var offer auth.PairingOffer
	if err := c.getJSON(ctx, "/api/v1/nodes/pairing/outbound/"+code, &offer, false); err != nil {
		return offer, err
	}
	return offer, nil
}

func (c *Client) CompletePairing(ctx context.Context, complete auth.PairingComplete) error {
	body, _ := json.Marshal(complete)
	return c.postJSON(ctx, "/internal/v1/pairing/complete", body, nil, false)
}

// RemoteChatRequest is sent to a peer for generation.
type RemoteChatRequest struct {
	ModelID           string                  `json:"model_id"`
	Messages          []pluginapi.ChatMessage `json:"messages"`
	Temperature       float64                 `json:"temperature,omitempty"`
	MaxTokens         int                     `json:"max_tokens,omitempty"`
	Role              string                  `json:"role,omitempty"`
	RequesterNodeID   string                  `json:"requester_node_id,omitempty"`
	RequesterNodeName string                  `json:"requester_node_name,omitempty"`
}

func (c *Client) Chat(ctx context.Context, req RemoteChatRequest) (<-chan pluginapi.ChatChunk, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson, application/json")
	c.applyAuth(httpReq)
	// Streaming may exceed default client timeout.
	streamClient := &http.Client{Timeout: 0, Transport: c.http.Transport}
	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, responseError(resp.StatusCode, b, true)
	}
	ch := make(chan pluginapi.ChatChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var chunk pluginapi.ChatChunk
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				ch <- pluginapi.ChatChunk{Error: err.Error(), Done: true}
				return
			}
			ch <- chunk
			if chunk.Done || chunk.Error != "" {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- pluginapi.ChatChunk{Error: err.Error(), Done: true}
		}
	}()
	return ch, nil
}

func (c *Client) getJSON(ctx context.Context, path string, v any, auth bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if auth {
		c.applyAuth(req)
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return responseError(resp.StatusCode, b, true)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (c *Client) postJSON(ctx context.Context, path string, body []byte, out any, auth bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		c.applyAuth(req)
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return responseError(resp.StatusCode, b, true)
	}
	if out == nil {
		_, err := io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Do sends an authenticated request to path, such as
// "/internal/v1/training/runs", and returns the raw response. The caller
// closes the body. A long-running transfer should use a context deadline;
// the client timeout is lifted for it.
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.applyAuth(req)
	hc := *c.http
	hc.Timeout = 0
	return hc.Do(req)
}

// send makes a request, asking for errors as JSON with a stable code
// (#588); a computer from before codes answers with text.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	return c.http.Do(req)
}

// responseError is a failed answer as an error: with the answering
// computer's code when it sent one, and its text, after the status when
// withStatus is set.
func responseError(status int, body []byte, withStatus bool) error {
	var coded contracts.APIError
	text := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &coded) == nil && coded.Error.Code != "" {
		text = coded.Error.Message
	}
	switch {
	case text == "":
		text = fmt.Sprintf("HTTP %d", status)
	case withStatus:
		text = fmt.Sprintf("HTTP %d: %s", status, text)
	}
	if coded.Error.Code == "" {
		return errors.New(text)
	}
	return contracts.NewError(coded.Error.Code, coded.Error.Details, errors.New(text))
}

func (c *Client) applyAuth(req *http.Request) {
	if c.identity != nil {
		req.Header.Set("Authorization", "Bearer "+c.identity.AuthToken(c.peerID))
	}
}
