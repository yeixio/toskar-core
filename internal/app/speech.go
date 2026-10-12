package app

import (
	"context"
	"errors"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Speaking a message instead of typing it (yeixio/toskar-apps#155): a
// device records, attaches the recording, and asks for its text, which the
// person checks before sending. The text comes from speech.transcribe
// (Whisper), run on this computer or a paired one that has it.

const noTranscription = "speech can't be turned into text on any of your computers yet; set up transcription on the Tools page"

var errNoTranscription = contracts.NewError("SPEECH_UNAVAILABLE", map[string]any{"detail": noTranscription}, errors.New(noTranscription))

// transcribeRecording answers an attached recording's text, its language,
// and its length.
func (a *App) transcribeRecording(ctx context.Context, artifactID, language string) (map[string]any, error) {
	if a.Tools == nil {
		return nil, errNoTranscription
	}
	t, err := a.Tools.Get("speech.transcribe")
	if err != nil {
		return nil, errNoTranscription
	}
	if av, ok := t.(interface{ Available() (bool, string) }); ok {
		if ready, why := av.Available(); !ready {
			if why == "" {
				return nil, errNoTranscription
			}
			return nil, contracts.NewError("SPEECH_UNAVAILABLE", map[string]any{"detail": why}, errors.New(why))
		}
	}
	args := map[string]any{"file": artifactID}
	if lang := strings.ToLower(strings.TrimSpace(language)); lang != "" {
		args["language"] = lang
	}
	out, err := t.Execute(ctx, args)
	if err != nil {
		return nil, contracts.NewError("SPEECH_FAILED", map[string]any{"detail": err.Error()}, err)
	}
	return map[string]any{"text": strings.TrimSpace(asString(out["text"])), "language": out["language"], "duration_seconds": out["duration_seconds"]}, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
