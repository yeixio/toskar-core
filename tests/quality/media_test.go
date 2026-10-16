package quality

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// The media set (#510): pictures, edits, video, and speech Toskar makes,
// judged for what they show or say, not only that a file came back. Every
// case checks the file itself (a picture decodes at the size asked, a WAV
// is whole and long enough); against a real daemon, a model that sees is
// asked whether the picture or the clip's frames show what was asked, and
// speech is transcribed back and compared word by word. The stub checks
// the plumbing with its stand-in tools.

// Judge is how a made file is judged.
type Judge struct {
	// Kind is image, video, or audio; Ext the file's extension.
	Kind string `json:"kind"`
	Ext  string `json:"ext"`
	// Shows is what a picture or a clip must show, asked of a model that
	// sees as "Does this show …?".
	Shows string `json:"shows"`
	// Says is what speech must say; up to MaxWER of its words may differ.
	Says   string  `json:"says"`
	MaxWER float64 `json:"max_wer"`
	// MinSide is the smallest a picture's sides may be, in pixels.
	MinSide int `json:"min_side"`
	// Moves asks the judge whether a clip's frames change, so a frozen
	// clip fails (#553).
	Moves bool `json:"moves,omitempty"`
	// Clean asks the judge whether any part of the picture, or any of a
	// clip's frames, is garbled or corrupted, or a frame flashes or jumps
	// apart from the frames around it, so a broken decode fails even when
	// the clip shows the right thing (#553).
	Clean bool `json:"clean,omitempty"`
}

// mediaCase is a case in media.json.
type mediaCase struct {
	Case
	// Judge is how the file the turn makes is judged; nil for a case that
	// reads a file instead, judged by its answer.
	Judge *Judge `json:"judge"`
	// Recording is text Toskar's own voice reads into a WAV that's attached
	// to the message (real only), for a transcription case.
	Recording string `json:"recording"`
	// OS limits the case to computers running it, such as darwin for a
	// regression in Apple's video decode path (#553); empty runs anywhere.
	OS string `json:"os,omitempty"`
}

type mediaFile struct {
	Cases []mediaCase `json:"cases"`
}

func loadMediaCases(t *testing.T) []mediaCase {
	t.Helper()
	raw, err := os.ReadFile("media.json")
	if err != nil {
		t.Fatal(err)
	}
	var f mediaFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f.Cases
}

// madeFile is the first file the turn made with ext.
func madeFile(r Result, ext string) (string, []byte) {
	for name, data := range r.Files {
		if strings.EqualFold(filepath.Ext(name), ext) {
			return name, data
		}
	}
	return "", nil
}

// fileFailures checks a made file by itself: there, whole, and big enough.
func fileFailures(j *Judge, r Result, real bool) []string {
	name, data := madeFile(r, j.Ext)
	if name == "" {
		return []string{fmt.Sprintf("no %s was made", j.Ext)}
	}
	switch j.Kind {
	case "image":
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return []string{fmt.Sprintf("%s doesn't decode: %v", name, err)}
		}
		// The stub's stand-in is small; a real picture must be the size of one.
		if b := img.Bounds(); real && (b.Dx() < j.MinSide || b.Dy() < j.MinSide) {
			return []string{fmt.Sprintf("%s is %dx%d; each side should be at least %d", name, b.Dx(), b.Dy(), j.MinSide)}
		}
		// A picture of one flat color, such as all black, is a failed run,
		// whatever a judge makes of it (#553).
		if real && flat(img) {
			return []string{fmt.Sprintf("%s is blank: nearly every pixel is the same color", name)}
		}
	case "audio":
		seconds, err := wavSeconds(data)
		if err != nil {
			return []string{fmt.Sprintf("%s isn't a whole WAV: %v", name, err)}
		}
		if seconds < 0.5 {
			return []string{fmt.Sprintf("%s is %.2f s long", name, seconds)}
		}
	case "video":
		if real && len(data) < 10_000 {
			return []string{fmt.Sprintf("%s is only %d bytes", name, len(data))}
		}
	}
	return nil
}

