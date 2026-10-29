package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// fakeModels is a daemon with two models: one installed, and one that
// downloads in four steps when it's installed.
func fakeModels(t *testing.T) (daemonClient, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var installs []string
	subscribed := make(chan struct{})
	progress := make(chan float64, 8)
	models := []contracts.Model{
		{ID: "llama-3.2-3b-q4", DisplayName: "Llama 3.2 3B", SizeBytes: 2_020_000_000, Installed: true, Fit: &contracts.ModelFit{Label: contracts.FitExcellent}},
		{ID: "qwen2.5-7b-q4", DisplayName: "Qwen 2.5 7B", SizeBytes: 4_680_000_000, Fit: &contracts.ModelFit{Label: contracts.FitGood}},
		{ID: "nomic-embed", DisplayName: "Nomic Embed", SupportRole: "embedding"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/models":
			_ = json.NewEncoder(w).Encode(models)
		case r.URL.Path == "/api/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			close(subscribed)
			for {
				select {
				case pct, ok := <-progress:
					if !ok {
						return
					}
					fmt.Fprintf(w, "event: model.download.progress\ndata: {\"type\":\"model.download.progress\",\"payload\":{\"model_id\":\"qwen2.5-7b-q4\",\"bytes_downloaded\":%d,\"bytes_total\":4680000000,\"percent\":%g}}\n\n", int64(pct*46_800_000), pct)
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
					return
				}
			}
		case strings.HasSuffix(r.URL.Path, "/install") && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["wait"] != true {
				t.Errorf("install without wait: %v", body)
			}
			mu.Lock()
			installs = append(installs, r.URL.Path)
			mu.Unlock()
			<-subscribed
			for _, pct := range []float64{3, 41, 45, 100} {
				progress <- pct
			}
			close(progress)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "installed"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return daemonClient{base: srv.URL, client: srv.Client()}, &installs
}

func TestModelsList(t *testing.T) {
	client, _ := fakeModels(t)
	var out bytes.Buffer
	if err := modelsCommand([]string{"list"}, client, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"ID", "llama-3.2-3b-q4", "Llama 3.2 3B", "2.0 GB", "excellent", "yes", "qwen2.5-7b-q4", "4.7 GB", "good"} {
		if !strings.Contains(text, want) {
			t.Errorf("list doesn't show %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "nomic-embed") {
		t.Errorf("list shows a supporting model:\n%s", text)
	}

	out.Reset()
	if err := modelsCommand([]string{"list", "--installed", "--json"}, client, &out); err != nil {
		t.Fatal(err)
	}
	var listed []contracts.Model
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].ID != "llama-3.2-3b-q4" {
		t.Errorf("--installed --json: %v %s", err, out.String())
	}
}

func TestModelsInstallShowsProgress(t *testing.T) {
	client, installs := fakeModels(t)
	var out bytes.Buffer
	if err := modelsCommand([]string{"install", "qwen2.5-7b-q4"}, client, &out); err != nil {
		t.Fatal(err)
	}
	want := "Downloading Qwen 2.5 7B: 0% (140 MB of 4.7 GB)\n" +
		"Downloading Qwen 2.5 7B: 40% (1.9 GB of 4.7 GB)\n" +
		"Downloading Qwen 2.5 7B: 100% (4.7 GB of 4.7 GB)\n" +
		"Installed Qwen 2.5 7B.\n"
	if out.String() != want {
		t.Errorf("install printed:\n%s\nwant:\n%s", out.String(), want)
	}
	if len(*installs) != 1 || (*installs)[0] != "/api/v1/models/qwen2.5-7b-q4/install" {
		t.Errorf("installs %v", *installs)
	}
}

func TestModelsInstallChecksTheModel(t *testing.T) {
	client, installs := fakeModels(t)
	var out bytes.Buffer
	if err := modelsCommand([]string{"install", "llama-3.2-3b-q4"}, client, &out); err != nil || out.String() != "Llama 3.2 3B is already installed.\n" {
		t.Errorf("already installed: %v %q", err, out.String())
	}
	if err := modelsCommand([]string{"install", "no-such-model"}, client, &out); err == nil || !strings.Contains(err.Error(), "toskarctl models list") {
		t.Errorf("unknown model: %v", err)
	}
	if len(*installs) != 0 {
		t.Errorf("installs %v", *installs)
	}
	for _, args := range [][]string{nil, {"install"}, {"remove", "x"}, {"list", "--nope"}} {
		var e *exitError
		if err := modelsCommand(args, client, &out); err == nil || !strings.Contains(err.Error(), "usage: toskarctl models") {
			t.Errorf("%v: %v", args, err)
		} else if !errors.As(err, &e) || e.code != 2 {
			t.Errorf("%v: exit %v", args, err)
		}
	}
}
