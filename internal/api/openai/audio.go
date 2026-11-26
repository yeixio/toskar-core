package openai

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/voice"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// OpenAI's embeddings and audio endpoints (#1357), answered by what Toskar
// already runs: the installed embedding model, Whisper, and the voices
// live voice speaks with. Apps that use Toskar as their OpenAI server
// (Open WebUI, n8n, note apps) need them for their own search, dictation
// and read-aloud, so nobody has to run a second server beside it.

// Transcription is what a recording says, for /v1/audio/transcriptions.
type Transcription struct {
	Text     string
	Language string
	Duration float64
	Segments []TranscriptSegment
}

// TranscriptSegment is a stretch of a recording and what it says.
type TranscriptSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Transcribe turns a recording, named for its format, into text; language
// is a hint such as en, or empty to detect it.
type Transcribe func(ctx context.Context, name string, audio []byte, language string) (Transcription, error)

// Speak says a sentence or two in voice (empty for the language's own) and
// returns its samples at voice.SampleRate.
type Speak func(ctx context.Context, text, voice string) ([]int16, error)

const (
	// maxAudioUpload is OpenAI's own limit on a transcription's file.
	maxAudioUpload = 25 << 20
	// maxSpeechRunes is OpenAI's limit on what /v1/audio/speech reads.
	maxSpeechRunes = 4096
	// maxEmbedInputs is OpenAI's limit on one request's inputs.
	maxEmbedInputs = 2048
)

// openAIVoices are OpenAI's voice names. Apps send one of them by default;
// Toskar answers in the language's own voice instead.
var openAIVoices = map[string]bool{
	"alloy": true, "ash": true, "ballad": true, "coral": true, "echo": true, "fable": true, "nova": true,
	"onyx": true, "sage": true, "shimmer": true, "verse": true, "marin": true, "cedar": true,
}

