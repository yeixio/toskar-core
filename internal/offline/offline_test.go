package offline

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
)

func TestIs(t *testing.T) {
	notFound := &net.DNSError{Err: "no such host", Name: "huggingface.co", IsNotFound: true}
	unreachable := &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("connect: %w", syscall.ENETUNREACH)}
	for _, c := range []struct {
		name string
		err  error
		want bool
	}{
		{"name not found", fmt.Errorf("Get \"https://huggingface.co/api\": %w", notFound), true},
		{"network unreachable", unreachable, true},
		{"joined behind a friendly message", errors.Join(errors.New("the AI news couldn't be read"), notFound), true},
		{"refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, false},
		{"other", errors.New("500 Internal Server Error"), false},
		{"nil", nil, false},
	} {
		if got := Is(c.err); got != c.want {
			t.Errorf("%s: Is = %v, want %v", c.name, got, c.want)
		}
	}
	if got := Message(notFound); !strings.Contains(got, "huggingface.co couldn't be reached") {
		t.Errorf("Message = %q", got)
	}
}

// A real lookup of a name that can't exist fails as the offline case does.
func TestIsRealLookup(t *testing.T) {
	_, err := net.DefaultResolver.LookupHost(context.Background(), "no-such-host.invalid")
	if !Is(err) {
		t.Fatalf("lookup error not seen as offline: %#v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 500) }))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
	}
	if Is(err) {
		t.Fatal("a server that answered is online")
	}
}
