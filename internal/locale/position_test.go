package locale

import (
	"context"
	"math"
	"testing"
)

// A device's position rides with one request, only when it's a real one
// (#1310).
func TestPosition(t *testing.T) {
	juneau := &Position{Latitude: 58.30194, Longitude: -134.41972, AccuracyM: 35}
	ctx := WithPosition(context.Background(), juneau)
	p, ok := PositionFrom(ctx)
	if !ok || p.String() != "58.3019,-134.4197" {
		t.Fatalf("position %v %v", p, ok)
	}
	for _, bad := range []*Position{nil, {}, {Latitude: 91, Longitude: 1}, {Latitude: 1, Longitude: 181}, {Latitude: 1, Longitude: 1, AccuracyM: 60_000}, {Latitude: math.NaN(), Longitude: 1}} {
		if _, ok := PositionFrom(WithPosition(context.Background(), bad)); ok {
			t.Errorf("%v was taken", bad)
		}
	}
	var none context.Context
	if _, ok := PositionFrom(none); ok {
		t.Error("nil context")
	}
}
