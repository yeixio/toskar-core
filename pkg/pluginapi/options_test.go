package pluginapi

import (
	"context"
	"testing"
)

func TestGenerateOptions(t *testing.T) {
	if o := GenerateOptionsFrom(context.Background()); o != (GenerateOptions{}) {
		t.Fatalf("none set: %+v", o)
	}
	ctx := WithGenerateOptions(context.Background(), GenerateOptions{Temperature: 0.4, MaxTokens: 512})
	if o := GenerateOptionsFrom(ctx); o.Temperature != 0.4 || o.MaxTokens != 512 {
		t.Fatalf("set: %+v", o)
	}
}

func TestSamplingCheck(t *testing.T) {
	neg := int64(-1)
	for _, bad := range []Sampling{{TopP: 1.1}, {TopP: -0.1}, {TopK: -1}, {MinP: 1.5}, {Seed: &neg}} {
		if bad.Check() == nil {
			t.Errorf("%+v passed", bad)
		}
	}
	zero := int64(0)
	if err := (Sampling{TopP: 1, TopK: 0, MinP: 0, Seed: &zero}).Check(); err != nil {
		t.Errorf("valid settings refused: %v", err)
	}
}
