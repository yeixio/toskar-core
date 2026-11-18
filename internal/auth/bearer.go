package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ErrAPIKeyRequired means a non-loopback listener has no API key configured.
var ErrAPIKeyRequired = contracts.NewError("API_KEY_REQUIRED", nil, errors.New("create an API key or add a person before listening beyond loopback"))

// ErrAPIKeyInURL means the caller put a credential in the request URL.
var ErrAPIKeyInURL = contracts.NewError("API_KEY_IN_URL", nil, errors.New("send the API key as Authorization: Bearer, not in the URL"))

// BearerToken reads an API key from the Authorization header, or, without
// one, from x-api-key, as Anthropic's clients send it (#843), or, on a
// WebSocket, from an openai-insecure-api-key.<key> subprotocol: a browser
// can't set headers on a WebSocket, so OpenAI's Realtime clients send it
// there (#795).
// Keys in the query string are rejected and are not accepted as credentials.
func BearerToken(r *http.Request) (string, error) {
	q := r.URL.Query()
	for _, name := range []string{"api_key", "access_token", "token", "key"} {
		if q.Get(name) != "" {
			return "", ErrAPIKeyInURL
		}
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if header == "" {
		// Anthropic's clients send the key as x-api-key (#843).
		if key := strings.TrimSpace(r.Header.Get("X-Api-Key")); key != "" {
			return key, nil
		}
		if key := subprotocolKey(r); key != "" {
			return key, nil
		}
	}
	if !strings.HasPrefix(header, prefix) {
		return "", fmt.Errorf("authorization required")
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", fmt.Errorf("authorization required")
	}
	return token, nil
}

// keyProtocol is the subprotocol prefix a browser's WebSocket sends its key in.
const keyProtocol = "openai-insecure-api-key."

func subprotocolKey(r *http.Request) string {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return ""
	}
	for _, h := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(h, ",") {
			if key, ok := strings.CutPrefix(strings.TrimSpace(p), keyProtocol); ok && key != "" {
				return key
			}
		}
	}
	return ""
}
