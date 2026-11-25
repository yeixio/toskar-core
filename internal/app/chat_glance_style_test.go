package app

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/turnopts"
)

// An answer asked on a watch or in a car is the whole answer, summed up when
// long: before, the app cut it to its first paragraph and said the rest was
// in Toskar on the phone.
func TestTurnInstructionsForWatchAndCar(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	for client, want := range map[string]string{"watch": watchStyle, "car": carStyle, "phone": "", "": ""} {
		ctx := runlog.WithOrigin(context.Background(), runlog.Origin{Client: client})
		got := (&chatExecEnv{app: a, ctx: ctx}).TurnInstructions(ctx, "What are the store's hours?")
		for _, style := range []string{watchStyle, carStyle} {
			if strings.Contains(got, style) != (style == want) {
				t.Errorf("client %q: style %q present %v", client, style[:30], !(style == want))
			}
		}
	}
	// Live voice mode keeps its own style.
	ctx := turnopts.WithSpoken(runlog.WithOrigin(context.Background(), runlog.Origin{Client: "car"}))
	got := (&chatExecEnv{app: a, ctx: ctx}).TurnInstructions(ctx, "What are the store's hours?")
	if !strings.Contains(got, spokenStyle) || strings.Contains(got, carStyle) {
		t.Errorf("a spoken turn from the car:\n%s", got)
	}
}
