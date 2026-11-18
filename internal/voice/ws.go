package voice

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// maxEventBytes is the largest client event: about 10 s of audio, base64.
const maxEventBytes = 2 << 20

// idleTimeout ends a session that has sent nothing, not even audio, for
// this long.
const idleTimeout = 2 * time.Minute

// Serve runs a session over a WebSocket on this request, as OpenAI's
// Realtime API does: one JSON event per text message, each way. It
// returns when the client goes. The caller has already checked who's
// asking. OpenAI's clients ask for the realtime subprotocol, which is
// agreed to.
func Serve(w http.ResponseWriter, r *http.Request, parts Parts) error {
	up := ws.HTTPUpgrader{
		Protocol: func(p string) bool { return p == "realtime" },
	}
	conn, _, _, err := up.Upgrade(r, w)
	if err != nil {
		return err
	}
	defer conn.Close()
	s := NewSession(parts, func(event map[string]any) error {
		b, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return wsutil.WriteServerMessage(conn, ws.OpText, b)
	})
	defer s.Close()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
		data, op, err := wsutil.ReadClientData(conn)
		if err != nil {
			return nil
		}
		if op != ws.OpText {
			continue
		}
		if len(data) > maxEventBytes {
			s.fail("event_too_large", "an event is at most 2 MB; send audio in smaller pieces", "")
			continue
		}
		s.Handle(data)
	}
}
