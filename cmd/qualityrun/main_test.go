package main

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHardwareLabelNamesNoComputer(t *testing.T) {
	type adapter = struct {
		Name          string `json:"name"`
		Model         string `json:"model"`
		DedicatedVRAM uint64 `json:"dedicated_vram_bytes"`
	}
	const gb = 1 << 30
	for _, c := range []struct {
		want     string
		adapters []adapter
	}{
		{"Some CPU, 64 GB", nil},
		{"Apple M5 Pro, 64 GB", []adapter{{Model: "Apple M5 Pro"}}},
		// The same name as the weekly run's reports give this card.
		{"AMD RX 7900 XTX, 24 GB", []adapter{{Model: "AMD Radeon RX 7900 XT/7900 XTX/7900 GRE/7900M", DedicatedVRAM: 24 * gb}}},
		{"Big Card, 16 GB", []adapter{{Model: "Built-in Graphics", DedicatedVRAM: gb}, {Model: "Big Card", DedicatedVRAM: 16 * gb}}},
		{"Some CPU, 64 GB", []adapter{{Model: "Microsoft Hyper-V Video"}}},
	} {
		var hw hardware
		hw.CPU.Model = "Some CPU"
		hw.Memory.TotalBytes = 64 * gb
		hw.Accelerators = c.adapters
		if got := hw.label(); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
}

func TestAskerWithNobodyThere(t *testing.T) {
	if got := newAsker(true).ask("?"); got != "" {
		t.Errorf("--yes answered %q", got)
	}
	a := &asker{in: bufio.NewReader(strings.NewReader("  y \n"))}
	if got := a.ask("?"); got != "y" {
		t.Errorf("%q", got)
	}
	if got := a.ask("?"); got != "" {
		t.Errorf("after the input ended: %q", got)
	}
}

func TestSendResult(t *testing.T) {
	var got string
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got = string(raw)
		_, _ = w.Write([]byte(`{"status":"received","number":7,"url":"https://github.com/o/r/issues/7"}`))
	}))
	defer ok.Close()
	link, err := sendResult(ok.URL, []byte(`{"model":"m"}`))
	if err != nil || link != "https://github.com/o/r/issues/7" || got != `{"model":"m"}` {
		t.Errorf("%q %v, sent %q", link, err, got)
	}
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"RESULTS_RATE_LIMITED","message":"Too many results"}}`))
	}))
	defer refused.Close()
	if _, err := sendResult(refused.URL, nil); err == nil || !strings.Contains(err.Error(), "Too many results") {
		t.Errorf("refused: %v", err)
	}
	if !strings.Contains(formLink([]byte(`{"a":1}`)), "result=%7B%22a%22") {
		t.Error(formLink([]byte(`{"a":1}`)))
	}
}
