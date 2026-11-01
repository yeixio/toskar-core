package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// acceleration is where Toskar says a loaded model runs.
type acceleration struct {
	State           string   `json:"state"`
	Reason          string   `json:"reason"`
	Backend         string   `json:"backend"`
	Devices         []string `json:"devices"`
	LayersOffloaded int      `json:"layers_offloaded"`
	LayersTotal     int      `json:"layers_total"`
	GPUMemoryBytes  uint64   `json:"gpu_memory_bytes"`
}

type runningView struct {
	ModelID      string        `json:"model_id"`
	InstanceID   string        `json:"instance_id"`
	Acceleration *acceleration `json:"acceleration"`
}

// fitCheck starts id through the Toskar daemon at base, as a chat would, and
// reports where Toskar says it runs and how fast it answers (#955). A model
// bigger than the graphics card should run partly on it, the rest in system
// memory, instead of failing to load. The model is stopped after.
func fitCheck(base, id string, maxTokens int) (string, error) {
	log.Printf("starting %s through Toskar", id)
	started := time.Now()
	var view runningView
	if err := toskarCall(base, http.MethodPost, "/api/v1/models/"+id+"/start", nil, &view); err != nil {
		return fitReport(id, 0, nil, 0, "", err), err
	}
	load := time.Since(started)
	defer func() {
		_ = toskarCall(base, http.MethodPost, "/api/v1/models/"+id+"/stop", map[string]string{"instance_id": view.InstanceID}, nil)
	}()
	var running []runningView
	if err := toskarCall(base, http.MethodGet, "/api/v1/models/running", nil, &running); err != nil {
		return fitReport(id, load, nil, 0, "", err), err
	}
	var acc *acceleration
	for _, r := range running {
		if r.ModelID == id {
			acc = r.Acceleration
		}
	}
	var answer struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	asked := time.Now()
	err := toskarCall(base, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model":       id,
		"messages":    []map[string]string{{"role": "user", "content": "In three sentences, why is the sky blue?"}},
		"max_tokens":  min(maxTokens, 200),
		"temperature": 0,
	}, &answer)
	if err != nil {
		return fitReport(id, load, acc, 0, "", err), err
	}
	speed := 0.0
	if s := time.Since(asked).Seconds(); s > 0 {
		speed = float64(answer.Usage.CompletionTokens) / s
	}
	text := ""
	if len(answer.Choices) > 0 {
		text = answer.Choices[0].Message.Content
	}
	if strings.TrimSpace(text) == "" {
		err = fmt.Errorf("%s answered nothing", id)
	}
	return fitReport(id, load, acc, speed, text, err), err
}

// fitReport is fitCheck's Markdown.
func fitReport(id string, load time.Duration, acc *acceleration, speed float64, text string, err error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s through Toskar (#955)\n\n| | |\n| --- | --- |\n", id)
	if load > 0 {
		fmt.Fprintf(&b, "| Started in | %.0f s |\n", load.Seconds())
	}
	if acc != nil {
		fmt.Fprintf(&b, "| Runs | %s (%s) on %s |\n", acc.State, acc.Backend, strings.Join(acc.Devices, ", "))
		if acc.Reason != "" {
			fmt.Fprintf(&b, "| Why | %s |\n", acc.Reason)
		}
		if acc.LayersTotal > 0 {
			fmt.Fprintf(&b, "| Layers on the GPU | %d of %d |\n", acc.LayersOffloaded, acc.LayersTotal)
		}
		if acc.GPUMemoryBytes > 0 {
			fmt.Fprintf(&b, "| GPU memory | %.1f GB |\n", float64(acc.GPUMemoryBytes)/1e9)
		}
	}
	if speed > 0 {
		fmt.Fprintf(&b, "| Answer | %.1f tokens/s, with the question |\n", speed)
	}
	if err != nil {
		fmt.Fprintf(&b, "| Failed | %s |\n", strings.ReplaceAll(tail(err.Error(), 300), "|", "/"))
	}
	if text != "" {
		if text = strings.TrimSpace(text); len(text) > 400 {
			text = text[:400] + "…"
		}
		fmt.Fprintf(&b, "\n> %s\n", strings.ReplaceAll(text, "\n", " "))
	}
	return b.String() + "\n"
}
