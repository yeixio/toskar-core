package screenshot

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// The user guide's pictures, clips, and vision page (#893): a picture
// being made, and a question about a screenshot.

// visionModelsPNG is the Models page as the guide showed it, kept here so
// the answer about it stays true when the guide's screenshot is taken again.
//
//go:embed vision-models.png
var visionModelsPNG []byte

// currentScreen is the screen whose page was last opened. The page drops
// ?screen= from its address once it loads, so its later requests, such as
// the event stream, don't carry it; screenshots are taken one at a time.
var currentScreen atomic.Value

// serveMedia answers the requests these chats make; false for any other.
func serveMedia(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/v1/artifacts/file-models-shot/content":
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(visionModelsPNG)
	case "/api/v1/conversations/conv-lighthouse/messages":
		writeJSON(w, http.StatusOK, screenshotLighthouseMessages)
	case "/api/v1/conversations/conv-lighthouse/turn":
		// Still making the picture, as the chat finds it when opened.
		writeJSON(w, http.StatusOK, `{"running":true,"steps":[{"tool_id":"image.generate","summary":"a watercolor of a lighthouse at dusk","status":"running"}]}`)
	case "/api/v1/conversations/conv-vision/messages":
		writeJSON(w, http.StatusOK, screenshotVisionMessages)
	default:
		return false
	}
	return true
}

// screenEvents are what a screen's event stream sends, for a state only
// events show, such as how far a picture has got.
var screenEvents = map[string][]string{
	"media-making": {
		`{"type":"chat.making_media","payload":{"conversation_id":"conv-lighthouse","kind":"image"}}`,
		`{"type":"tool.progress","payload":{"conversation_id":"conv-lighthouse","tool_id":"image.generate","stage":"drawing","step":5,"steps":8,"seconds_left":6}}`,
	},
}

// sendScreenEvents sends the screen's events until the page goes, again
// each second, so a page that subscribes late still gets them.
func sendScreenEvents(w http.ResponseWriter, r *http.Request) {
	screen, _ := currentScreen.Load().(string)
	events := screenEvents[screen]
	if len(events) == 0 {
		return
	}
	flusher, _ := w.(http.Flusher)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		for _, e := range events {
			var head struct{ Type string }
			_ = json.Unmarshal([]byte(e), &head)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", head.Type, e)
		}
		if flusher != nil {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}

const screenshotLighthouseMessages = `[
  {
    "id": "l1",
    "conversation_id": "conv-lighthouse",
    "role": "user",
    "content": "Make a watercolor of a lighthouse at dusk.",
    "created_at": "2026-09-24T16:02:00Z"
  }
]`

// screenshotVisionMessages asks about a screenshot, answered by a model
// that sees pictures.
const screenshotVisionMessages = `[
  {
    "id": "v1",
    "conversation_id": "conv-vision",
    "role": "user",
    "content": "Which of these models can see pictures, and will it fit?",
    "meta": {"files": [{"id": "file-models-shot", "name": "Models page.png", "mime_type": "image/png", "kind": "image", "size_bytes": 164229, "producer": "user"}]},
    "created_at": "2026-09-24T16:20:00Z"
  },
  {
    "id": "v2",
    "conversation_id": "conv-vision",
    "role": "assistant",
    "content": "**Ornith 1.5 9B** is the one marked **Sees pictures**. It's a 5.4 GB download, and the page rates it an **Excellent fit** for studio, the M4 Pro with 48 GB of memory, so it will fit.\n\nGemma 4 E4B, already installed, is the page's best overall pick, but it isn't marked as seeing pictures.",
    "meta": {"model_id": "gemma-4-e4b"},
    "created_at": "2026-09-24T16:20:12Z"
  }
]`
