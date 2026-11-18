package speech

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Kokoro (Kokoro-82M, Apache 2.0, run through kokoro-onnx, MIT) is live
// voice mode's natural voice: it sounds much closer to a person than Piper,
// and is still quick on a laptop's processor. It speaks fewer languages, so
// Piper reads the rest.

// kokoroFile is one of Kokoro's model files, pinned by size and checksum.
type kokoroFile struct {
	Name string
	Size int64
	Sum  string
}

// kokoroRelease is where kokoro-onnx publishes the model files.
const kokoroRelease = "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/"

// kokoroFiles are the 8-bit model (92 MB) and the voices (28 MB).
var kokoroFiles = []kokoroFile{
	{"kokoro-v1.0.int8.onnx", 92361271, "6e742170d309016e5891a994e1ce1559c702a2ccd0075e67ef7157974f6406cb"},
	{"voices-v1.0.bin", 28214398, "bca610b8308e8d99f32e6fe4197e7ec01679264efed0cac9140fe9c29f1fbf7d"},
}

// naturalVoices is the Kokoro voice for each language it speaks well, by
// language and, where it has one, region.
var naturalVoices = map[string]string{
	"en":    "af_heart",
	"en-gb": "bf_emma",
	"es":    "ef_dora",
	"fr":    "ff_siwis",
	"it":    "if_sara",
	"pt":    "pf_dora",
}

// kokoroLangs is the espeak language for a Kokoro voice, by its first
// letter: af_heart is American English.
var kokoroLangs = map[byte]string{'a': "en-us", 'b': "en-gb", 'e': "es", 'f': "fr-fr", 'i': "it", 'p': "pt-br"}

// kokoroRe is a Kokoro voice name in a language it's used for, such as
// af_heart or bm_george.
var kokoroRe = regexp.MustCompile(`^[abefip][fm]_[a-z]+$`)

// NaturalVoiceFor is the Kokoro voice for lang, such as af_heart for en.
func NaturalVoiceFor(lang string) (string, bool) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if v, ok := naturalVoices[lang]; ok {
		return v, true
	}
	base, _, _ := strings.Cut(lang, "-")
	v, ok := naturalVoices[base]
	return v, ok
}

// kokoroDir holds Kokoro's model files.
func (e *Engine) kokoroDir() string { return filepath.Join(e.Dir, "kokoro") }

// kokoroReady is whether Kokoro's files are all downloaded.
func kokoroReady(dir string) bool {
	for _, f := range kokoroFiles {
		st, err := os.Stat(filepath.Join(dir, f.Name))
		if err != nil || st.Size() != f.Size {
			return false
		}
	}
	return true
}

// fetchKokoro downloads Kokoro's files into dir from base, checking each
// one's size and checksum.
func fetchKokoro(ctx context.Context, client *http.Client, base, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, f := range kokoroFiles {
		path := filepath.Join(dir, f.Name)
		if st, err := os.Stat(path); err == nil && st.Size() == f.Size {
			continue
		}
		if err := fetchFile(ctx, client, base+f.Name, path, f); err != nil {
			return fmt.Errorf("couldn't download the natural voice (%s): %w", f.Name, err)
		}
	}
	return nil
}

func fetchFile(ctx context.Context, client *http.Client, url, path string, f kokoroFile) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "toskar-daemon")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	part := path + ".part"
	out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, f.Size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != f.Size {
		err = fmt.Errorf("expected %d bytes, got %d", f.Size, n)
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != f.Sum {
		err = fmt.Errorf("the download did not match its checksum")
	}
	if err != nil {
		_ = os.Remove(part)
		return err
	}
	return os.Rename(part, path)
}
