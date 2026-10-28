package models

import (
	"context"
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
}

func (c *thinksCache) at(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	c.mu.Lock()
	f, ok := c.files[path]
	c.mu.Unlock()
	if ok && f.size == st.Size() && f.mod.Equal(st.ModTime()) {
		return f.thinks
	}
	d, err := gguf.Inspect(path)
	f = thinksFile{size: st.Size(), mod: st.ModTime(), thinks: err == nil && d.Thinks}
	c.mu.Lock()
	if c.files == nil {
		c.files = map[string]thinksFile{}
	}
	c.files[path] = f
	c.mu.Unlock()
	return f.thinks
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
