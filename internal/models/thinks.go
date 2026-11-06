package models

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/gguf"
)

// thinksCache is what installed files' headers say about thinking (#840),
// by path, kept while the file is unchanged: a header is read once, not on
// every model list.
type thinksCache struct {
	mu    sync.Mutex
	files map[string]thinksFile
}

type thinksFile struct {
	size   int64
	mod    time.Time
	thinks bool
	// harmony is gpt-oss's chat format (#1099).
	harmony bool
}

func (c *thinksCache) at(path string) bool {
	return c.file(path).thinks
}

func (c *thinksCache) file(path string) thinksFile {
	st, err := os.Stat(path)
	if err != nil {
		return thinksFile{}
	}
	c.mu.Lock()
	f, ok := c.files[path]
	c.mu.Unlock()
	if ok && f.size == st.Size() && f.mod.Equal(st.ModTime()) {
		return f
	}
	d, err := gguf.Inspect(path)
	f = thinksFile{size: st.Size(), mod: st.ModTime(), thinks: err == nil && d.Thinks, harmony: err == nil && d.Harmony}
	c.mu.Lock()
	if c.files == nil {
		c.files = map[string]thinksFile{}
	}
	c.files[path] = f
	c.mu.Unlock()
	return f
}

// thinks reports a model that thinks before it answers: the catalog says
// so, or its installed file's chat template or name does, as for a Qwen 3
// or gpt-oss added from a file or from Hugging Face.
func (m *Manager) thinks(e CatalogEntry, path string) bool {
	if e.Capabilities.Thinks {
		return true
	}
	return path != "" && m.thinkers.at(path)
}

// Thinks reports whether a model thinks before it answers (#840), for the
// room its answer needs.
func (m *Manager) Thinks(ctx context.Context, modelID string) bool {
	e, ok := m.catalog.Get(modelID)
	if !ok {
		return false
	}
	_, path, _ := m.storage.IsInstalled(ctx, modelID)
	return m.thinks(e, path)
}

// NativeTools reports whether a model's tool calls come back only when the
// request offers its tools natively: gpt-oss, whose harmony format puts a
// call in a message llama.cpp drops otherwise (#1099).
func (m *Manager) NativeTools(ctx context.Context, modelID string) bool {
	_, path, _ := m.storage.IsInstalled(ctx, modelID)
	return path != "" && m.thinkers.file(path).harmony
}

// ThinkingOff reports whether a call runs modelID without thinking: the
// call says so (#840), or the model answers first by default and the call
// didn't ask it to think (#593).
func (m *Manager) ThinkingOff(modelID string, noThinking, thinking bool) bool {
	if noThinking {
		return true
	}
	e, ok := m.catalog.Get(modelID)
	return !thinking && ok && e.Thinking == ThinkingWhenAsked
}

// thinkingErrors is each catalog entry whose Thinking isn't one Core knows.
func thinkingErrors(list []CatalogEntry) []error {
	var errs []error
	for _, e := range list {
		if e.Thinking != "" && e.Thinking != ThinkingWhenAsked {
			errs = append(errs, fmt.Errorf("%s: thinking %q is empty or %q", e.ID, e.Thinking, ThinkingWhenAsked))
		}
	}
	return errs
}
