package connectors

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/connectors/reolinktest"
)

// fakeReolinkCLI stands in for reolink-cli: it writes its arguments and the
// audio it was given to dir, and prints answer.
func fakeReolinkCLI(t *testing.T, answer string, code int) (dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake reolink-cli is a shell script")
	}
	dir = t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + dir + "/args\"\nprintf '%s' \"$REOLINK_PASSWORD\" > \"" + dir + "/password\"\ncat > \"" + dir + "/audio\"\necho '" + answer + "'\nexit " + string(rune('0'+code)) + "\n"
	bin := filepath.Join(dir, "reolink-cli")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old, oldUp := reolinkCLI, gatewayUp
	reolinkCLI = func() (string, error) { return bin, nil }
	gatewayUp = func(string) bool { return true }
	t.Cleanup(func() { reolinkCLI, gatewayUp = old, oldUp })
	return dir
}

// Toskar speaks through a Reolink doorbell with reolink-cli, and adds
// silence after the words, since the doorbell cuts off the last moment
// of what it's sent (#667).
func TestReolinkTalksThroughReolinkCLI(t *testing.T) {
	door := reolinktest.New(false)
	defer door.Close()
	m := reolinkManager(t)
	ctx := context.Background()
	if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password}); err != nil {
		t.Fatal(err)
	}
	words := make([]int16, ReolinkTalkRate) // one second
	for i := range words {
		words[i] = 1000
	}
	// Nothing to set up (#667): it reaches the connection's own camera, at
	// its address and channel with its login, through the default gateway,
	// and the password isn't on the command line.
	if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password, "channel": "0"}); err != nil {
		t.Fatal(err)
	}
	dir := fakeReolinkCLI(t, `{"ok":true,"command":"audio","data":{"durationMs":1800}}`, 0)
	if !m.CanTalk("reolink", "camera") {
		t.Fatal("a connection with reolink-cli can't talk")
	}
	if err := m.Talk(ctx, "reolink", "camera", words, ReolinkTalkRate); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if got := strings.Join(strings.Fields(string(args)), " "); got != "--gateway-addr 127.0.0.1:9000 --host=127.0.0.1:9000 --channel=0 --user=admin audio talk --stdin --sample-rate 16000" {
		t.Fatalf("args = %v", got)
	}
	if strings.Contains(string(args), door.Password) {
		t.Fatal("the password is on the command line")
	}
	if pw, _ := os.ReadFile(filepath.Join(dir, "password")); string(pw) != door.Password {
		t.Fatalf("reolink-cli was given the password %q", pw)
	}

	// A camera named in reolink-cli is used instead.
	if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password, "talk_camera": "hub-ch0", "talk_gateway": "127.0.0.1:9100"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Talk(ctx, "reolink", "camera", words, ReolinkTalkRate); err != nil {
		t.Fatal(err)
	}
	args, _ = os.ReadFile(filepath.Join(dir, "args"))
	if got := strings.Join(strings.Fields(string(args)), " "); got != "--gateway-addr 127.0.0.1:9100 --camera hub-ch0 audio talk --stdin --sample-rate 16000" {
		t.Fatalf("args = %v", got)
	}
	audio, _ := os.ReadFile(filepath.Join(dir, "audio"))
	tail := ReolinkTalkRate * 8 / 10
	if len(audio) != 2*(len(words)+tail) {
		t.Fatalf("sent %d bytes, want the words and 0.8 s of silence, %d", len(audio), 2*(len(words)+tail))
	}
	for i := 2 * len(words); i < len(audio); i++ {
		if audio[i] != 0 {
			t.Fatalf("byte %d after the words isn't silence", i)
		}
	}
	// The words are sent louder, for a doorbell's small speaker: 1000 is
	// raised by the most it's raised, four times.
	if audio[0] != 0xa0 || audio[1] != 0x0f { // 4000, little-endian
		t.Fatalf("words start % x", audio[:2])
	}

	fakeReolinkCLI(t, `{"ok":false,"error":{"code":"CAMERA_OFFLINE","message":"camera is offline"}}`, 1)
	if err := m.Talk(ctx, "reolink", "camera", words, ReolinkTalkRate); err == nil || !strings.Contains(err.Error(), "camera is offline") {
		t.Fatalf("a failed talk: %v", err)
	}
	if err := m.Talk(ctx, "homeassistant", "", words, ReolinkTalkRate); err == nil {
		t.Fatal("Home Assistant has no speaker here")
	}
}