// wavSeconds reads a PCM WAV's header and checks its data is all there.
func wavSeconds(data []byte) (float64, error) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a WAV file")
	}
	var rate, bytesPerSec uint32
	pos := 12
	for pos+8 <= len(data) {
		id, size := string(data[pos:pos+4]), int(binary.LittleEndian.Uint32(data[pos+4:pos+8]))
		body := pos + 8
		switch id {
		case "fmt ":
			if size < 16 || body+16 > len(data) {
				return 0, fmt.Errorf("short fmt chunk")
			}
			rate = binary.LittleEndian.Uint32(data[body+4 : body+8])
			bytesPerSec = binary.LittleEndian.Uint32(data[body+8 : body+12])
		case "data":
			if rate == 0 || bytesPerSec == 0 {
				return 0, fmt.Errorf("data before fmt")
			}
			if body+size > len(data) {
				return 0, fmt.Errorf("the data stops at %d of %d bytes", len(data)-body, size)
			}
			return float64(size) / float64(bytesPerSec), nil
		}
		pos = body + size + size%2
	}
	return 0, fmt.Errorf("no data chunk")
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}']+`)

// wordErrorRate is how many words of got differ from want (substituted,
// left out, or added), as a share of want's words.
func wordErrorRate(want, got string) float64 {
	w := wordRe.FindAllString(strings.ToLower(want), -1)
	g := wordRe.FindAllString(strings.ToLower(got), -1)
	if len(w) == 0 {
		return 0
	}
	prev := make([]int, len(g)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(w); i++ {
		cur := make([]int, len(g)+1)
		cur[0] = i
		for j := 1; j <= len(g); j++ {
			cost := 1
			if w[i-1] == g[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return float64(prev[len(g)]) / float64(len(w))
}

// judgeFailures asks a real daemon about a made file: a model that sees
// whether a picture or a clip shows what was asked, or a transcription of
// speech compared with what it should say.
func judgeFailures(t *testing.T, d realDriver, j *Judge, r Result) []string {
	name, data := madeFile(r, j.Ext)
	if name == "" {
		return nil // fileFailures said so
	}
	switch j.Kind {
	case "image", "video":
		what := "picture"
		if j.Kind == "video" {
			what = "video"
		}
		answer, err := d.askAbout(t, judgeModel(), name, data,
			fmt.Sprintf("Does this %s show %s? Answer yes or no on the first line, then say in one sentence what it shows.", what, j.Shows))
		if err != nil {
			return []string{fmt.Sprintf("the judge couldn't look at %s: %v", name, err)}
		}
		if !saysYes(answer) {
			return []string{fmt.Sprintf("the judge says %s doesn't show %s: %.200q", name, j.Shows, answer)}
		}
		if j.Kind == "video" && j.Moves {
			moving, err := d.askAbout(t, judgeModel(), name, data,
				"Do these frames of a video change from one to the next, with something moving, or are they all the same picture? Answer yes if something moves or changes, or no if they're the same, on the first line.")
			if err != nil {
				return []string{fmt.Sprintf("the judge couldn't look at %s: %v", name, err)}
			}
			if !saysYes(moving) {
				return []string{fmt.Sprintf("the judge says nothing moves in %s: %.200q", name, moving)}
			}
		}
		if j.Clean {
			broken, err := d.askAbout(t, judgeModel(), name, data, cleanQuestion(j.Kind))
			if err != nil {
				return []string{fmt.Sprintf("the judge couldn't look at %s: %v", name, err)}
			}
			if saysYes(broken) {
				return []string{fmt.Sprintf("the judge says %s is corrupted: %.200q", name, broken)}
			}
		}
	case "audio":
		answer, err := d.askAbout(t, d.modelID(), name, data, "Transcribe this recording word for word. Reply with only the words it says.")
		if err != nil {
			return []string{fmt.Sprintf("couldn't transcribe %s: %v", name, err)}
		}
		limit := j.MaxWER
		if limit == 0 {
			limit = 0.25
		}
		if wer := wordErrorRate(j.Says, answer); wer > limit {
			return []string{fmt.Sprintf("%s says %.200q; %.0f%% of its words differ from %q (at most %.0f%%)", name, answer, wer*100, j.Says, limit*100)}
		}
	}
	return nil
}

// cleanQuestion asks about decode corruption: blocks, noise, stripes,
// smears, and, for a clip, a frame that flashes or jumps. Yes means broken.
func cleanQuestion(kind string) string {
	if kind == "video" {
		return "Look closely at each of these frames of a video. Is any frame garbled or corrupted, with blocks of solid or wrong color, " +
			"noise, stripes, smears, or a scrambled area, or does any frame flash or jump, looking unlike the frames before and after it? " +
			"Answer no on the first line if every frame looks clean and each follows smoothly from the one before; otherwise answer yes and say which frame and what's wrong."
	}
	return "Look closely at this picture. Is any part of it garbled or corrupted, with blocks of solid or wrong color, noise, stripes, " +
		"smears, or a scrambled area? Answer no on the first line if it looks clean; otherwise answer yes and say where and what's wrong."
}

// judgeModel is the model that sees, asked about pictures and clips.
func judgeModel() string {
	if m := config.Env("QUALITY_JUDGE_MODEL"); m != "" {
		return m
	}
	return "gemma-3-4b-q4"
}

// askAbout sends a file to a fresh chat with model and returns the answer.
func (d realDriver) askAbout(t *testing.T, model, name string, data []byte, question string) (string, error) {
	var conv struct {
		ID string `json:"id"`
	}
	if _, err := d.request(http.MethodPost, "/api/v1/conversations", map[string]any{"title": "Quality judge", "profile_id": "general-assistant", "model_id": model}, &conv, t.Logf); err != nil {
		return "", err
	}
	defer func() { _, _ = d.request(http.MethodDelete, "/api/v1/conversations/"+conv.ID, nil, nil, t.Logf) }()
	var saved struct {
		ID string `json:"id"`
	}
	if _, err := d.request(http.MethodPost, "/api/v1/artifacts", map[string]any{
		"name": name, "content_base64": base64.StdEncoding.EncodeToString(data), "conversation_id": conv.ID,
	}, &saved, t.Logf); err != nil {
		return "", err
	}
	if _, err := d.request(http.MethodPost, "/api/v1/chat", map[string]any{
		"conversation_id": conv.ID, "profile_id": "general-assistant", "model_id": model, "message": question, "stream": false,
		"attachments": []string{saved.ID},
	}, nil, t.Logf); err != nil {
		return "", err
	}
	var msgs []contracts.Message
	if _, err := d.request(http.MethodGet, "/api/v1/conversations/"+conv.ID+"/messages", nil, &msgs, t.Logf); err != nil {
		return "", err
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return msgs[i].Content, nil
		}
	}
	return "", fmt.Errorf("no answer")
}

// recording is text read by Toskar's own voice, as a WAV.
func (d realDriver) recording(t *testing.T, text string) []byte {
	t.Helper()
	var out struct {
		Artifact struct {
			ID string `json:"id"`
		} `json:"artifact"`
	}
	d.do(t, http.MethodPost, "/api/v1/speech", map[string]any{"text": text}, &out)
	var data rawBody
	if _, err := d.request(http.MethodGet, "/api/v1/artifacts/"+out.Artifact.ID+"/content", nil, &data, t.Logf); err != nil {
		t.Fatal(err)
	}
	return data
}

// installMedia sets up picture and video making, a model that sees, and
// speech, for a daemon started fresh for the run (TOSKAR_QUALITY_MEDIA_INSTALL).
func (d realDriver) installMedia(t *testing.T) {
	t.Helper()
	// TOSKAR_QUALITY_MEDIA_IMAGE_MODEL tests another picture model, such as
	// one not yet tested on real hardware (#548). FLUX.2 [klein] 4B is set
	// up first anyway, so edits have a model that edits.
	steps := []struct{ path, model string }{{"/api/v1/images/setup", "flux2-klein-4b"}}
	if m := config.Env("QUALITY_MEDIA_IMAGE_MODEL"); m != "" && m != "flux2-klein-4b" {
		steps = append(steps, struct{ path, model string }{"/api/v1/images/setup", m})
	}
	// TOSKAR_QUALITY_MEDIA_VIDEO_MODEL does the same for clips; Wan 2.2
	// TI2V 5B is set up first, for clips from pictures.
	// A model that needs a license is tested with the token of an account
	// that accepted it (TOSKAR_QUALITY_HF_TOKEN), and the set confirms the
	// license for that run (#551).
	if token := config.Env("QUALITY_HF_TOKEN"); token != "" {
		d.do(t, http.MethodPatch, "/api/v1/settings", map[string]any{"huggingface_token": token}, nil)
	}
	steps = append(steps, struct{ path, model string }{"/api/v1/video/setup", "wan2.2-ti2v-5b"})
	if m := config.Env("QUALITY_MEDIA_VIDEO_MODEL"); m != "" && m != "wan2.2-ti2v-5b" {
		steps = append(steps, struct{ path, model string }{"/api/v1/video/setup", m})
	}
	for _, s := range steps {
		var status struct {
			Ready  bool   `json:"ready"`
			Active string `json:"active"`
			Models []struct {
				ID        string `json:"id"`
				Installed bool   `json:"installed"`
				EditOnly  bool   `json:"edit_only"`
			} `json:"models"`
			Job *struct {
				Running bool   `json:"running"`
				Error   string `json:"error"`
			} `json:"job"`
		}
		d.do(t, http.MethodGet, s.path, nil, &status)
		// Setting a model up makes it the active one, except a model that
		// only edits, which is used for edits beside it: that one is done
		// when it's installed.
		done := func() bool {
			for _, m := range status.Models {
				if m.ID == s.model && m.EditOnly {
					return m.Installed
				}
			}
			return status.Ready && status.Active == s.model
		}
		if done() {
			continue
		}
		if status.Ready {
			t.Logf("setting up %s beside %s", s.model, status.Active)
		} else {
			t.Logf("setting up %s", s.model)
		}
		// The set agrees to a model that's slow or near the limit here: it
		// measures it (#550).
		d.do(t, http.MethodPost, s.path, map[string]any{"model_id": s.model, "accept_tight": true, "accept_license": true}, nil)
		deadline := time.Now().Add(installWithin)
		for !done() {
			if time.Now().After(deadline) {
				t.Fatalf("%s wasn't ready within %s", s.model, installWithin)
			}
			time.Sleep(10 * time.Second)
			d.do(t, http.MethodGet, s.path, nil, &status)
			if status.Job != nil && !status.Job.Running && status.Job.Error != "" {
				t.Fatalf("setting up %s failed: %s", s.model, status.Job.Error)
			}
		}
	}
	d.installModel(t, judgeModel())
	// Speech installs on first use; one sentence now keeps that out of the cases.
	_ = d.recording(t, "Getting ready.")
}

// TestMediaCasesValid checks the set: attachments build, judges name a
// kind and an extension, and what's judged says what it must show or say.
func TestMediaCasesValid(t *testing.T) {
	for _, c := range loadMediaCases(t) {
		for _, a := range c.Attach {
			if _, err := a.Bytes(); err != nil {
				t.Errorf("%s: %v", c.ID, err)
			}
		}
		if c.OS != "" && c.OS != "darwin" && c.OS != "linux" && c.OS != "windows" {
			t.Errorf("%s: os %q", c.ID, c.OS)
		}
		if j := c.Judge; j != nil {
			if j.Clean && j.Kind == "audio" {
				t.Errorf("%s: clean is for pictures and clips", c.ID)
			}
			if j.Ext == "" || (j.Kind != "image" && j.Kind != "video" && j.Kind != "audio") {
				t.Errorf("%s: judge %+v", c.ID, j)
			}
			if (j.Kind == "audio") == (j.Says == "") || (j.Kind != "audio" && j.Shows == "") {
				t.Errorf("%s: a judge needs shows, or says for audio", c.ID)
			}
		}
	}
	for want, got := range map[float64]float64{
		0:    wordErrorRate("The quick brown fox", "the quick, brown fox."),
		0.25: wordErrorRate("The quick brown fox", "the quick brown box"),
		0.5:  wordErrorRate("The quick brown fox", "quick fox"),
	} {
		if got != want {
			t.Errorf("word error rate %v, want %v", got, want)
		}
	}
}

// TestMediaQuality runs the media set. TOSKAR_QUALITY_MEDIA_REPORT writes
// a report; TOSKAR_QUALITY_MEDIA_MIN_PASS (default 0.75) is the share a
// real daemon must pass, since a picture is judged by another model.
func TestMediaQuality(t *testing.T) {
	// A clip from Wan 2.2 A14B takes well over ten minutes (#548).
	saved := requestTimeout
	requestTimeout = 80 * time.Minute
	t.Cleanup(func() { requestTimeout = saved })
	d, build := qualityDriver(t)
	real, isReal := d.(realDriver)
	if isReal && config.Env("QUALITY_MEDIA_INSTALL") != "" {
		real.installMedia(t)
	}
	var report []reportRow
	var peaks []memoryPeak
	for _, mc := range loadMediaCases(t) {
		if mc.RealOnly && !isReal {
			continue
		}
		if mc.OS != "" && mc.OS != runtime.GOOS {
			continue
		}
		if stopped(t, d, mc.Case) {
			break
		}
		c := mc.Case
		var failures []string
		recorded := false
		t.Run(c.ID, func(t *testing.T) {
			if mc.Recording != "" {
				c.Attach = append(c.Attach, Attachment{Name: "recording.wav", Content: string(real.recording(t, mc.Recording))})
			}
			var stop func() memoryPeak
			if isReal {
				stop = real.watchMemory(c.ID)
			}
			r := d.Run(t, c)
			if stop != nil {
				peaks = append(peaks, stop())
			}
			failures = check(d.Name(), c, r, nil)
			if j := mc.Judge; j != nil {
				failures = append(failures, fileFailures(j, r, isReal)...)
				if isReal {
					failures = append(failures, judgeFailures(t, real, j, r)...)
				}
			}
			recorded = true
			for _, f := range failures {
				if d.Name() == "stub" {
					t.Error(f)
				} else {
					t.Log("FAIL: " + f)
				}
			}
			report = append(report, reportRow{Case: c, Answer: r.Answer, Failures: failures})
		})
		if !recorded {
			report = append(report, reportRow{Case: c, Failures: []string{"the case stopped before it could be checked; see its log"}})
		}
	}
	passed := len(report) - failedRows(report)
	rate := share(passed, len(report))
	min := 1.0
	if isReal {
		min = 0.75
		if v, err := strconv.ParseFloat(config.Env("QUALITY_MEDIA_MIN_PASS"), 64); err == nil && v > 0 && v <= 1 {
			min = v
		}
	}
	t.Logf("media: %d of %d cases passed (%.0f%%) with the %s model", passed, len(report), rate*100, d.Name())
	seen, sees := visionScore(report)
	if sees > 0 {
		t.Logf("sees: %d of %d", seen, sees)
	}
	if path := config.Env("QUALITY_MEDIA_REPORT"); path != "" {
		md := strings.Replace(markdownReport(d.Name(), report, rate, min), "# Chat quality:", "# Media quality:", 1)
		models := ""
		if isReal {
			models = fmt.Sprintf("Picture model: %s. Clip model: %s.", orDefault(config.Env("QUALITY_MEDIA_IMAGE_MODEL"), "flux2-klein-4b"), orDefault(config.Env("QUALITY_MEDIA_VIDEO_MODEL"), "wan2.2-ti2v-5b"))
		}
		if sees > 0 {
			md += fmt.Sprintf("\n**Sees: %d of %d.** The cases that show the model a picture or a video and check what it says about it (#449).\n", seen, sees)
		}
		md += memoryTable(models, peaks)
		writeReport(t, d, build, path, md)
	}
	if rate < min {
		t.Errorf("%.0f%% of cases passed; at least %.0f%% must", rate*100, min*100)
	}
}

// visionScore counts the cases that show the model a picture or a video and
// judge its answer, and how many passed, for the report's "sees" line
// (#449).
func visionScore(report []reportRow) (passed, total int) {
	for _, row := range report {
		if !shows(row.Case) {
			continue
		}
		total++
		if len(row.Failures) == 0 {
			passed++
		}
	}
	return passed, total
}

// shows reports a case about what the model sees: one that checks a
// picture reached it, or that shows it a video. An edit attaches a picture
// too, but is judged by the picture it makes.
func shows(c Case) bool {
	if c.Expect.ImageSent {
		return true
	}
	for _, a := range c.Attach {
		switch strings.ToLower(filepath.Ext(a.Name)) {
		case ".mp4", ".mov", ".webm":
			return true
		}
	}
	return false
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// saysYes reads a judge's yes-or-no first line.
func saysYes(answer string) bool {
	return strings.HasPrefix(strings.ToLower(strings.Trim(strings.TrimSpace(answer), "*`\"'")), "yes")
}

// flat reports a picture of nearly one color: samples across it differ
// from their average by almost nothing.
func flat(img image.Image) bool {
	b := img.Bounds()
	var sum, sq [3]float64
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y += max(1, b.Dy()/64) {
		for x := b.Min.X; x < b.Max.X; x += max(1, b.Dx()/64) {
			r, g, bl, _ := img.At(x, y).RGBA()
			for i, v := range []float64{float64(r >> 8), float64(g >> 8), float64(bl >> 8)} {
				sum[i] += v
				sq[i] += v * v
			}
			n++
		}
	}
	if n == 0 {
		return true
	}
	spread := 0.0
	for i := range sum {
		mean := sum[i] / float64(n)
		spread = max(spread, sq[i]/float64(n)-mean*mean)
	}
	// A standard deviation under 3 of 255 in every channel.
	return spread < 9
}
