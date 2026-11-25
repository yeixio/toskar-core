// Package features is one-step setup's answer (#1355): each feature, and
// what it still needs on this computer.
package features

import (
	"errors"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ErrUnknown is a feature id the check doesn't know.
var ErrUnknown = contracts.NewError("FEATURE_UNKNOWN", nil, errors.New("there is no such feature"))

// Feature is one feature and whether it works here now.
type Feature struct {
	ID    string `json:"id"`
	Ready bool   `json:"ready"`
	Needs []Need `json:"needs"`
	// SetupBytes is what setting up the missing needs downloads.
	SetupBytes int64 `json:"setup_bytes"`
	// Installing is true while one of its needs is being set up.
	Installing bool `json:"installing"`
}

// Need is one thing a feature needs: chat, tools, vision,
// embedding, voice, images, video, trainer, or camera.
type Need struct {
	Need  string `json:"need"`
	Ready bool   `json:"ready"`
	// Install says how the offer sets it up: model, voice, images, or
	// video; empty when nothing here can.
	Install string `json:"install,omitempty"`
	// Option is what it installs: a model's id, or a picture or clip
	// model's; Name is its name, SizeBytes what it downloads.
	Option    string `json:"option,omitempty"`
	Name      string `json:"name,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	// NodeID and NodeName are a paired computer it is set up on.
	NodeID   string `json:"node_id,omitempty"`
	NodeName string `json:"node_name,omitempty"`
	// Installing is true while it is being set up, with Percent done.
	Installing bool `json:"installing,omitempty"`
	Percent    int  `json:"percent,omitempty"`
	// Detail is why it can't be set up here, when this computer says.
	Detail string `json:"detail,omitempty"`
}
