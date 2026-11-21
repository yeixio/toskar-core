package connectors

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Talkback (#667): Reolink's HTTP API has no way to speak through a
// camera, so Toskar hands the audio to Reolink's own reolink-cli, which
// does. It reaches the connection's own camera, at its address and
// channel with its login, so nobody has to add the camera to reolink-cli
// or name it; a connection may still name one (talk_camera). The audio is
// raw 16-bit mono PCM at ReolinkTalkRate.

// ReolinkTalkRate is the rate reolink-cli's audio talk expects by default.
const ReolinkTalkRate = 16000

// reolinkTail is the silence after the words: a Reolink doorbell cuts off
// the last moment of what it's sent, so the words end before it does.
const reolinkTail = 800 * time.Millisecond

// reolinkCLI finds reolink-cli: on the PATH, or where its installer puts
// it, ~/.local/bin. Tests replace it.
var reolinkCLI = func() (string, error) {
	if p, err := exec.LookPath("reolink-cli"); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", "reolink-cli")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("reolink-cli isn't installed on this computer; install it from github.com/reolink/reolink-cli to speak through the camera")
}

var (
	talkNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	talkAddrRe = regexp.MustCompile(`^[A-Za-z0-9.\-\[\]:]{1,100}$`)
	channelRe  = regexp.MustCompile(`^[0-9]{1,3}$`)
)

// DefaultGateway is where reolink-cli's gateway listens when a connection
// doesn't say: reolink-cli's own default, which Toskar starts when nothing
// listens there.
const DefaultGateway = "127.0.0.1:9000"

// reolinkPort is the port reolink-cli reaches a camera or hub on, Reolink's
// own protocol's, whatever port its HTTP API answers on.
const reolinkPort = "9000"

// reolinkTarget is how reolink-cli reaches a connection's camera.
type reolinkTarget struct {
	args    []string // the camera, before the command
	env     []string // the password, kept off the command line
	gateway string
	name    string // the camera, in messages
}

func targetOf(cred Credential) (reolinkTarget, error) {
	t := reolinkTarget{gateway: strings.TrimSpace(cred["talk_gateway"])}
	if t.gateway == "" {
		t.gateway = DefaultGateway
	}
	if !talkAddrRe.MatchString(t.gateway) {
		return t, fmt.Errorf("%q isn't an address such as 127.0.0.1:9000", t.gateway)
	}
	t.args = []string{"--gateway-addr", t.gateway}
	if camera := strings.TrimSpace(cred["talk_camera"]); camera != "" {
		if !talkNameRe.MatchString(camera) {
			return t, fmt.Errorf("%q isn't a reolink-cli camera name", camera)
		}
		t.args = append(t.args, "--camera", camera)
		t.name = camera
		return t, nil
	}
	u, err := url.Parse(strings.TrimSpace(cred["url"]))
	host := ""
	if err == nil {
		host = u.Hostname()
	}
	if host == "" || !talkAddrRe.MatchString(host) {
		return t, errors.New("the connection's camera address isn't one reolink-cli can reach")
	}
	t.args = append(t.args, "--host="+net.JoinHostPort(host, reolinkPort))
	if ch := strings.TrimSpace(cred["channel"]); ch != "" {
		if !channelRe.MatchString(ch) {
			return t, fmt.Errorf("%q isn't a channel number", ch)
		}
		t.args = append(t.args, "--channel="+ch)
	}
	if user := cred["username"]; user != "" {
		t.args = append(t.args, "--user="+user)
	}
	t.env = []string{"REOLINK_PASSWORD=" + cred["password"]}
	t.name = "the camera"
	return t, nil
}

// command runs reolink-cli's args at the target.
func (t reolinkTarget) command(ctx context.Context, bin string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, append(append([]string{}, t.args...), args...)...)
	if len(t.env) > 0 {
		cmd.Env = append(os.Environ(), t.env...)
	}
	return cmd
}

// Talk says samples, 16-bit mono at rate, through the service's camera.
func (m *Manager) Talk(ctx context.Context, service string, samples []int16, rate int) error {
	s, err := m.service(service)
	if err != nil {
		return err
	}
	if service != "reolink" {
		return fmt.Errorf("%s has no speaker Toskar can use", s.Name())
	}
	cred, err := m.credential(service)
	if err != nil {
		return fmt.Errorf("%s is not connected. Connect it in Settings", s.Name())
	}
	if err := reolinkTalk(ctx, cred, samples, rate); err != nil {
		return sanitizeErr(err, secretsOnly(s, cred))
	}
	return nil
}

// CanTalk reports a Reolink connection and reolink-cli to speak through it.
func (m *Manager) CanTalk(service string) bool {
	if service != "reolink" {
		return false
	}
	cred, err := m.credential(service)
	if err != nil {
		return false
	}
	if _, err := targetOf(cred); err != nil {
		return false
	}
	_, err = reolinkCLI()
	return err == nil
}