// HandleEmbeddings serves POST /v1/embeddings with the installed embedding
// model, whatever model the request names.
func (h *Handler) HandleEmbeddings(w http.ResponseWriter, r *http.Request) {
	_, r, err := h.permissionsFor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
		return
	}
	var req struct {
		Model          string          `json:"model"`
		Input          json.RawMessage `json:"input"`
		EncodingFormat string          `json:"encoding_format"`
		Dimensions     int             `json:"dimensions"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "The request isn't JSON.")
		return
	}
	texts, err := embedInputs(req.Input)
	if err != nil {
		writeErrorParam(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), "input")
		return
	}
	if req.EncodingFormat != "" && req.EncodingFormat != "float" && req.EncodingFormat != "base64" {
		writeErrorParam(w, http.StatusBadRequest, "BAD_REQUEST", "encoding_format must be float or base64", "encoding_format")
		return
	}
	if h.Embed == nil {
		writeError(w, http.StatusNotImplemented, "EMBEDDINGS_UNAVAILABLE", "Embeddings aren't available on this computer.")
		return
	}
	vectors, model, err := h.Embed(r, texts)
	if err != nil {
		writeCoded(w, http.StatusServiceUnavailable, "EMBEDDINGS_UNAVAILABLE", err)
		return
	}
	if req.Dimensions > 0 && len(vectors) > 0 && req.Dimensions != len(vectors[0]) {
		writeErrorParam(w, http.StatusBadRequest, "BAD_REQUEST",
			fmt.Sprintf("%s makes vectors of %d dimensions; leave dimensions out, or ask for %d", model, len(vectors[0]), len(vectors[0])), "dimensions")
		return
	}
	data := make([]map[string]any, len(vectors))
	tokens := 0
	for i, v := range vectors {
		var embedding any = v
		if req.EncodingFormat == "base64" {
			embedding = float32Base64(v)
		}
		data[i] = map[string]any{"object": "embedding", "index": i, "embedding": embedding}
	}
	for _, t := range texts {
		tokens += estimateTokens(t)
	}
	writeJSON(w, map[string]any{
		"object": "list",
		"data":   data,
		"model":  model,
		"usage":  map[string]int{"prompt_tokens": tokens, "total_tokens": tokens},
	})
}

// embedInputs reads input as one string or a list of them. Token arrays,
// which OpenAI also takes, mean another model's tokenizer: refused.
func embedInputs(raw json.RawMessage) ([]string, error) {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		if strings.TrimSpace(one) == "" {
			return nil, errors.New("input is required")
		}
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, errors.New("input must be a string or a list of strings; token arrays aren't taken")
	}
	if len(many) == 0 {
		return nil, errors.New("input is required")
	}
	if len(many) > maxEmbedInputs {
		return nil, fmt.Errorf("input has more than %d strings", maxEmbedInputs)
	}
	for i, s := range many {
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("input %d is empty", i)
		}
	}
	return many, nil
}

// float32Base64 is a vector as OpenAI's base64 encoding: little-endian
// float32s.
func float32Base64(v []float32) string {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// estimateTokens is about how many tokens text is, for usage: four
// characters each, as OpenAI's own rule of thumb.
func estimateTokens(text string) int {
	n := (utf8.RuneCountInString(text) + 3) / 4
	if n == 0 {
		n = 1
	}
	return n
}

// HandleTranscriptions serves POST /v1/audio/transcriptions (multipart:
// file, model, language, response_format) with Whisper, on this computer.
func (h *Handler) HandleTranscriptions(w http.ResponseWriter, r *http.Request) {
	_, r, err := h.permissionsFor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAudioUpload+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Send the recording as multipart/form-data, in a field named file, of at most 25 MB.")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErrorParam(w, http.StatusBadRequest, "BAD_REQUEST", "file is required", "file")
		return
	}
	defer file.Close()
	audio, err := io.ReadAll(io.LimitReader(file, maxAudioUpload+1))
	if err != nil || len(audio) == 0 {
		writeErrorParam(w, http.StatusBadRequest, "BAD_REQUEST", "file is empty", "file")
		return
	}
	if len(audio) > maxAudioUpload {
		writeErrorParam(w, http.StatusRequestEntityTooLarge, "BAD_REQUEST", "file is larger than 25 MB", "file")
		return
	}
	format := r.FormValue("response_format")
	switch format {
	case "", "json", "text", "verbose_json", "srt", "vtt":
	default:
		writeErrorParam(w, http.StatusBadRequest, "BAD_REQUEST", "response_format must be json, text, verbose_json, srt or vtt", "response_format")
		return
	}
	if h.Transcribe == nil {
		writeError(w, http.StatusNotImplemented, "SPEECH_UNAVAILABLE", "Transcription isn't available on this computer.")
		return
	}
	name := header.Filename
	if filepath.Ext(name) == "" {
		name += ".wav"
	}
	language := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	t, err := h.Transcribe(r.Context(), name, audio, language)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeCoded(w, http.StatusServiceUnavailable, "SPEECH_UNAVAILABLE", err)
		return
	}
	text := strings.TrimSpace(t.Text)
	switch format {
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, text+"\n")
	case "srt", "vtt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, subtitles(t, format == "vtt"))
	case "verbose_json":
		segments := make([]map[string]any, len(t.Segments))
		for i, s := range t.Segments {
			segments[i] = map[string]any{"id": i, "start": s.Start, "end": s.End, "text": s.Text}
		}
		writeJSON(w, map[string]any{"task": "transcribe", "language": t.Language, "duration": t.Duration, "text": text, "segments": segments})
	default:
		writeJSON(w, map[string]any{"text": text})
	}
}

// subtitles is a transcript as SubRip or WebVTT, one cue per segment.
func subtitles(t Transcription, vtt bool) string {
	segments := t.Segments
	if len(segments) == 0 && strings.TrimSpace(t.Text) != "" {
		segments = []TranscriptSegment{{End: t.Duration, Text: t.Text}}
	}
	var b strings.Builder
	if vtt {
		b.WriteString("WEBVTT\n\n")
	}
	for i, s := range segments {
		if !vtt {
			b.WriteString(strconv.Itoa(i+1) + "\n")
		}
		b.WriteString(cueTime(s.Start, vtt) + " --> " + cueTime(s.End, vtt) + "\n")
		b.WriteString(strings.TrimSpace(s.Text) + "\n\n")
	}
	return b.String()
}

func cueTime(seconds float64, vtt bool) string {
	ms := int64(math.Round(seconds * 1000))
	sep := ","
	if vtt {
		sep = "."
	}
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", ms/3600000, ms/60000%60, ms/1000%60, sep, ms%1000)
}

// HandleSpeech serves POST /v1/audio/speech: input read aloud in the
// voice live voice speaks with, as WAV, or as raw 24 kHz 16-bit PCM for
// response_format pcm. Toskar doesn't encode MP3, Opus, AAC or FLAC: a
// request for one gets WAV, which every player takes, with its type said.
func (h *Handler) HandleSpeech(w http.ResponseWriter, r *http.Request) {
	_, r, err := h.permissionsFor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
		return
	}
	var req struct {
		Model          string  `json:"model"`
		Input          string  `json:"input"`
		Voice          string  `json:"voice"`
		ResponseFormat string  `json:"response_format"`
		Speed          float64 `json:"speed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "The request isn't JSON.")
		return
	}
	text := strings.TrimSpace(req.Input)
	if text == "" {
		writeErrorParam(w, http.StatusBadRequest, "BAD_REQUEST", "input is required", "input")
		return
	}
	if utf8.RuneCountInString(text) > maxSpeechRunes {
		writeErrorParam(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("input is longer than %d characters", maxSpeechRunes), "input")
		return
	}
	switch req.ResponseFormat {
	case "", "wav", "pcm", "mp3", "opus", "aac", "flac":
	default:
		writeErrorParam(w, http.StatusBadRequest, "BAD_REQUEST", "response_format must be wav or pcm (mp3, opus, aac and flac answer as WAV)", "response_format")
		return
	}
	if h.Speak == nil {
		writeError(w, http.StatusNotImplemented, "SPEECH_UNAVAILABLE", "Speech isn't available on this computer.")
		return
	}
	name := strings.TrimSpace(req.Voice)
	if openAIVoices[strings.ToLower(name)] {
		name = ""
	}
	var samples []int16
	for _, sentence := range voice.Sentences(text) {
		s, err := h.Speak(r.Context(), sentence, name)
		if err != nil {
			if r.Context().Err() != nil {
				return
			}
			writeCoded(w, http.StatusServiceUnavailable, "SPEECH_UNAVAILABLE", err)
			return
		}
		samples = append(samples, s...)
	}
	var body []byte
	if req.ResponseFormat == "pcm" {
		body = make([]byte, 2*len(samples))
		for i, s := range samples {
			binary.LittleEndian.PutUint16(body[2*i:], uint16(s))
		}
		w.Header().Set("Content-Type", "audio/pcm")
	} else {
		body = voice.WAV(samples, voice.SampleRate)
		w.Header().Set("Content-Type", "audio/wav")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// writeCoded answers an error with its own code when it has one, such as
// SPEECH_UNAVAILABLE with how to set speech up, or code.
func writeCoded(w http.ResponseWriter, status int, code string, err error) {
	if c, _ := contracts.ErrorCode(err); c != "" {
		code = c
	}
	writeError(w, status, code, err.Error())
}
