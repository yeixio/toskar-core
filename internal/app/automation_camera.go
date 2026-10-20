package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/egress"
)

// lookAtCameras takes the pictures an automation's cameras show as its run
// starts (#667), through the services they're connected with, and keeps
// them like photos attached in chat, so a model that can see is shown them.
func (a *App) lookAtCameras(ctx context.Context, automation automations.Automation) ([]artifacts.Artifact, error) {
	if a.Connectors == nil || a.Artifacts == nil {
		return nil, fmt.Errorf("cameras aren't available on this computer")
	}
	// Only an Admin's automation looks: one made by someone who is no longer
	// an Admin stops (#667).
	if a.People != nil && automation.PersonID != "" {
		if p, err := a.People.Get(ctx, automation.PersonID); err != nil || !p.Role.AtLeast(auth.RoleAdmin) {
			return nil, fmt.Errorf("looking at cameras needs an Admin's automation")
		}
	}
	var out []artifacts.Artifact
	for _, cam := range automation.Cameras {
		data, mime, err := a.Connectors.CameraPicture(ctx, cam.Connector, cam.Device)
		if err != nil {
			return nil, fmt.Errorf("couldn't look at %s: %w", cam.Device, err)
		}
		if a.Egress != nil {
			a.Egress.Add(ctx, egress.Connector, a.Connectors.ServiceName(cam.Connector), "camera picture: "+cam.Device)
		}
		ext := ".jpg"
		if mime == "image/png" {
			ext = ".png"
		}
		name := cameraName(cam.Device) + " " + time.Now().Format("2006-01-02 15.04.05") + ext
		art, err := a.Artifacts.Save(ctx, artifacts.Input{
			Name: name, Producer: "automation", SourceTask: "automation:" + automation.ID, Data: data,
			Details: map[string]any{"camera": cam.Device, "connector": cam.Connector},
		})
		if err != nil {
			return nil, err
		}
		out = append(out, art)
	}
	return out, nil
}

// cameraName is a camera's name for its picture's file: Home Assistant's
// without its domain.
func cameraName(device string) string {
	return strings.TrimPrefix(device, "camera.")
}

// camerasNote tells the run about the pictures it's shown. It names each
// camera as a person would, not by its service's ID, and says the picture,
// not the camera's status, is what's there: a small model given
// camera.front_door asked Home Assistant for it and answered "idle".
func camerasNote(cams []automations.Camera) string {
	if len(cams) == 0 {
		return ""
	}
	names := make([]string, len(cams))
	for i, c := range cams {
		names[i] = spokenCamera(c.Device)
	}
	shown := "A picture from the " + names[0] + " camera, taken as this run started, is shown with this message."
	if len(names) > 1 {
		shown = "Pictures from the " + strings.Join(names, ", ") + " cameras, in that order, taken as this run started, are shown with this message."
	}
	return shown + " They are what the cameras see now: answer from them. A camera's status, such as idle or recording, says nothing about what is in front of it."
}

// spokenCamera is a camera's name as a person says it: camera.front_door_fluent
// is "front door", without its domain and a stream name such as fluent.
func spokenCamera(device string) string {
	name := strings.TrimPrefix(device, "camera.")
	for _, stream := range []string{"_fluent", "_clear", "_sub", "_main", "_snapshots", "_high", "_low"} {
		name = strings.TrimSuffix(name, stream)
	}
	return strings.ReplaceAll(name, "_", " ")
}
