package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/netguard"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Pictures in a chat completion (#830). The last user message's pictures
// are kept as files for the turn, attached as the web chat attaches them,
// so the turn goes to a model that sees, its image encoder is loaded, and
// with none that sees, the answer says so. They're removed once the
// answer is done.

// PictureStore keeps a request's pictures while its turn runs.
type PictureStore interface {
	Save(ctx context.Context, in artifacts.Input) (artifacts.Artifact, error)
	Delete(ctx context.Context, id string) error
}

// errPicture is a picture the request can't send: 400 with why.
type errPicture struct{ msg string }

func (e errPicture) Error() string { return e.msg }

// lastUserPictures are the pictures in the last user message.
func lastUserPictures(messages []pluginapi.ChatMessage) []string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Images
		}
	}
	return nil
}

// savePictures keeps each picture as a file with no chat, and returns the
// files' ids and what removes them.
func (h *Handler) savePictures(ctx context.Context, urls []string) ([]string, func(), error) {
	done := func() {}
	if len(urls) == 0 {
		return nil, done, nil
	}
	if h.Pictures == nil {
		return nil, done, errPicture{"pictures aren't available on this Toskar"}
	}
	var ids []string
	remove := func() {
		// Not the request's: it may be over, and the files go anyway.
		ctx := context.WithoutCancel(ctx)
		for _, id := range ids {
			_ = h.Pictures.Delete(ctx, id)
		}
	}
	for i, u := range urls {
		data, mediaType, err := h.readPicture(ctx, u)
		if err != nil {
			remove()
			return nil, done, err
		}
		ext := ".png"
		if exts, _ := mime.ExtensionsByType(mediaType); len(exts) > 0 {
			ext = exts[len(exts)-1]
		}
		a, err := h.Pictures.Save(ctx, artifacts.Input{Name: fmt.Sprintf("picture-%d%s", i+1, ext), Producer: artifacts.ProducerUser, Data: data})
		if err != nil {
			remove()
			return nil, done, errPicture{fmt.Sprintf("picture %d: %v", i+1, err)}
		}
		ids = append(ids, a.ID)
	}
	return ids, remove, nil
}

// readPicture is a data: URL's picture, or an http(s) one's, fetched from
// the internet only: never this computer or its network.
func (h *Handler) readPicture(ctx context.Context, u string) ([]byte, string, error) {
	switch {
	case strings.HasPrefix(u, "data:"):
		meta, payload, ok := strings.Cut(strings.TrimPrefix(u, "data:"), ",")
		mediaType, enc, _ := strings.Cut(meta, ";")
		if !ok || enc != "base64" {
			return nil, "", errPicture{"a data: picture must be base64, as data:image/png;base64,…"}
		}
		if !strings.HasPrefix(mediaType, "image/") {
			return nil, "", errPicture{fmt.Sprintf("a data: picture must be an image, not %q", mediaType)}
		}
		if len(payload) > artifacts.MaxBytes*4/3+4 {
			return nil, "", errPicture{fmt.Sprintf("a picture can be up to %d MB", artifacts.MaxBytes>>20)}
		}
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", errPicture{"a data: picture isn't valid base64"}
		}
		return data, mediaType, nil
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "http://"):
		return fetchPicture(ctx, h.pictureClient(), u)
	}
	return nil, "", errPicture{"a picture must be a data: URL or an http(s) one"}
}

func (h *Handler) pictureClient() *http.Client {
	if h.PictureClient != nil {
		return h.PictureClient
	}
	return guardedClient
}

// guardedClient reaches the internet only: netguard refuses this computer
// and private networks when connecting, so a picture's URL can't reach
// what's behind Toskar.
var guardedClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: netguard.Control}).DialContext,
		Proxy:       nil,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

func fetchPicture(ctx context.Context, client *http.Client, u string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", errPicture{"a picture's URL isn't valid"}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", errPicture{"couldn't get the picture at " + u + ": " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", errPicture{fmt.Sprintf("couldn't get the picture at %s: %s", u, resp.Status)}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, artifacts.MaxBytes+1))
	if err != nil {
		return nil, "", errPicture{"couldn't get the picture at " + u + ": " + err.Error()}
	}
	if len(data) > artifacts.MaxBytes {
		return nil, "", errPicture{fmt.Sprintf("the picture at %s is over %d MB", u, artifacts.MaxBytes>>20)}
	}
	mediaType := http.DetectContentType(data)
	if !strings.HasPrefix(mediaType, "image/") {
		return nil, "", errPicture{fmt.Sprintf("%s isn't a picture (%s)", u, mediaType)}
	}
	return data, mediaType, nil
}
