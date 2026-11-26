package locale

import (
	"context"
	"math"
	"strconv"
)

// Position is where the person's device is, from the device itself, when
// they turned on Use this device's location (#1310). A client sends it with
// a message; it lives only for that request and is never stored.
type Position struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	// AccuracyM is how far off it may be, in meters, as the device says.
	AccuracyM float64 `json:"accuracy_m,omitempty"`
}

// maxAccuracyM is the vaguest position worth using: a town's width.
const maxAccuracyM = 50_000

// Valid reports a position on Earth that's precise enough to use. 0,0,
// what a device sends when it has nothing, isn't one.
func (p Position) Valid() bool {
	for _, v := range []float64{p.Latitude, p.Longitude, p.AccuracyM} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	if p.Latitude < -90 || p.Latitude > 90 || p.Longitude < -180 || p.Longitude > 180 {
		return false
	}
	if p.Latitude == 0 && p.Longitude == 0 {
		return false
	}
	return p.AccuracyM >= 0 && p.AccuracyM <= maxAccuracyM
}

// String is the position as "lat,lon" to four places, about 11 m, which
// the places tools and weather look-ups take as a place.
func (p Position) String() string {
	return strconv.FormatFloat(p.Latitude, 'f', 4, 64) + "," + strconv.FormatFloat(p.Longitude, 'f', 4, 64)
}

type positionKey struct{}

// WithPosition carries the device's position for a request; nil or an
// invalid one leaves it unknown.
func WithPosition(ctx context.Context, p *Position) context.Context {
	if p == nil || !p.Valid() {
		return ctx
	}
	return context.WithValue(ctx, positionKey{}, *p)
}

// PositionFrom is the device's position for a request, when it sent one.
func PositionFrom(ctx context.Context) (Position, bool) {
	if ctx == nil {
		return Position{}, false
	}
	p, ok := ctx.Value(positionKey{}).(Position)
	return p, ok
}
