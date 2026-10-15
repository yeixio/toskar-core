package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Picture and clip setup on a paired computer (#153): a laptop can offer to
// set image generation up on the workstation whose GPU would make the
// pictures, start it there, and follow its download. Once it is ready, the
// request runs there through the remote tools (#136).

// MediaSetupStatus is a computer's image or video setup, with the free disk
// space a download would use.
type MediaSetupStatus struct {
	imagegen.Status
	FreeBytes uint64 `json:"free_bytes,omitempty"`
}

// mediaSetup is this computer's setup for a kind: images or video.
func (a *App) mediaSetup(kind string) *imagegen.Setup {
	switch kind {
	case "images":
		return a.Images
	case "video":
		return a.Video
	}
	return nil
}

func (a *App) mediaSetupStatus(_ context.Context, st *imagegen.Setup) MediaSetupStatus {
	out := MediaSetupStatus{Status: st.Status()}
	if st.FreeBytes != nil {
		if free, err := st.FreeBytes(); err == nil && free > 0 {
			out.FreeBytes = uint64(free)
		}
	}
	return out
}

// mediaSetupHandler serves this computer's image and video setup to paired
// computers, at /media/{kind}/setup: GET for its status, POST {model_id} to
// start it, and DELETE to stop it. The internal server checks the peer
// first, as it does before installing a model for one.
func (a *App) mediaSetupHandler() http.Handler {
	mux := http.NewServeMux()
	bound := func(w http.ResponseWriter, r *http.Request) *imagegen.Setup {
		st := a.mediaSetup(r.PathValue("kind"))
		if st == nil {
			peerJSONError(w, http.StatusNotFound, "SETUP_REFUSED", errors.New("this computer doesn't make "+r.PathValue("kind")))
		}
		return st
	}
	mux.HandleFunc("GET /media/{kind}/setup", func(w http.ResponseWriter, r *http.Request) {
		if st := bound(w, r); st != nil {
			writeJSONStatus(w, http.StatusOK, a.mediaSetupStatus(r.Context(), st))
		}
	})
	mux.HandleFunc("POST /media/{kind}/setup", func(w http.ResponseWriter, r *http.Request) {
		st := bound(w, r)
		if st == nil {
			return
		}
		var body struct {
			ModelID string `json:"model_id"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			peerJSONError(w, http.StatusBadRequest, "INVALID_BODY", errors.New("invalid body"))
			return
		}
		if err := st.Start(body.ModelID); err != nil {
			peerJSONError(w, http.StatusConflict, "SETUP_REFUSED", err)
			return
		}
		writeJSONStatus(w, http.StatusAccepted, a.mediaSetupStatus(r.Context(), st))
	})
	mux.HandleFunc("DELETE /media/{kind}/setup", func(w http.ResponseWriter, r *http.Request) {
		if st := bound(w, r); st != nil {
			st.Cancel()
			writeJSONStatus(w, http.StatusOK, a.mediaSetupStatus(r.Context(), st))
		}
	})
	return mux
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errOldPeer is a paired computer whose Toskar can't set up images for
// another.
var errOldPeer = fmt.Errorf("that computer needs a newer Toskar to set this up from here")

// peerMediaCall sends a setup request to a paired computer and returns its
// answer as it came.
func (a *App) peerMediaCall(ctx context.Context, n contracts.Node, method, kind string, body []byte) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	resp, err := a.peerClient(n).Do(ctx, method, "/internal/v1/media/"+kind+"/setup", r)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return 0, nil, errOldPeer
	}
	return resp.StatusCode, raw, nil
}

// RemoteMediaSetup serves the app's setup routes for a paired computer:
// status, start, and stop there. handled is false for this computer, whose
// own setup answers.
func (a *App) RemoteMediaSetup(ctx context.Context, nodeID, kind, method string, body []byte) (status int, raw []byte, handled bool, err error) {
	if nodeID == "" || nodeID == a.Config.Get().NodeID {
		return 0, nil, false, nil
	}
	n, err := a.findPairedNode(ctx, nodeID)
	if err != nil {
		return 0, nil, true, err
	}
	status, raw, err = a.peerMediaCall(ctx, n, method, kind, body)
	if err != nil {
		return 0, nil, true, err
	}
	var st MediaSetupStatus
	if json.Unmarshal(raw, &st) == nil {
		a.peerMedia.put(nodeID, kind, &st)
		// Ready there: its tools are asked again now, so the request sent
		// next runs there instead of being offered the setup again.
		if st.Ready && a.toolNet != nil {
			a.toolNet.Refresh(ctx, nodeID)
		}
	}
	return status, raw, true, nil
}

// peerMediaCache remembers each paired computer's setup for a while, so
// what Toskar can do is answered without asking every computer each time.
type peerMediaCache struct {
	mu sync.Mutex
	m  map[string]*peerMediaEntry
}

type peerMediaEntry struct {
	at       time.Time
	kinds    map[string]*MediaSetupStatus
	fetching bool
}

const (
	peerMediaTTL     = 30 * time.Second
	peerMediaTimeout = 4 * time.Second
)

func (c *peerMediaCache) put(nodeID, kind string, st *MediaSetupStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*peerMediaEntry{}
	}
	e := c.m[nodeID]
	if e == nil {
		e = &peerMediaEntry{kinds: map[string]*MediaSetupStatus{}}
		c.m[nodeID] = e
	}
	e.kinds[kind] = st
}

// peerMediaSetups is each online paired computer's image and video setup,
// by computer. With wait, stale answers are fetched first; without, what is
// known is used and refreshed in the background.
func (a *App) peerMediaSetups(ctx context.Context, wait bool) map[string]map[string]*MediaSetupStatus {
	out := map[string]map[string]*MediaSetupStatus{}
	if a.Nodes == nil {
		return out
	}
	list, err := a.Nodes.List(ctx)
	if err != nil {
		return out
	}
	c := &a.peerMedia
	var wg sync.WaitGroup
	for _, n := range list {
		if n.IsLocal || !n.Paired || n.Address == "" || n.Status != contracts.NodeStatusOnline {
			continue
		}
		c.mu.Lock()
		if c.m == nil {
			c.m = map[string]*peerMediaEntry{}
		}
		e := c.m[n.ID]
		if e == nil {
			e = &peerMediaEntry{kinds: map[string]*MediaSetupStatus{}}
			c.m[n.ID] = e
		}
		stale := time.Since(e.at) > peerMediaTTL
		start := stale && !e.fetching
		if start {
			e.fetching = true
		}
		c.mu.Unlock()
		if start {
			wg.Add(1)
			go func(n contracts.Node) {
				defer wg.Done()
				a.fetchPeerMedia(context.WithoutCancel(ctx), n)
			}(n)
		}
	}
	if wait {
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(peerMediaTimeout + time.Second):
		case <-ctx.Done():
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, e := range c.m {
		if len(e.kinds) == 0 {
			continue
		}
		kinds := map[string]*MediaSetupStatus{}
		for k, v := range e.kinds {
			kinds[k] = v
		}
		out[id] = kinds
	}
	return out
}

// fetchPeerMedia asks a paired computer for its image and video setup.
func (a *App) fetchPeerMedia(ctx context.Context, n contracts.Node) {
	ctx, cancel := context.WithTimeout(ctx, peerMediaTimeout)
	defer cancel()
	kinds := map[string]*MediaSetupStatus{}
	for _, kind := range []string{"images", "video"} {
		status, raw, err := a.peerMediaCall(ctx, n, http.MethodGet, kind, nil)
		if err != nil || status != http.StatusOK {
			continue
		}
		var st MediaSetupStatus
		if json.Unmarshal(raw, &st) == nil {
			kinds[kind] = &st
		}
	}
	c := &a.peerMedia
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.m[n.ID]
	if e == nil {
		return
	}
	e.at, e.fetching, e.kinds = time.Now(), false, kinds
}

// mediaElsewhere lists, for each model of a kind, the online paired
// computers it works well on, from what they last reported (#550). It
// doesn't wait for a computer that hasn't answered yet.
func (a *App) mediaElsewhere(ctx context.Context, kind string) []imagegen.Place {
	names := map[string]string{}
	if a.Nodes != nil {
		if list, err := a.Nodes.List(ctx); err == nil {
			for _, n := range list {
				names[n.ID] = n.Name
			}
		}
	}
	var out []imagegen.Place
	for id, byKind := range a.peerMediaSetups(ctx, false) {
		st := byKind[kind]
		if st == nil || !st.Supported {
			continue
		}
		for _, m := range st.Models {
			if worksWell(m) {
				out = append(out, imagegen.Place{NodeID: id, NodeName: names[id], ModelID: m.ID, Installed: m.Installed})
			}
		}
	}
	slices.SortFunc(out, func(x, y imagegen.Place) int { return strings.Compare(x.NodeName+x.ModelID, y.NodeName+y.ModelID) })
	return out
}

// worksWell reports a model that works well on the computer that reported
// it; for a computer from before verdicts, memory decides.
func worksWell(m imagegen.ModelStatus) bool {
	if m.Verdict == "" {
		return !m.TooLittleMemory && !m.TightMemory
	}
	return m.Verdict == imagegen.VerdictWorks
}

// peerJSONError answers a paired computer's failed request with the
// error's text, as before, and its stable code (#588): the error's own, or
// code.
func peerJSONError(w http.ResponseWriter, status int, code string, err error) {
	failure := contracts.FailureOf(err, code)
	writeJSONStatus(w, status, map[string]any{"error": failure.Text, "code": failure.Code, "details": failure.Details})
}
