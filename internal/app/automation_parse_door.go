package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/connectors"
	"github.com/yeixio/toskar-core/internal/locale"
)

// Reading a request for an automation that runs when something happens,
// such as "when someone rings the doorbell, greet them and tell me who is
// there" (#1341): it starts from the doorbell's button, looks at its
// camera, and says its answer at the door, instead of becoming an hourly
// schedule that looks at nothing.

// ringWordsRe finds a request about a doorbell ringing, in the App
// languages.
var ringWordsRe = regexp.MustCompile(`(?i)door ?bell|\brings?\b|\brang\b|\bringing\b|klingel|timbre|sonnette|campanello|campainha|インターホン|呼び鈴|チャイム|초인종|门铃|門鈴`)

// greetWordsRe finds a request to speak to the visitor.
var greetWordsRe = regexp.MustCompile(`(?i)greet|\bsay\b|speak|talk|answer|\bask\b|begrü|grüß|salud|saluer|salue|salut|saluta|cumpriment|挨拶|あいさつ|인사|问候|打招呼|問候`)

// talkWordsRe finds a request to find out who came, or to talk with them.
var talkWordsRe = regexp.MustCompile(`(?i)talk with|talk to|find out|\bask\b|who (is|it is|they are)|why|wer da|qui(é|e)n|qui est|chi è|quem|誰|누가|是谁|是誰`)

// connectedDevices are the connected services' devices, or none.
func (a *App) connectedDevices(ctx context.Context) []connectors.Device {
	if a.Connectors == nil {
		return nil
	}
	list, err := a.Connectors.Devices(ctx)
	if err != nil {
		return nil
	}
	return list
}

// ringDevice is the first doorbell button among devices.
func ringDevice(devices []connectors.Device) (connectors.Device, bool) {
	for _, d := range devices {
		if automations.Rang(&automations.Trigger{Kind: automations.TriggerDevice, Connector: d.Connector, Device: d.Device}) {
			return d, true
		}
	}
	return connectors.Device{}, false
}

// cameraFor is the camera beside a doorbell's button: a Reolink doorbell's
// own, on the same hub channel (#1333), then one of the same service, or
// the first camera.
func cameraFor(devices []connectors.Device, ring connectors.Device) (connectors.Device, bool) {
	if ring.Connector == automations.CameraReolink {
		_, ch, _ := strings.Cut(ring.Device, "@")
		own := connectors.ReolinkCamera
		if ch != "" {
			own += "@" + ch
		}
		for _, d := range devices {
			if d.Kind == connectors.KindCamera && d.Connector == ring.Connector && d.Device == own {
				return d, true
			}
		}
	}
	var first *connectors.Device
	for i, d := range devices {
		if d.Kind != connectors.KindCamera {
			continue
		}
		if d.Connector == ring.Connector {
			return d, true
		}
		if first == nil {
			first = &devices[i]
		}
	}
	if first != nil {
		return *first, true
	}
	return connectors.Device{}, false
}

// doorRequest reads a request about the doorbell ringing, when one is
// connected: it starts from the button, looks at the camera, and speaks
// at the door when the request asks to greet or answer the visitor. ok is
// false for any other request.
func doorRequest(text string, devices []connectors.Device, schedule automations.Schedule, lang string) (automations.ParsedRequest, bool) {
	if !ringWordsRe.MatchString(text) {
		return automations.ParsedRequest{}, false
	}
	ring, ok := ringDevice(devices)
	if !ok {
		return automations.ParsedRequest{}, false
	}
	read := automations.ParsedRequest{
		Name:         locale.T(lang, "automations:ideas.door.title", nil),
		Prompt:       strings.TrimSpace(text),
		Schedule:     schedule,
		Notification: automations.Notification{Mode: automations.NotifyAlways},
		Notes:        []string{},
		Trigger:      &automations.Trigger{Kind: automations.TriggerDevice, Connector: ring.Connector, Device: ring.Device},
	}
	if cam, ok := cameraFor(devices, ring); ok {
		read.Cameras = []automations.Camera{{Connector: cam.Connector, Device: cam.Device}}
		if cam.Connector == automations.SayReolink && greetWordsRe.MatchString(text) {
			read.Say = &automations.Say{Connector: cam.Connector, Device: cam.Device, Listen: talkWordsRe.MatchString(text)}
			if read.Say.Validate() != nil {
				read.Say = nil
			}
		}
	}
	return read, true
}

// devicesNote lists the devices a model may start an automation from or
// look at, for parseWithModel.
func devicesNote(devices []connectors.Device) string {
	if len(devices) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nConnected devices it can start from or look at (id: name, kind):\n")
	for _, d := range devices {
		fmt.Fprintf(&b, "- %s:%s: %s, %s\n", d.Connector, d.Device, d.Name, d.Kind)
	}
	b.WriteString(`When the request says it runs when something happens, such as the doorbell ringing or a door opening, put that device's id in "starts". Put cameras it should look at in "cameras", and set "speak_at_door" when it should greet or answer someone through a camera, and "talk_with_visitor" when it should find out who came and why.`)
	return b.String()
}

// deviceByID is the device an id from devicesNote names.
func deviceByID(devices []connectors.Device, id string) (connectors.Device, bool) {
	connector, device, ok := strings.Cut(strings.TrimSpace(id), ":")
	if !ok {
		return connectors.Device{}, false
	}
	for _, d := range devices {
		if d.Connector == connector && d.Device == device {
			return d, true
		}
	}
	return connectors.Device{}, false
}

// withDevices adds what starts the automation, its cameras, and speaking
// at the door from a model's reply, keeping only devices that exist.
func withDevices(read automations.ParsedRequest, obj map[string]any, devices []connectors.Device) automations.ParsedRequest {
	if id, _ := obj["starts"].(string); id != "" {
		if d, ok := deviceByID(devices, id); ok && d.Kind != connectors.KindCamera {
			read.Trigger = &automations.Trigger{Kind: automations.TriggerDevice, Connector: d.Connector, Device: d.Device}
		}
	}
	if ids, _ := obj["cameras"].([]any); len(ids) > 0 {
		for _, v := range ids {
			id, _ := v.(string)
			if d, ok := deviceByID(devices, id); ok && d.Kind == connectors.KindCamera && len(read.Cameras) < automations.MaxCameras {
				read.Cameras = append(read.Cameras, automations.Camera{Connector: d.Connector, Device: d.Device})
			}
		}
	}
	if speak, _ := obj["speak_at_door"].(bool); speak {
		for _, c := range read.Cameras {
			if c.Connector == automations.SayReolink {
				say := &automations.Say{Connector: c.Connector, Device: c.Device}
				say.Listen, _ = obj["talk_with_visitor"].(bool)
				if say.Validate() == nil {
					read.Say = say
				}
				break
			}
		}
	}
	return read
}

// readableName is a model's name as people write one: "Doorbell visitor
// check", not doorbell_visitor_check (#1341).
func readableName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsRune(name, ' ') || !strings.ContainsAny(name, "_-") {
		return name
	}
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	out := strings.ToLower(strings.Join(words, " "))
	r := []rune(out)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}
