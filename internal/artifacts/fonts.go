package artifacts

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode"
)

// Made PDFs embed Noto Sans (#510), which covers Latin, Greek, and
// Cyrillic text. Chinese, Japanese, and Korean need fonts too large to
// build in, so the first PDF that needs one downloads it into the data
// folder, where later ones find it. Noto is under the SIL Open Font
// License (fonts/OFL.txt).

//go:embed fonts/NotoSans-Regular.ttf
var notoSansRegular []byte

//go:embed fonts/NotoSans-Bold.ttf
var notoSansBold []byte

var builtinFonts = sync.OnceValues(func() ([2]*ttfFont, error) {
	regular, err := parseTTF(notoSansRegular)
	if err != nil {
		return [2]*ttfFont{}, err
	}
	bold, err := parseTTF(notoSansBold)
	return [2]*ttfFont{regular, bold}, err
})

// cjkFont is a font downloaded when a PDF first needs it: Google Fonts'
// static Regular instance, as a TrueType file, checked against its hash.
type cjkFont struct {
	file, url, sha256 string
	size              int64
}

var cjkFonts = map[string]cjkFont{
	"ja": {"NotoSansJP-Regular.ttf", "https://fonts.gstatic.com/s/notosansjp/v57/-F6jfjtqLzI2JPCgQBnw7HFyzSD-AsregP8VFBEj75s.ttf",
		"f037d714109f22d57ce75fd9a6b9d23439c903061fb11106c070b2dcc03f2021", 5703608},
	"ko": {"NotoSansKR-Regular.ttf", "https://fonts.gstatic.com/s/notosanskr/v40/PbyxFmXiEBPT4ITbgNA5Cgms3VYcOA-vvnIzzuoyeLQ.ttf",
		"84e2b8acadd0a01af9f0de6b0a2d29117fb5b0eabd7c2f8353d9d2da0461d5fa", 6170740},
	"zh-Hans": {"NotoSansSC-Regular.ttf", "https://fonts.gstatic.com/s/notosanssc/v41/k3kCo84MPvpLmixcA63oeAL7Iqp5IZJF9bmaG9_FnYw.ttf",
		"450625c8d46ab3df97b7904ded955ec2746d17ec76740cb1e91d1ba63a0f89af", 10540644},
	"zh-Hant": {"NotoSansTC-Regular.ttf", "https://fonts.gstatic.com/s/notosanstc/v40/-nFuOG829Oofr2wohFbTp9ifNAn722rq0MXz76Cy_Co.ttf",
		"619662a0583f38311e92666927e5edbfd30f2a1fbe8593685660bd11bdd46a10", 7090820},
}

// Characters written differently in Traditional and Simplified Chinese,
// in pairs, to tell which a text is in.
const (
	traditionalOnly = "們這個來說時國會為學對從開關點體經過與後發電話樣種實現長問題應當東車書見門馬鳥魚愛讓還進動無氣頭業產記處聽專義歷灣"
	simplifiedOnly  = "们这个来说时国会为学对从开关点体经过与后发电话样种实现长问题应当东车书见门马鸟鱼爱让还进动无气头业产记处听专义历湾"
)

// cjkNeeds is the fonts text needs beyond Noto Sans, in the order they're
// tried: Korean for Hangul, Japanese for kana, and otherwise Chinese, as
// Traditional or Simplified by the characters it uses. The Korean and
// Japanese fonts also have the Chinese characters those languages use.
func cjkNeeds(text string) []string {
	var hangul, kana, han bool
	trad, simp := 0, 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Hangul, r):
			hangul = true
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			kana = true
		case unicode.Is(unicode.Han, r):
			han = true
			for _, t := range traditionalOnly {
				if r == t {
					trad++
				}
			}
			for _, s := range simplifiedOnly {
				if r == s {
					simp++
				}
			}
		}
	}
	var out []string
	if kana {
		out = append(out, "ja")
	}
	if hangul {
		out = append(out, "ko")
	}
	if han && !kana && !hangul {
		if trad > simp {
			out = append(out, "zh-Hant")
		} else {
			out = append(out, "zh-Hans")
		}
	}
	return out
}

// Fonts finds the Chinese, Japanese, and Korean fonts a PDF needs, in Dir,
// downloading each the first time. A nil *Fonts has none, and shows those
// characters as "?".
type Fonts struct {
	Dir    string
	Client *http.Client
	// Fetched is told of each font downloaded, with its address, for What
	// left this computer (#1415).
	Fetched func(ctx context.Context, url string)

	mu     sync.Mutex
	loaded map[string]*ttfFont
}

// MarkdownToPDF makes a PDF with the fonts its text needs.
func (fs *Fonts) MarkdownToPDF(ctx context.Context, text string) ([]byte, error) {
	var extra []*ttfFont
	if fs != nil {
		for _, key := range cjkNeeds(text) {
			f, err := fs.font(ctx, key)
			if err != nil {
				return nil, err
			}
			extra = append(extra, f)
		}
	}
	return markdownToPDF(text, extra)
}

func (fs *Fonts) font(ctx context.Context, key string) (*ttfFont, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if f := fs.loaded[key]; f != nil {
		return f, nil
	}
	spec := cjkFonts[key]
	path := filepath.Join(fs.Dir, spec.file)
	data, err := os.ReadFile(path)
	if err != nil || hashOf(data) != spec.sha256 {
		if data, err = fs.download(ctx, spec, path); err != nil {
			return nil, fmt.Errorf("the font for %s text couldn't be downloaded: %w", key, err)
		}
	}
	f, err := parseTTF(data)
	if err != nil {
		return nil, err
	}
	if fs.loaded == nil {
		fs.loaded = map[string]*ttfFont{}
	}
	fs.loaded[key] = f
	return f, nil
}

func hashOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (fs *Fonts) download(ctx context.Context, spec cjkFont, path string) ([]byte, error) {
	client := fs.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.url, nil)
	if err != nil {
		return nil, err
	}
	if fs.Fetched != nil {
		fs.Fetched(ctx, spec.url)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the font server answered %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, spec.size+1))
	if err != nil {
		return nil, err
	}
	if hashOf(data) != spec.sha256 {
		return nil, fmt.Errorf("%s isn't the font expected", spec.file)
	}
	if err := os.MkdirAll(fs.Dir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(fs.Dir, spec.file+".*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	return data, os.Rename(tmp.Name(), path)
}
