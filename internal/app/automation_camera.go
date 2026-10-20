package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/egress"
)

// lookAtCamera takes the picture an automation's camera shows as its run
// starts, such as the front door through Home Assistant (#667), and keeps it
// like a photo attached in chat, so a model that can see is shown it.
func (a *App) lookAtCamera(ctx context.Context, automation automations.Automation) (artifacts.Artifact, error) {
	cam := automation.Camera
	if a.Connectors == nil || a.Artifacts == nil {
		return artifacts.Artifact{}, fmt.Errorf("cameras aren't available on this computer")
	}
	data, mime, err := a.Connectors.CameraPicture(ctx, cam.Connector, cam.Entity)
	if err != nil {
		return artifacts.Artifact{}, fmt.Errorf("couldn't look at %s: %w", cam.Entity, err)
	}
	if a.Egress != nil {
		a.Egress.Add(ctx, egress.Connector, a.Connectors.ServiceName(cam.Connector), "camera picture: "+cam.Entity)
	}
	ext := ".jpg"
	if mime == "image/png" {
		ext = ".png"
	}
	name := strings.TrimPrefix(cam.Entity, "camera.") + " " + time.Now().Format("2006-01-02 15.04.05") + ext
	return a.Artifacts.Save(ctx, artifacts.Input{
		Name: name, Producer: "automation", SourceTask: "automation:" + automation.ID, Data: data,
		Details: map[string]any{"camera": cam.Entity},
	})
}

// cameraNote tells the run about the picture it's shown.
func cameraNote(cam *automations.Camera) string {
	if cam == nil {
		return ""
	}
	return "A picture from the camera " + cam.Entity + ", taken as this run started, is shown with this message. Look at it to answer."
}
