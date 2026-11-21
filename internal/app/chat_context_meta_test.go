package app

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// An answer keeps its context reading, so the gauge shows it when the chat
// is opened again, even when it has nothing else to keep.
func TestWithContextUsage(t *testing.T) {
	usage := map[string]any{"prompt_tokens": 1200, "limit": 8192, "instructions": 400, "tools": 300, "conversation": 500, "memory_bytes": int64(1 << 30)}
	meta := withContextUsage(nil, usage)
	if meta == nil || meta.Context == nil || meta.Context.PromptTokens != 1200 || meta.Context.Limit != 8192 || meta.Context.MemoryBytes != 1<<30 || meta.Contract != contracts.ContractVersion {
		t.Fatalf("meta = %+v", meta)
	}
	kept := &contracts.MessageMeta{Notice: "smaller model", Contract: contracts.ContractVersion}
	if got := withContextUsage(kept, usage); got != kept || got.Notice != "smaller model" || got.Context == nil {
		t.Fatalf("existing meta = %+v", got)
	}
	// No reading, or an empty one, adds nothing.
	if withContextUsage(nil, nil) != nil || withContextUsage(nil, map[string]any{"limit": 8192}) != nil {
		t.Fatal("an empty reading made metadata")
	}
}

// An answer keeps the backend and device that ran it (#317), and an
// unknown report leaves the metadata as it was.
func TestWithDevice(t *testing.T) {
	meta := withDevice(nil, &pluginapi.Acceleration{Backend: "vulkan", Devices: []string{"AMD Radeon RX 7900 XTX"}})
	if meta == nil || meta.Backend != "vulkan" || meta.Device != "AMD Radeon RX 7900 XTX" || meta.Contract == "" {
		t.Fatalf("meta: %+v", meta)
	}
	if withDevice(nil, nil) != nil {
		t.Error("no report should make no metadata")
	}
	kept := &contracts.MessageMeta{Notice: "smaller model"}
	if got := withDevice(kept, &pluginapi.Acceleration{Backend: "cpu"}); got != kept || got.Backend != "cpu" || got.Device != "" {
		t.Errorf("cpu: %+v", got)
	}
}

// An answer names the model that wrote it and the computer it ran on, and
// no computer when its steps ran on more than one (toskar-apps#447).
func TestWithAnsweredBy(t *testing.T) {
	cfg, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *config.Config) { c.NodeID, c.NodeName = "here", "Studio" }); err != nil {
		t.Fatal(err)
	}
	a := &App{Config: cfg}
	env := &chatExecEnv{app: a, lastModel: "llama-3.2-1b", roleNodes: map[string]string{"assistant": "here", "router": ""}}
	meta := a.withAnsweredBy(nil, env)
	if meta == nil || meta.Model != "llama-3.2-1b" || meta.Computer != "Studio" || meta.Contract == "" {
		t.Fatalf("here: %+v", meta)
	}
	env.roleNodes["drafter:2"] = "laptop"
	if got := a.withAnsweredBy(nil, env); got.Model != "llama-3.2-1b" || got.Computer != "" {
		t.Fatalf("two computers: %+v", got)
	}
	if a.withAnsweredBy(nil, &chatExecEnv{app: a}) != nil {
		t.Fatal("no model should make no metadata")
	}
}
