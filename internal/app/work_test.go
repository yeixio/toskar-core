package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestActiveWorkCountsStreamingTurns(t *testing.T) {
	a := &App{}
	in := make(chan pluginapi.ChatChunk, 1)
	out := countWork(context.Background(), in, a.beginWork("gpu-box"))
	done := a.beginWork("gpu-box")
	if got := a.activeWork()["gpu-box"]; got != 2 {
		t.Fatalf("two turns: %d", got)
	}
	done()
	done() // ending twice counts once
	in <- pluginapi.ChatChunk{Content: "hi"}
	if c := <-out; c.Content != "hi" {
		t.Fatalf("chunk %+v", c)
	}
	close(in)
	for range out {
	}
	deadline := time.Now().Add(time.Second)
	for len(a.activeWork()) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if w := a.activeWork(); len(w) != 0 {
		t.Fatalf("after the streams ended: %v", w)
	}

	// A turn whose reader went away still ends its count.
	ctx, cancel := context.WithCancel(context.Background())
	stuck := make(chan pluginapi.ChatChunk)
	_ = countWork(ctx, stuck, a.beginWork("studio"))
	go func() { stuck <- pluginapi.ChatChunk{Content: "unread"} }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	deadline = time.Now().Add(time.Second)
	for len(a.activeWork()) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if w := a.activeWork(); len(w) != 0 {
		t.Fatalf("after the turn was cancelled: %v", w)
	}
}

// An answer counts its prompt tokens while it streams, and what it measured
// teaches placement the computer's speed and where the chat is cached (#836).
func TestAnswersTeachPlacement(t *testing.T) {
	a := &App{}
	in := make(chan pluginapi.ChatChunk, 2)
	messages := []pluginapi.ChatMessage{{Role: "user", Content: strings.Repeat("word ", 800)}}
	out := countWorkMeasured(context.Background(), in, a.beginWorkOf("gpu-box", promptTokens(messages)),
		func(m *pluginapi.GenerationMetrics) { a.observeSpeed(context.Background(), "gpu-box", "m1", m) })
	out = a.rememberTurnPlace(out, "chat-1", "gpu-box", "m1")
	if got := a.inFlightTokens()["gpu-box"]; got < 900 || got > 1100 {
		t.Fatalf("tokens in flight %d", got)
	}
	in <- pluginapi.ChatChunk{Content: "hi"}
	in <- pluginapi.ChatChunk{Done: true, Metrics: &pluginapi.GenerationMetrics{
		PromptTokens: 1000, CompletionTokens: 200, PromptTokPerSec: 2500, EvalTokPerSec: 80,
	}}
	close(in)
	for range out {
	}
	deadline := time.Now().Add(time.Second)
	for a.previousTurn("chat-1") == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p := a.previousTurn("chat-1"); p == nil || p.NodeID != "gpu-box" || p.ModelID != "m1" || p.Tokens != 1200 {
		t.Fatalf("previous turn %+v", p)
	}
	if prefill, decode, ok := a.speeds.Rate("gpu-box", "m1"); !ok || prefill != 2500 || decode != 80 {
		t.Fatalf("speeds %v %v %v", prefill, decode, ok)
	}
	if len(a.inFlightTokens()) != 0 {
		t.Fatalf("tokens still in flight: %v", a.inFlightTokens())
	}
}

func TestChatPlacesForgetTheOldest(t *testing.T) {
	a := &App{}
	for i := range maxChatPlaces + 5 {
		a.rememberPlace(fmt.Sprint("chat-", i), "mac", "m1", 100)
	}
	if len(a.chatPlaces) != maxChatPlaces {
		t.Fatalf("%d places", len(a.chatPlaces))
	}
	if a.previousTurn("chat-0") != nil || a.previousTurn(fmt.Sprint("chat-", maxChatPlaces+4)) == nil {
		t.Fatal("kept the wrong places")
	}
}
