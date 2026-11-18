package webm

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// The fixtures are 64×48 VP8 clips of nine frames at 16 fps, made with
// ffmpeg: a.webm and b.webm with every frame a keyframe, as
// stable-diffusion.cpp writes them, and inter.webm with frames that depend
// on the ones before.
func load(t *testing.T, name string) *Clip {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return c
}

func TestParse(t *testing.T) {
	c := load(t, "a.webm")
	if c.Codec != "V_VP8" || c.Width != 64 || c.Height != 48 || len(c.Frames) != 9 || c.Scale != 1_000_000 {
		t.Fatalf("clip %s %dx%d, %d frames, scale %d", c.Codec, c.Width, c.Height, len(c.Frames), c.Scale)
	}
	if s := c.Seconds(); s < 0.5 || s > 0.6 {
		t.Errorf("plays %.3f s", s)
	}
	for _, bad := range [][]byte{nil, []byte("not a clip"), []byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01")} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q was read", bad)
		}
	}
}

func TestLastFrame(t *testing.T) {
	img, err := load(t, "a.webm").LastFrame()
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 48 {
		t.Fatalf("frame %v", b)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if _, kind, err := image.DecodeConfig(&buf); err != nil || kind != "png" {
		t.Fatalf("png: %s %v", kind, err)
	}
	if _, err := load(t, "inter.webm").LastFrame(); !errors.Is(err, ErrNotKeyframe) {
		t.Fatalf("an inter frame: %v", err)
	}
}

func TestJoin(t *testing.T) {
	a, b := load(t, "a.webm"), load(t, "b.webm")
	for _, continued := range []bool{false, true} {
		joined, err := Join(a, b, continued)
		if err != nil {
			t.Fatal(err)
		}
		c, err := Parse(joined)
		if err != nil {
			t.Fatal(err)
		}
		want := 18
		if continued {
			want = 17
		}
		if len(c.Frames) != want {
			t.Fatalf("continued %v: %d frames", continued, len(c.Frames))
		}
		for i := 1; i < len(c.Frames); i++ {
			if d := c.Frames[i].Time - c.Frames[i-1].Time; d < 60 || d > 65 {
				t.Fatalf("frame %d comes %d ms after the one before", i, d)
			}
		}
		if !bytes.Equal(c.Frames[8].Data, a.Frames[8].Data) || !bytes.Equal(c.Frames[want-1].Data, b.Frames[8].Data) {
			t.Fatal("frames out of order")
		}
		// ffprobe, where it's installed, reads it as one clip.
		if probe, err := exec.LookPath("ffprobe"); err == nil {
			path := filepath.Join(t.TempDir(), "joined.webm")
			_ = os.WriteFile(path, joined, 0o600)
			out, err := exec.Command(probe, "-v", "error", "-count_frames", "-select_streams", "v:0", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", path).CombinedOutput()
			if err != nil || string(bytes.TrimSpace(out)) != strconv.Itoa(want) {
				t.Fatalf("ffprobe: %s %v", out, err)
			}
		}
	}
	small := *b
	small.Width = 32
	if _, err := Join(a, &small, true); err == nil {
		t.Fatal("clips of different sizes were joined")
	}
}
