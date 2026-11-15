package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// With Check links in answers on, a saved answer's dead links are added to
// it with a notice, the apps hear of it, and each check is listed in What
// left this computer (#209).
func TestCheckAnswerLinks(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone":
			w.WriteHeader(http.StatusGone)
		case "/signin":
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	a.linkChecker.AllowPrivate = true
	conv, err := a.Conversations.Create(ctx, "links", "", "")
	if err != nil {
		t.Fatal(err)
	}
	answer := "See [the guide](" + srv.URL + "/ok), [the old page](" + srv.URL + "/gone), and [the account](" + srv.URL + "/signin)."
	msg, err := a.Conversations.AddMessageWithMeta(ctx, conv.ID, "assistant", answer, &contracts.MessageMeta{})
	if err != nil {
		t.Fatal(err)
	}
	sub, ch := a.Bus.Subscribe()
	defer a.Bus.Unsubscribe(sub)

	a.checkAnswerLinks(ctx, "en", conv.ID, msg.ID, answer)

	msgs, err := a.Conversations.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	meta := msgs[len(msgs)-1].Meta
	if meta == nil || meta.LinkCheck == nil || !slices.Equal(meta.LinkCheck.Dead, []string{srv.URL + "/gone"}) || !slices.Equal(meta.LinkCheck.Unknown, []string{srv.URL + "/signin"}) {
		t.Fatalf("link check %+v", meta)
	}
	if !strings.Contains(meta.Notice, "didn't open") || !strings.Contains(meta.Notice, srv.URL+"/gone") {
		t.Fatalf("notice %q", meta.Notice)
	}
	select {
	case evt := <-ch:
		if evt.Type != events.ChatLinksChecked || evt.Payload["message_id"] != msg.ID {
			t.Fatalf("event %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no chat.links_checked")
	}
	recs, err := a.Egress.List(ctx, egress.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(recs, func(r egress.Record) bool { return r.Kind == egress.LinkCheck }) {
		t.Fatalf("no link_check record in %v", recs)
	}
}

// Links are checked only with the setting on, for a profile that may use
// the internet, in a chat that used nothing kept on this computer.
func TestMayCheckLinks(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	online := profiles.Profile{Tools: []contracts.ToolPolicy{{ToolID: "internet.open", Policy: "allow"}}}
	if a.mayCheckLinks(ctx, online, false) {
		t.Fatal("checked with the setting off")
	}
	if err := a.Settings.SetBool(ctx, settingCheckLinks, true); err != nil {
		t.Fatal(err)
	}
	if !a.mayCheckLinks(ctx, online, false) {
		t.Fatal("not checked with the setting on")
	}
	if a.mayCheckLinks(ctx, online, true) {
		t.Fatal("checked in a chat kept on this computer")
	}
	offline := profiles.Profile{Tools: []contracts.ToolPolicy{{ToolID: "internet.open", Policy: "deny"}}}
	if a.mayCheckLinks(ctx, offline, false) {
		t.Fatal("checked for a profile that can't use the internet")
	}
}