func reolinkTalk(ctx context.Context, cred Credential, samples []int16, rate int) error {
	t, err := targetOf(cred)
	if err != nil {
		return err
	}
	if rate <= 0 || len(samples) == 0 {
		return errors.New("nothing to say")
	}
	bin, err := reolinkCLI()
	if err != nil {
		return err
	}
	samples = louder(samples, rate)
	pcm := make([]byte, 0, 2*(len(samples)+int(int64(rate)*int64(reolinkTail)/int64(time.Second))))
	for _, v := range samples {
		pcm = binary.LittleEndian.AppendUint16(pcm, uint16(v))
	}
	pcm = append(pcm, make([]byte, 2*int(int64(rate)*int64(reolinkTail)/int64(time.Second)))...)
	length := time.Duration(len(pcm)/2) * time.Second / time.Duration(rate)

	ensureGateway(ctx, bin, t.gateway, false)
	err = talkOnce(ctx, bin, t, pcm, rate, length)
	if unreachable(err) && localGateway(t.gateway) && ctx.Err() == nil {
		// A stale gateway: once more, after restarting it.
		ensureGateway(ctx, bin, t.gateway, true)
		err = talkOnce(ctx, bin, t, pcm, rate, length)
	}
	if unreachable(err) {
		// Said plainly, in the App language, and not retried (#1340).
		return contracts.NewError("CAMERA_SPEAKER_UNREACHABLE", nil, err)
	}
	return err
}

func talkOnce(ctx context.Context, bin string, t reolinkTarget, pcm []byte, rate int, length time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, length+20*time.Second)
	defer cancel()
	cmd := t.command(ctx, bin, "audio", "talk", "--stdin", "--sample-rate", strconv.Itoa(rate))
	cmd.Stdin = bytes.NewReader(pcm)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	var out struct {
		OK    bool `json:"ok"`
		Error any  `json:"error"`
	}
	if err := json.Unmarshal(lastLine(stdout.Bytes()), &out); err == nil {
		if out.OK {
			return nil
		}
		if out.Error != nil {
			return fmt.Errorf("reolink-cli couldn't speak through %s: %s", t.name, talkError(out.Error))
		}
	}
	if runErr != nil {
		detail := cliMessage(lastLine(stderr.Bytes()))
		if detail == "" {
			detail = runErr.Error()
		}
		return fmt.Errorf("reolink-cli couldn't speak through %s: %s", t.name, detail)
	}
	return fmt.Errorf("reolink-cli didn't say it spoke through %s", t.name)
}

// talkPeak is how loud the words' loudest moment is sent, of full scale,
// and talkGain the most they're raised. A doorbell's small speaker is
// quiet, and the voices read at a comfortable level for headphones, about
// a third of full scale: Mike found it hard to hear at the door (#667).
const (
	talkPeak = 0.9
	talkGain = 4.0
)

// A doorbell's speaker is a few centimetres across: what's below
// talkLowCut barely comes out of it, and only takes room the words could
// use. Above talkKnee of full scale, the words are pressed talkRatio to 1,
// so the quieter syllables come up nearer the loudest before all are
// raised to talkPeak. At the hub's own maximum volume, Mike still wished
// it were louder (#667).
const (
	talkLowCut = 250.0 // Hz
	talkKnee   = 0.25
	talkRatio  = 4.0
	talkFall   = 0.08 // s
)

// louder makes words at rate sound as loud as a small speaker can play
// them: it leaves out what's too low for it, evens out the syllables, and
// raises the loudest moment to talkPeak, by at most talkGain overall.
func louder(samples []int16, rate int) []int16 {
	x := make([]float64, len(samples))
	before := 0.0
	for i, v := range samples {
		x[i] = float64(v) / 32767
		before = max(before, math.Abs(x[i]))
	}
	if before == 0 || rate <= 0 {
		return samples
	}
	highPass(x, float64(rate), talkLowCut)
	// Even out the syllables, measured as if the words were at full scale.
	scale := 1 / peakOf(x)
	fall := math.Exp(-1 / (talkFall * float64(rate)))
	env := 0.0
	for i, v := range x {
		level := math.Abs(v) * scale
		env = max(level, fall*env)
		if env > talkKnee {
			x[i] = v * (talkKnee + (env-talkKnee)/talkRatio) / env
		}
	}
	peak := peakOf(x)
	gain := min(talkPeak/peak, talkGain*before/peak)
	out := make([]int16, len(x))
	for i, v := range x {
		out[i] = int16(max(-32767, min(32767, math.Round(v*gain*32767))))
	}
	return out
}

// highPass leaves out what's below cut Hz from x, sampled at rate: a
// second-order Butterworth filter.
func highPass(x []float64, rate, cut float64) {
	w := 2 * math.Pi * cut / rate
	alpha := math.Sin(w) / math.Sqrt2 // sin(w)/2Q, Q = 1/√2
	cos := math.Cos(w)
	a0 := 1 + alpha
	b0, b1, b2 := (1+cos)/2/a0, -(1+cos)/a0, (1+cos)/2/a0
	a1, a2 := -2*cos/a0, (1-alpha)/a0
	var x1, x2, y1, y2 float64
	for i, v := range x {
		y := b0*v + b1*x1 + b2*x2 - a1*y1 - a2*y2
		x2, x1, y2, y1 = x1, v, y1, y
		x[i] = y
	}
}

func peakOf(x []float64) float64 {
	peak := 1e-9
	for _, v := range x {
		peak = max(peak, math.Abs(v))
	}
	return peak
}

// lastLine is the last non-empty line of a command's output.
func lastLine(b []byte) []byte {
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	return bytes.TrimSpace(lines[len(lines)-1])
}

// talkError is reolink-cli's error, which is a message or an object with one.
// cliMessage is a line reolink-cli wrote: its error's message when it's
// reolink-cli's JSON, rather than the JSON itself (#1340).
func cliMessage(line []byte) string {
	var out struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(line, &out) == nil && out.Error != nil {
		return talkError(out.Error)
	}
	return strings.TrimSpace(string(line))
}

func talkError(v any) string {
	switch e := v.(type) {
	case string:
		return e
	case map[string]any:
		for _, k := range []string{"message", "detail", "code"} {
			if s, ok := e[k].(string); ok && s != "" {
				return s
			}
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}
