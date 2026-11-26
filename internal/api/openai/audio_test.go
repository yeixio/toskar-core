package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"errors"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// OpenAI's embeddings endpoint answers with the installed embedding model,
// in OpenAI's shape, as floats or base64 (#1357).
func TestEmbeddings(t *testing.T) {
	h := testHandler(t)
	h.Embed = func(_ *http.Request, texts []string) ([][]float32, string, error) {
		out := make([][]float32, len(texts))
		for i := range texts {
			out[i] = []float32{float32(i), 0.5}
		}
		return out, "bge-small", nil
	}
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.HandleEmbeddings(rec, httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(body)))
		return rec
	}
	rec := post(`{"model":"text-embedding-3-small","input":["hello","world"]}`)
	var got struct {
		Object string `json:"object"`
		Model  string `json:"model"`
		Data   []struct {
			Object    string    `json:"object"`
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got.Object != "list" || got.Model != "bge-small" || len(got.Data) != 2 || got.Data[1].Index != 1 || got.Data[1].Embedding[0] != 1 || got.Data[0].Object != "embedding" || got.Usage.PromptTokens != 4 {
		t.Fatalf("embeddings = %+v", got)
	}

	rec = post(`{"input":"hello","encoding_format":"base64"}`)
	var b64 struct {
		Data []struct {
			Embedding string `json:"embedding"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &b64)
	raw, err := base64.StdEncoding.DecodeString(b64.Data[0].Embedding)
	if err != nil || len(raw) != 8 || math.Float32frombits(binary.LittleEndian.Uint32(raw[4:])) != 0.5 {
		t.Fatalf("base64: %s", rec.Body.String())
	}

	for body, want := range map[string]string{
		`{"input":[1,2,3]}`:                   "token arrays",
		`{"input":""}`:                        "input is required",
		`{"input":"a","dimensions":256}`:      "2 dimensions",
		`{"input":"a","encoding_format":"x"}`: "encoding_format",
	} {
		if rec := post(body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}

	h.Embed = func(*http.Request, []string) ([][]float32, string, error) {
		return nil, "", errors.New("no embedding model is installed: set one up in Settings › Features")
	}
	if rec := post(`{"input":"a"}`); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "Settings › Features") {
		t.Errorf("not installed: %d %s", rec.Code, rec.Body.String())
	}
}

func transcriptionRequest(t *testing.T, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if _, ok := fields["nofile"]; !ok {
		fw, _ := mw.CreateFormFile("file", "note.m4a")
		_, _ = fw.Write([]byte("audio"))
	}
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

// Transcriptions take OpenAI's multipart upload and answer in each of its
// formats (#1357).
func TestTranscriptions(t *testing.T) {
	h := testHandler(t)
	var gotName, gotLang string
	h.Transcribe = func(_ context.Context, name string, audio []byte, language string) (Transcription, error) {
		gotName, gotLang = name, language
		return Transcription{Text: " Hello there. ", Language: "en", Duration: 2.5,
			Segments: []TranscriptSegment{{Start: 0, End: 1.25, Text: " Hello"}, {Start: 1.25, End: 2.5, Text: " there."}}}, nil
	}
	for format, want := range map[string]string{
		"":     `{"text":"Hello there."}` + "\n",
		"text": "Hello there.\n",
		"srt":  "1\n00:00:00,000 --> 00:00:01,250\nHello\n\n2\n00:00:01,250 --> 00:00:02,500\nthere.\n\n",
		"vtt":  "WEBVTT\n\n00:00:00.000 --> 00:00:01.250\nHello\n\n00:00:01.250 --> 00:00:02.500\nthere.\n\n",
	} {
		rec := httptest.NewRecorder()
		h.HandleTranscriptions(rec, transcriptionRequest(t, map[string]string{"model": "whisper-1", "response_format": format, "language": "EN"}))
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%q: %d %q", format, rec.Code, rec.Body.String())
		}
	}
	if gotName != "note.m4a" || gotLang != "en" {
		t.Errorf("name %q language %q", gotName, gotLang)
	}
	rec := httptest.NewRecorder()
	h.HandleTranscriptions(rec, transcriptionRequest(t, map[string]string{"response_format": "verbose_json"}))
	if !strings.Contains(rec.Body.String(), `"duration":2.5`) || !strings.Contains(rec.Body.String(), `"language":"en"`) || !strings.Contains(rec.Body.String(), `"segments":[{`) {
		t.Errorf("verbose_json: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.HandleTranscriptions(rec, transcriptionRequest(t, map[string]string{"nofile": ""}))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "file is required") {
		t.Errorf("no file: %d %s", rec.Code, rec.Body.String())
	}
	h.Transcribe = func(context.Context, string, []byte, string) (Transcription, error) {
		return Transcription{}, contracts.NewError("SPEECH_UNAVAILABLE", nil, errors.New("speech isn't set up on this computer"))
	}
	rec = httptest.NewRecorder()
	h.HandleTranscriptions(rec, transcriptionRequest(t, nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"code":"SPEECH_UNAVAILABLE"`) {
		t.Errorf("unavailable: %d %s", rec.Code, rec.Body.String())
	}
}

// Speech reads the input a sentence at a time in Toskar's voice, as WAV or
// raw PCM; OpenAI's voice names mean the language's own voice (#1357).
func TestSpeech(t *testing.T) {
	h := testHandler(t)
	var sentences, voices []string
	h.Speak = func(_ context.Context, text, voice string) ([]int16, error) {
		sentences, voices = append(sentences, text), append(voices, voice)
		return []int16{1, -1, 2}, nil
	}
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.HandleSpeech(rec, httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(body)))
		return rec
	}
	rec := post(`{"model":"tts-1","input":"Hello there. How are you?","voice":"alloy"}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "audio/wav" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("RIFF")) {
		t.Fatalf("wav: %d %s %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Bytes()[:4])
	}
	if len(sentences) != 2 || voices[0] != "" {
		t.Fatalf("sentences %q voices %q", sentences, voices)
	}
	rec = post(`{"input":"Hi.","voice":"af_heart","response_format":"pcm"}`)
	if rec.Header().Get("Content-Type") != "audio/pcm" || rec.Body.Len() != 6 || voices[len(voices)-1] != "af_heart" {
		t.Fatalf("pcm: %s %d %q", rec.Header().Get("Content-Type"), rec.Body.Len(), voices)
	}
	if rec := post(`{"input":"` + strings.Repeat("a", maxSpeechRunes+1) + `"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("too long: %d", rec.Code)
	}
	if rec := post(`{"input":"hi","response_format":"ogg"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown format: %d", rec.Code)
	}
}

// A chat-only key reaches the embeddings and audio endpoints, which save
// nothing (#1357).
func TestChatKeyReachesEmbeddingsAndAudio(t *testing.T) {
	for _, route := range []string{"/v1/embeddings", "/v1/audio/transcriptions", "/v1/audio/speech"} {
		if !auth.ChatKeyMayReach(http.MethodPost, route) {
			t.Errorf("%s", route)
		}
	}
}
