package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/pyenv"
)

// What Toskar downloads and swaps in by itself is recorded in What left
// this computer, and the swaps a person didn't start are told in the app
// (#1209): a faster llama.cpp build at start, and stable-diffusion.cpp's
// new release after a Toskar update. Python and its packages are recorded;
// the person started the feature that needs them, and its setup shows
// each step.

// runtimeUpgraded records and tells that runtime id's better build, such
// as llama.cpp's GPU build, replaced the one installed.
func (a *App) runtimeUpgraded(ctx context.Context, id string) {
	name := id
	if id == "llamacpp" {
		name = "llama.cpp"
	}
	a.recordDownload(ctx, "github.com", name+": a faster build for this computer's graphics, replacing the one installed")
	a.tellDownloaded(ctx, "runtime-build:"+id, "runtimeUpgraded", "runtimeUpgradedBody", map[string]any{"runtime": name}, "/settings/privacy")
}

// imagesUpdated records and tells that stable-diffusion.cpp's release for
// this Toskar replaced the earlier one.
func (a *App) imagesUpdated(ctx context.Context, archive string) {
	a.recordDownload(ctx, "github.com", "stable-diffusion.cpp "+imagegen.Release+" ("+archive+"), replacing the earlier release")
	a.tellDownloaded(ctx, "sdcpp:"+imagegen.Release, "imagesUpdated", "imagesUpdatedBody", map[string]any{"release": imagegen.Release}, "/settings/privacy")
}

// pythonInstalled records Python and spec's packages, set up for a
// feature's first use.
func (a *App) pythonInstalled(ctx context.Context, spec pyenv.Spec) {
	detail := fmt.Sprintf("Python %s and packages for %s: %s", pyenv.PythonVersion, spec.Name, strings.Join(spec.Requirements, ", "))
	a.recordDownload(ctx, "github.com, pypi.org", detail)
}

func (a *App) recordDownload(ctx context.Context, host, detail string) {
	if a.Egress != nil {
		a.Egress.Add(context.WithoutCancel(ctx), egress.Download, host, detail)
	}
}

func (a *App) tellDownloaded(ctx context.Context, key, title, body string, params map[string]any, link string) {
	if a.Notifications == nil {
		return
	}
	_, err := a.Notifications.Notify(context.WithoutCancel(ctx), gjallarhorn.Request{
		SourceType: "system",
		SourceID:   key,
		Category:   gjallarhorn.CategorySystem,
		Severity:   gjallarhorn.SeverityInfo,
		Message:    notice(title, params, body, params),
		Link:       link,
		DedupeKey:  "downloaded:" + key,
	})
	if err != nil && a.Logger != nil {
		a.Logger.Warn("couldn't tell about a download", "what", key, "error", err)
	}
}
