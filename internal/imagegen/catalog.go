// Package imagegen makes and edits images on this computer (Gungnir §17)
// with stable-diffusion.cpp and GGUF models. Setup downloads the program and
// a model the first time; after that, images are made offline and nothing
// leaves the computer.
package imagegen

// File is one download a model needs.
type File struct {
	// Role is how sd-cli takes it: diffusion, vae, llm (the text encoder),
	// llm_vision (the text encoder's image encoder, for an editor that
	// reads the picture it changes), or high_noise (a two-stage video
	// model's first stage).
	Role   string `json:"role"`
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Size   int64  `json:"size_bytes"`
	SHA256 string `json:"sha256"`
	// Source, when set, is the file's full address, pinned to a commit,
	// for a file that is not on Hugging Face.
	Source string `json:"source,omitempty"`
	// Revision pins a Hugging Face file to a commit, so it never changes
	// under its checksum.
	Revision string `json:"revision,omitempty"`
	// Gated is a file behind its model's license on Hugging Face: it's
	// downloaded with the person's own token, from the vendor (#551).
	Gated bool `json:"gated,omitempty"`
}

// URL is where the file is downloaded from, pinned to its revision.
func (f File) URL() string {
	if f.Source != "" {
		return f.Source
	}
	return "https://huggingface.co/" + f.Repo + "/resolve/" + f.Revision + "/" + f.Path
}

// Model is an image model and everything it needs.
type Model struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	License     string `json:"license"`
	// MemoryBytes is the memory it is comfortable with; smaller computers
	// get the smaller model recommended.
	MemoryBytes int64 `json:"memory_bytes"`
	// GraphicsBytes is the graphics memory it is comfortable with on a
	// computer with its own graphics card; with less, parts of it run from
	// system memory, more slowly (#550).
	GraphicsBytes int64 `json:"graphics_bytes,omitempty"`
	// Stages are its measured parts, from which GraphicsFor works out the
	// graphics memory a request of a given size and length needs (#550);
	// with them, GraphicsBytes is what the default request needs.
	Stages []Stage `json:"-"`
	Files  []File  `json:"files"`
	// Edits says whether it can change an image from an instruction, or,
	// for a video model, animate one.
	Edits bool `json:"edits"`
	// Kind is image or video.
	Kind string `json:"kind"`
	// Untested is a model that hasn't yet passed the media quality set on
	// real hardware (#548, #553): it can be chosen, but it's never
	// recommended or picked for the person.
	Untested bool `json:"untested,omitempty"`
	// EditOnly is a model that changes pictures but doesn't make new ones:
	// edits prefer it, and new pictures never use it.
	EditOnly bool `json:"edit_only,omitempty"`
	// ModelArgs is passed to sd-cli as --model-args, for a model that needs
	// one to work as intended.
	ModelArgs string `json:"-"`
	// NeedsLicense is a model Toskar can't give out: the person accepts
	// its license on Hugging Face, adds their own token, and confirms
	// their license covers their use before it's set up (#551).
	// LicenseURL is the license, and LicenseAllows says what it allows.
	NeedsLicense  bool   `json:"needs_license,omitempty"`
	LicenseURL    string `json:"license_url,omitempty"`
	LicenseAllows string `json:"license_allows,omitempty"`
	// Tier is the quality a model gives among those of its kind: fast,
	// balanced, or best (#549). A request for a quality uses the installed
	// model of that tier.
	Tier string `json:"tier,omitempty"`
	// Steps and CFG are its sampling defaults; FlowShift, Frames, and FPS
	// are a video model's.
	Steps     int     `json:"-"`
	CFG       float64 `json:"-"`
	FlowShift float64 `json:"-"`
	Frames    int     `json:"-"`
	FPS       int     `json:"-"`
	// HighNoiseSteps and HighNoiseCFG are a two-stage video model's first
	// stage, which lays out the clip before the second adds detail.
	HighNoiseSteps int     `json:"-"`
	HighNoiseCFG   float64 `json:"-"`
}

// SizeBytes is the total download.
func (m Model) SizeBytes() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// Catalog lists the picture models setup offers, from media.json (#551).
// FLUX.2 [klein] 4B, the default, makes and edits images in four steps; it
// and its parts are Apache 2.0 and need no account to download.
func Catalog() []Model { return toModels(manifest().Images) }

// VideoCatalog lists the clip models setup offers, from media.json. Wan
// 2.2 TI2V 5B, the default, makes a short clip from a description or
// brings an image to life.
func VideoCatalog() []Model { return toModels(manifest().Video) }

// Lookup returns an image model by id.
func Lookup(id string) (Model, bool) { return lookupIn(Catalog(), id) }

func lookupIn(list []Model, id string) (Model, bool) {
	for _, m := range list {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// Recommend picks the image model for a computer with this much memory:
// the largest it is comfortable with, else the smallest.
func Recommend(memoryBytes int64) Model { return recommendIn(Catalog(), memoryBytes) }

// recommendIn picks from models that do everything the tools offer: make
// pictures and edit them, or make clips and bring pictures to life. A model
// that only makes, or only edits, is set up by choice beside it (#548).
func recommendIn(list []Model, memoryBytes int64) Model {
	best := list[0]
	for _, m := range list {
		if !m.Untested && !m.NeedsLicense && m.Edits && !m.EditOnly && m.MemoryBytes <= memoryBytes && m.MemoryBytes > best.MemoryBytes {
			best = m
		}
	}
	return best
}
