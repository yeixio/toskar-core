package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// pictureStore keeps pictures in memory, and what was removed.
type pictureStore struct {
	mu      sync.Mutex
	saved   map[string]artifacts.Input
	removed []string
}

func (p *pictureStore) Save(ctx context.Context, in artifacts.Input) (artifacts.Artifact, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.saved == nil {
		p.saved = map[string]artifacts.Input{}
	}
	id := "pic" + string(rune('0'+len(p.saved)))
	p.saved[id] = in
	return artifacts.Artifact{ID: id, Name: in.Name}, nil
}

func (p *pictureStore) Delete(ctx context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removed = append(p.removed, id)
	return nil
}

// attachChat records the attachments each turn ran with.
type attachChat struct{ attached [][]string }

func (c *attachChat) RunChat(ctx context.Context, profileID, conversationID, message string, stream bool, modelID, execution string) (<-chan pluginapi.ChatChunk, error) {
	_ = turnopts.From(ctx)
	c.attached = append(c.attached, artifacts.AttachmentsFrom(ctx))
	ch := make(chan pluginapi.ChatChunk, 1)
	ch <- pluginapi.ChatChunk{Content: "A red bicycle.", Done: true}
	close(ch)
	return ch, nil
}

func pictureRequest(url string) string {
	b, _ := json.Marshal(map[string]any{"model": "profile:general", "messages": []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": "What's in it?"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}},
	}}}})
	return string(b)
}

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// A request's pictures are attached to its turn as the web chat's are, then
// removed (#830).
func TestCompletionPictures(t *testing.T) {
	h := testHandler(t)
	store := &pictureStore{}
	chat := &attachChat{}
	h.Pictures, h.Chat = store, chat

	rec := post(h, pictureRequest("data:image/png;base64,"+base64.StdEncoding.EncodeToString(pngBytes)))
	if rec.Code != http.StatusOK || len(chat.attached) != 1 || len(chat.attached[0]) != 1 {
		t.Fatalf("%d %s, attached %v", rec.Code, rec.Body, chat.attached)
	}
	id := chat.attached[0][0]
	if in := store.saved[id]; string(in.Data) != string(pngBytes) || !strings.HasSuffix(in.Name, ".png") || in.Producer != artifacts.ProducerUser {
		t.Fatalf("saved %+v", in)
	}
	if len(store.removed) != 1 || store.removed[0] != id {
		t.Fatalf("removed %v", store.removed)
	}

	// An http(s) picture is fetched, and must be a picture.
	pics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bike.png" {
			_, _ = w.Write(pngBytes)
			return
		}
		_, _ = w.Write([]byte("<html>not a picture</html>"))
	}))
	defer pics.Close()
	h.PictureClient = pics.Client()
	if rec := post(h, pictureRequest(pics.URL+"/bike.png")); rec.Code != http.StatusOK || len(chat.attached) != 2 {
		t.Fatalf("fetched: %d %s", rec.Code, rec.Body)
	}
	for name, url := range map[string]string{
		"a page, not a picture": pics.URL + "/page",
		"not an image":          "data:text/plain;base64,aGk=",
		"not base64":            "data:image/png,rawbytes",
		"bad base64":            "data:image/png;base64,***",
		"another scheme":        "file:///etc/passwd",
	} {
		rec := post(h, pictureRequest(url))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_IMAGE") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}

	// Without the test's client, a picture's URL reaches the internet only,
	// never this computer.
	h.PictureClient = nil
	if rec := post(h, pictureRequest(pics.URL+"/bike.png")); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "couldn't get the picture") {
		t.Errorf("a picture on this computer was fetched: %d %s", rec.Code, rec.Body)
	}
	if len(chat.attached) != 2 {
		t.Errorf("a refused picture still ran a turn: %v", chat.attached)
	}
}