// A speaker name or gateway that isn't one never reaches the command line.
func TestReolinkTalkRefusesOddNames(t *testing.T) {
	fakeReolinkCLI(t, `{"ok":true}`, 0)
	for _, cred := range []Credential{
		{"talk_camera": "--help"},
		{"talk_camera": "a b"},
		{"talk_camera": "door", "talk_gateway": "x; rm -rf"},
		{"url": "https://cam", "channel": "0; reboot"},
		{"url": ""},
	} {
		if err := reolinkTalk(context.Background(), cred, []int16{1}, ReolinkTalkRate); err == nil {
			t.Errorf("%v: no error", cred)
		}
	}
}

// speech is a voice reading a few syllables, some loud and some quiet,
// at about a third of full scale, as Toskar's voices read.
func speech(rate int) []int16 {
	var out []int16
	for _, loud := range []float64{1, 0.3, 0.6, 0.2, 0.8, 0.35} {
		n := rate / 5
		for i := range n {
			t := float64(i) / float64(rate)
			v := 0.0
			for k := 1.0; k <= 15; k++ {
				v += math.Sin(2*math.Pi*150*k*t+k*k*0.7) / k // phases as a voice's
			}
			out = append(out, int16(10000*loud*math.Sin(math.Pi*float64(i)/float64(n))*v/2.2))
		}
		out = append(out, make([]int16, rate/16)...)
	}
	return out
}

// played is what a doorbell's speaker plays of x: little below 400 Hz.
func played(x []int16) []int16 {
	f := make([]float64, len(x))
	for i, v := range x {
		f[i] = float64(v)
	}
	highPass(f, ReolinkTalkRate, 400)
	highPass(f, ReolinkTalkRate, 400)
	out := make([]int16, len(x))
	for i, v := range f {
		out[i] = int16(v)
	}
	return out
}

func loudest(x []int16) float64 {
	top := 0.0
	for _, v := range x {
		top = max(top, math.Abs(float64(v)))
	}
	return top
}

func rms(x []int16) float64 {
	sum := 0.0
	for _, v := range x {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(x)))
}

// The words reach 90% of full scale and are never clipped; evened out and
// without the low end a doorbell's speaker can't play, they come out
// louder than raising them alone would (#667).
func TestLouderForADoorbell(t *testing.T) {
	words := speech(ReolinkTalkRate)
	peak := 0
	for _, v := range words {
		peak = max(peak, int(math.Abs(float64(v))))
	}
	got := louder(words, ReolinkTalkRate)
	top := 0
	for _, v := range got {
		top = max(top, int(math.Abs(float64(v))))
	}
	if top < 29000 || top > 29491 {
		t.Errorf("the loudest moment is %d of 32767", top)
	}
	// What the speaker plays of each, against the words raised alone.
	raised := make([]int16, len(words))
	for i, v := range words {
		raised[i] = int16(float64(v) * talkPeak * 32767 / float64(peak))
	}
	if db := 20 * math.Log10(rms(played(got))/rms(played(raised))); db < 4 {
		t.Errorf("only %.1f dB louder than raising the words alone", db)
	}

	// A hum too low for the speaker is left out, and a voice's middle kept.
	tone := func(hz float64) float64 {
		x := make([]float64, ReolinkTalkRate)
		for i := range x {
			x[i] = math.Sin(2 * math.Pi * hz * float64(i) / ReolinkTalkRate)
		}
		highPass(x, ReolinkTalkRate, talkLowCut)
		return peakOf(x[ReolinkTalkRate/2:])
	}
	if hum, voice := tone(60), tone(1000); hum > 0.1 || voice < 0.95 {
		t.Errorf("60 Hz kept %.2f, 1 kHz kept %.2f", hum, voice)
	}
	// Very quiet words: their loudest moment is raised at most four times.
	quiet := make([]int16, len(words))
	for i, v := range words {
		quiet[i] = v / 100
	}
	if r := loudest(louder(quiet, ReolinkTalkRate)) / loudest(quiet); r > talkGain*1.01 {
		t.Errorf("quiet words raised %.1f times", r)
	}
	if got := louder([]int16{0, 0}, ReolinkTalkRate); got[0] != 0 {
		t.Errorf("silence: %v", got)
	}
}
