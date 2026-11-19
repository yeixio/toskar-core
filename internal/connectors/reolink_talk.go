package connectors

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Talkback (#667): Reolink's HTTP API has no way to speak through a
// camera, so Toskar hands the audio to Reolink's own reolink-cli, which
// does, with the camera added there under a name the connection gives as
// talk_camera. The audio is raw 16-bit mono PCM at ReolinkTalkRate.

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
)

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

// CanTalk reports a connection that names its camera in reolink-cli.
func (m *Manager) CanTalk(service string) bool {
	cred, err := m.credential(service)
	return err == nil && service == "reolink" && strings.TrimSpace(cred["talk_camera"]) != ""
}

func reolinkTalk(ctx context.Context, cred Credential, samples []int16, rate int) error {
	camera := strings.TrimSpace(cred["talk_camera"])
	if camera == "" {
		return errors.New("to speak through the camera, add it to reolink-cli and give its name as the connection's Speaker")
	}
	if !talkNameRe.MatchString(camera) {
		return fmt.Errorf("%q isn't a reolink-cli camera name", camera)
	}
	gateway := strings.TrimSpace(cred["talk_gateway"])
	if gateway != "" && !talkAddrRe.MatchString(gateway) {
		return fmt.Errorf("%q isn't an address such as 127.0.0.1:9000", gateway)
	}
	if rate <= 0 || len(samples) == 0 {
		return errors.New("nothing to say")
	}
	bin, err := reolinkCLI()
	if err != nil {
		return err
	}
	pcm := make([]byte, 0, 2*(len(samples)+int(int64(rate)*int64(reolinkTail)/int64(time.Second))))
	for _, v := range samples {
		pcm = binary.LittleEndian.AppendUint16(pcm, uint16(v))
	}
	pcm = append(pcm, make([]byte, 2*int(int64(rate)*int64(reolinkTail)/int64(time.Second)))...)
	length := time.Duration(len(pcm)/2) * time.Second / time.Duration(rate)

	ensureGateway(ctx, bin, gateway, false)
	err = talkOnce(ctx, bin, camera, gateway, pcm, rate, length)
	if unreachable(err) && localGateway(gateway) && ctx.Err() == nil {
		// A stale gateway: once more, after restarting it.
		ensureGateway(ctx, bin, gateway, true)
		err = talkOnce(ctx, bin, camera, gateway, pcm, rate, length)
	}
	return err
}

func talkOnce(ctx context.Context, bin, camera, gateway string, pcm []byte, rate int, length time.Duration) error {
	args := []string{}
	if gateway != "" {
		args = append(args, "--gateway-addr", gateway)
	}
	args = append(args, "--camera", camera, "audio", "talk", "--stdin", "--sample-rate", strconv.Itoa(rate))
	ctx, cancel := context.WithTimeout(ctx, length+20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
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
			return fmt.Errorf("reolink-cli couldn't speak through %s: %s", camera, talkError(out.Error))
		}
	}
	if runErr != nil {
		detail := strings.TrimSpace(string(lastLine(stderr.Bytes())))
		if detail == "" {
			detail = runErr.Error()
		}
		return fmt.Errorf("reolink-cli couldn't speak through %s: %s", camera, detail)
	}
	return fmt.Errorf("reolink-cli didn't say it spoke through %s", camera)
}

// lastLine is the last non-empty line of a command's output.
func lastLine(b []byte) []byte {
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	return bytes.TrimSpace(lines[len(lines)-1])
}

// talkError is reolink-cli's error, which is a message or an object with one.
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
