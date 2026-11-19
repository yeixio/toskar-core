package connectors

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Listening at the door (#667): Reolink's HTTP API has no microphone, but
// reolink-cli's live preview carries it, AAC at 16 kHz, inside the preview's
// BcMedia frames, written to a file or a named pipe. Toskar reads the pipe,
// keeps the audio, and has ffmpeg decode it to 16-bit samples.

// ReolinkListenRate is the rate the doorbell's microphone is heard at, and
// the rate speech to text listens at.
const ReolinkListenRate = 16000

// listenChunk is how much audio each read gives: 20 ms.
const listenChunk = ReolinkListenRate / 50

// ffmpegBin finds ffmpeg: on the PATH, or where Homebrew and most Linux
// installs put it, since an app started from the Dock has a short PATH.
// Tests replace it.
var ffmpegBin = func() (string, error) {
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/usr/bin/ffmpeg"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("ffmpeg isn't installed on this computer; install it (on a Mac, brew install ffmpeg) to hear the camera")
}

// Hearing is a camera's microphone while it's heard: Audio gives 16-bit mono
// samples at ReolinkListenRate as they arrive, and closes when listening
// ends; Err then says why, or nil when it was stopped.
type Hearing struct {
	Audio <-chan []int16
	mu    sync.Mutex
	err   error
}

func (h *Hearing) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

func (h *Hearing) fail(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.err == nil {
		h.err = err
	}
}

// Listen hears the service's camera until ctx ends.
func (m *Manager) Listen(ctx context.Context, service string) (*Hearing, error) {
	s, err := m.service(service)
	if err != nil {
		return nil, err
	}
	if service != "reolink" {
		return nil, fmt.Errorf("%s has no microphone Toskar can hear", s.Name())
	}
	cred, err := m.credential(service)
	if err != nil {
		return nil, fmt.Errorf("%s is not connected. Connect it in Settings", s.Name())
	}
	h, err := reolinkListen(ctx, cred)
	if err != nil {
		return nil, sanitizeErr(err, secretsOnly(s, cred))
	}
	return h, nil
}

func reolinkListen(ctx context.Context, cred Credential) (*Hearing, error) {
	camera := strings.TrimSpace(cred["talk_camera"])
	if camera == "" {
		return nil, errors.New("to hear the camera, add it to reolink-cli and give its name as the connection's Speaker")
	}
	if !talkNameRe.MatchString(camera) {
		return nil, fmt.Errorf("%q isn't a reolink-cli camera name", camera)
	}
	gateway := strings.TrimSpace(cred["talk_gateway"])
	if gateway != "" && !talkAddrRe.MatchString(gateway) {
		return nil, fmt.Errorf("%q isn't an address such as 127.0.0.1:9000", gateway)
	}
	bin, err := reolinkCLI()
	if err != nil {
		return nil, err
	}
	ff, err := ffmpegBin()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "toskar-door-")
	if err != nil {
		return nil, err
	}
	pipe := filepath.Join(dir, "preview")
	if err := makeFIFO(pipe); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	args := []string{}
	if gateway != "" {
		args = append(args, "--gateway-addr", gateway)
	}
	// The sub stream: its audio is the same, its video the smallest.
	args = append(args, "--camera", camera, "preview", "capture", "--stream", "sub", "--packets", "100000000", "--file", pipe)
	cli := exec.CommandContext(ctx, bin, args...)
	var cliOut strings.Builder
	cli.Stdout = &cliOut
	cli.Stderr = &cliOut
	dec := exec.CommandContext(ctx, ff, "-hide_banner", "-loglevel", "error", "-fflags", "nobuffer", "-probesize", "32", "-analyzeduration", "0",
		"-f", "aac", "-i", "pipe:0", "-f", "s16le", "-ac", "1", "-ar", fmt.Sprint(ReolinkListenRate), "-flush_packets", "1", "pipe:1")
	decIn, err := dec.StdinPipe()
	if err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	decOut, err := dec.StdoutPipe()
	if err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err := dec.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("couldn't start ffmpeg: %w", err)
	}
	if err := cli.Start(); err != nil {
		cancel()
		_ = dec.Wait()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("couldn't start reolink-cli: %w", err)
	}
	audio := make(chan []int16, 500) // 10 s
	h := &Hearing{Audio: audio}

	// reolink-cli: when it ends, the pipe's reader is let go, even if it
	// never opened the pipe.
	cliDone := make(chan struct{})
	go func() {
		err := cli.Wait()
		if ctx.Err() == nil {
			line := lastLine([]byte(cliOut.String()))
			detail := strings.TrimSpace(string(line))
			var out struct {
				Error any `json:"error"`
			}
			if json.Unmarshal(line, &out) == nil && out.Error != nil {
				detail = talkError(out.Error)
			}
			if detail == "" && err != nil {
				detail = err.Error()
			}
			h.fail(fmt.Errorf("reolink-cli stopped hearing %s: %s", camera, detail))
		}
		close(cliDone)
		if w, err := openFIFOWriter(pipe); err == nil {
			_ = w.Close()
		}
	}()
	// The preview's audio, into ffmpeg.
	go func() {
		defer decIn.Close()
		f, err := os.Open(pipe)
		if err != nil {
			h.fail(err)
			return
		}
		defer f.Close()
		if err := bcmediaAudio(f, func(b []byte) error { _, err := decIn.Write(b); return err }); err != nil && ctx.Err() == nil {
			h.fail(err)
		}
	}()
	// ffmpeg's samples, out.
	go func() {
		defer func() {
			cancel()
			_ = dec.Wait()
			<-cliDone
			_ = os.RemoveAll(dir)
			close(audio)
		}()
		buf := make([]byte, 2*listenChunk)
		for {
			if _, err := io.ReadFull(decOut, buf); err != nil {
				return
			}
			samples := make([]int16, listenChunk)
			for i := range samples {
				samples[i] = int16(binary.LittleEndian.Uint16(buf[2*i:]))
			}
			select {
			case audio <- samples:
			default:
				// Nobody's reading: the oldest is what's dropped, by not
				// keeping this one; the reader is only behind while it
				// speaks, when the door isn't heard anyway.
			}
		}
	}()
	return h, nil
}
