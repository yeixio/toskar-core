package auth

import (
	"errors"
	"net/http/httptest"
	"testing"
)

func TestBearerToken(t *testing.T) {
	req := httptest.NewRequest(httpMethod, "/api/v1/health", nil)
	if _, err := BearerToken(req); err == nil {
		t.Fatal("expected missing token to fail")
	}

	req.Header.Set("Authorization", "Bearer ygg_test")
	got, err := BearerToken(req)
	if err != nil || got != "ygg_test" {
		t.Fatalf("bearer: %q %v", got, err)
	}

	// Anthropic's clients send x-api-key; Authorization wins when both come.
	req = httptest.NewRequest(httpMethod, "/v1/messages", nil)
	req.Header.Set("X-Api-Key", "ygg_anthropic")
	if got, err := BearerToken(req); err != nil || got != "ygg_anthropic" {
		t.Fatalf("x-api-key: %q %v", got, err)
	}
	req.Header.Set("Authorization", "Bearer ygg_test")
	if got, _ := BearerToken(req); got != "ygg_test" {
		t.Fatalf("both: %q", got)
	}

	req = httptest.NewRequest(httpMethod, "/api/v1/health?api_key=ygg_test", nil)
	req.Header.Set("Authorization", "Bearer ygg_test")
	_, err = BearerToken(req)
	if !errors.Is(err, ErrAPIKeyInURL) {
		t.Fatalf("query key: %v", err)
	}
}

const httpMethod = "GET"
