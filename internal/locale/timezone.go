package locale

import (
	"context"
	"os"
	"strings"
	"time"
)

type timeZoneKey struct{}

// WithTimeZone carries the person's time zone for a request, as an IANA
// name such as America/Juneau from their browser. An empty or unknown name
// leaves this computer's own.
func WithTimeZone(ctx context.Context, name string) context.Context {
	if name == "" {
		return ctx
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, timeZoneKey{}, loc)
}

// TimeZone is the person's time zone for a request, or this computer's.
func TimeZone(ctx context.Context) *time.Location {
	if ctx != nil {
		if loc, ok := ctx.Value(timeZoneKey{}).(*time.Location); ok {
			return loc
		}
	}
	return time.Local
}

// SystemZone is this computer's time zone as an IANA name, such as
// America/Juneau, which every client understands. Go names it "Local" when
// TZ isn't set, which browsers refuse, so this reads TZ, then the
// /etc/localtime link that macOS and Linux keep; else UTC.
func SystemZone() string {
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" && tz != "Local" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(target, "zoneinfo/"); ok {
			if _, err := time.LoadLocation(name); err == nil {
				return name
			}
		}
	}
	if name := time.Local.String(); name != "" && name != "Local" {
		return name
	}
	return "UTC"
}

// ZoneName is name, or this computer's IANA zone for "Local" or none.
func ZoneName(name string) string {
	if name == "" || name == "Local" {
		return SystemZone()
	}
	return name
}
