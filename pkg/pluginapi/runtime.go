package pluginapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// RuntimeDetection reports what a runtime found on the host.
type RuntimeDetection struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
	Message   string `json:"message,omitempty"`
	// Backends are what the installed build can run on, such as ["cpu",
	// "vulkan"]; a CPU-only build is just ["cpu"] (#317).
	Backends []string `json:"backends,omitempty"`
}

// InstallOptions configures runtime installation.
type InstallOptions struct {
	Force bool `json:"force,omitempty"`
}

// RuntimeCapabilities describes what the runtime can do.
type RuntimeCapabilities struct {
	Backends          []string `json:"backends"`
	SupportsStreaming bool     `json:"supports_streaming"`
	SupportsTools     bool     `json:"supports_tools"`
	SupportsGPU       bool     `json:"supports_gpu"`
}

// ModelStartConfig starts a model under a runtime.
// ErrLoadFailed marks a start where the runtime ran but the model did not
// load, such as running out of memory: a failure of the model on this
// computer, not of the setup. Match it with errors.Is.
var ErrLoadFailed = errors.New("the model did not load")

// LoadFailed marks err as ErrLoadFailed, keeping its message.
func LoadFailed(err error) error {
	if err == nil {
		return nil
	}
	return loadFailed{err}
}

type loadFailed struct{ err error }

func (e loadFailed) Error() string   { return e.err.Error() }
func (e loadFailed) Unwrap() []error { return []error{e.err, ErrLoadFailed} }

type ModelStartConfig struct {
	ModelID   string `json:"model_id"`
	ModelPath string `json:"model_path"`
	Context   int    `json:"context,omitempty"`
	GPULayers int    `json:"gpu_layers,omitempty"`
	Port      int    `json:"port,omitempty"`
	// Adapters are LoRA adapters loaded next to the base weights. They are
	// inactive unless a request names one.
	Adapters []Adapter `json:"adapters,omitempty"`
	// Mode is ModeEmbedding or ModeReranking for a supporting model, or
	// empty to chat.
	Mode string `json:"mode,omitempty"`
	// Projector is a vision model's image encoder (llama.cpp's mmproj), so
	// the model can see pictures; empty for a model that reads text only.
	Projector string `json:"projector,omitempty"`
}

// Supporting model modes (AI experience spec §61). A model started in one of
// these modes serves only that endpoint and cannot chat.
const (
	ModeEmbedding = "embedding"
	ModeReranking = "reranking"
)

// Adapter is a LoRA adapter file for a base model.
type Adapter struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// RunningModel is a loaded model instance.
type RunningModel struct {
	ID        string `json:"id"`
	ModelID   string `json:"model_id"`
	Endpoint  string `json:"endpoint"`
	Status    string `json:"status"`
	RuntimeID string `json:"runtime_id"`
	// Adapters are the ids of the loaded LoRA adapters, in load order.
	Adapters []string `json:"adapters,omitempty"`
	// Mode is how the instance was started (see ModelStartConfig.Mode).
	Mode string `json:"mode,omitempty"`
	// Projector is the image encoder it was started with, if any.
	Projector string `json:"projector,omitempty"`
	// Acceleration is where the instance runs, from the runtime's own
	// report when it loaded the model; nil when it is not known.
	Acceleration *Acceleration `json:"acceleration,omitempty"`
}

// Acceleration is where a loaded model runs: the backend and devices the
// runtime used, and how much of the model it put on them.
type Acceleration struct {
	// Backend is "metal", "vulkan", "cuda", "rocm", "sycl", or "cpu".
	Backend string `json:"backend"`
	// Devices are the GPUs it used, such as "AMD Radeon RX 7900 XTX".
	Devices []string `json:"devices,omitempty"`
	// LayersOffloaded of LayersTotal layers are on a GPU; LayersTotal is 0
	// when the runtime did not say.
	LayersOffloaded int `json:"layers_offloaded"`
	LayersTotal     int `json:"layers_total"`
	// GPUMemoryBytes is what the model, its cache, and its working memory
	// take on the GPUs.
	GPUMemoryBytes uint64 `json:"gpu_memory_bytes,omitempty"`
}

// Runtime is the replaceable inference runtime adapter.
type Runtime interface {
	ID() string
	DisplayName() string

	Detect(ctx context.Context) (RuntimeDetection, error)
	Install(ctx context.Context, opts InstallOptions) error
	Update(ctx context.Context) error

	Capabilities(ctx context.Context) (RuntimeCapabilities, error)

	StartModel(ctx context.Context, cfg ModelStartConfig) (RunningModel, error)
	StopModel(ctx context.Context, id string) error
	ListRunning(ctx context.Context) ([]RunningModel, error)

	Health(ctx context.Context) error
}

// ChatRequest is a generation request to a running model.
type ChatRequest struct {
	ModelEndpoint string           `json:"model_endpoint"`
	Messages      []ChatMessage    `json:"messages"`
	Temperature   float64          `json:"temperature,omitempty"`
	MaxTokens     int              `json:"max_tokens,omitempty"`
	Stream        bool             `json:"stream"`
	Tools         []map[string]any `json:"tools,omitempty"`
	// Adapter applies one loaded LoRA adapter. Empty means the base model.
	Adapter string `json:"adapter,omitempty"`
	// ResponseSchema, when set, constrains the reply to JSON matching this
	// JSON Schema, where the runtime supports it.
	ResponseSchema json.RawMessage `json:"response_schema,omitempty"`
}

// ChatMessage is a single chat turn.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Images are pictures for a model that can see, as data URLs
	// ("data:image/png;base64,…"). A message with images is sent in the
	// OpenAI form, content parts: the text, then each image (#191).
	Images []string `json:"-"`
}

// FoldSystem moves the system messages into the first user message, marked
// as instructions, for a model whose chat template has no system role. Its
// template would put them there anyway, unmarked, and the model then answers
// them as if the user had written them.
func FoldSystem(messages []ChatMessage) []ChatMessage {
	var system []string
	out := make([]ChatMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			if s := strings.TrimSpace(m.Content); s != "" {
				system = append(system, s)
			}
			continue
		}
		out = append(out, m)
	}
	if len(system) == 0 {
		return messages
	}
	block := "Instructions for you, from the app, not from the user. Follow them without mentioning, repeating, or summarizing them:\n<instructions>\n" +
		strings.Join(system, "\n\n") + "\n</instructions>"
	for i, m := range out {
		if m.Role == "user" {
			out[i].Content = block + "\n\nThe user's message:\n" + m.Content
			return out
		}
	}
	return append([]ChatMessage{{Role: "user", Content: block}}, out...)
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

// MarshalJSON writes content as a string, or as parts when the message has
// images.
func (m ChatMessage) MarshalJSON() ([]byte, error) {
	if len(m.Images) == 0 {
		return json.Marshal(struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{m.Role, m.Content})
	}
	parts := make([]contentPart, 0, len(m.Images)+1)
	if m.Content != "" {
		parts = append(parts, contentPart{Type: "text", Text: m.Content})
	}
	for _, url := range m.Images {
		parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: url}})
	}
	return json.Marshal(struct {
		Role    string        `json:"role"`
		Content []contentPart `json:"content"`
	}{m.Role, parts})
}

// UnmarshalJSON reads content as a string or as OpenAI content parts, whose
// text parts are joined and whose images are kept.
func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = ChatMessage{Role: raw.Role}
	if len(raw.Content) == 0 || string(raw.Content) == "null" {
		return nil
	}
	if raw.Content[0] == '"' {
		return json.Unmarshal(raw.Content, &m.Content)
	}
	var parts []contentPart
	if err := json.Unmarshal(raw.Content, &parts); err != nil {
		return errors.New("message content must be a string or a list of content parts")
	}
	var texts []string
	for _, p := range parts {
		switch {
		case p.Type == "text":
			texts = append(texts, p.Text)
		case p.Type == "image_url" && p.ImageURL != nil && p.ImageURL.URL != "":
			m.Images = append(m.Images, p.ImageURL.URL)
		}
	}
	m.Content = strings.Join(texts, "\n")
	return nil
}

// ChatChunk is a streaming generation fragment.
type ChatChunk struct {
	Content string             `json:"content,omitempty"`
	Done    bool               `json:"done"`
	Error   string             `json:"error,omitempty"`
	Metrics *GenerationMetrics `json:"metrics,omitempty"`
	// Code and Details are Error's stable code and values, when the error
	// had its own (#588); without them, core recognizes the code from Error.
	Code    string         `json:"code,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// GenerationMetrics mirrors contracts.GenerationMetrics for runtime adapters.
type GenerationMetrics struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	TTFTMs           float64 `json:"ttft_ms"`
	PromptMs         float64 `json:"prompt_ms"`
	EvalMs           float64 `json:"eval_ms"`
	TotalMs          float64 `json:"total_ms"`
	PromptTokPerSec  float64 `json:"prompt_tok_per_sec"`
	EvalTokPerSec    float64 `json:"eval_tok_per_sec"`
	// CachedTokens are prompt tokens reused from the model's cache instead
	// of processed again, when the runtime reports them.
	CachedTokens int `json:"cached_tokens,omitempty"`
}

// Generator can stream chat completions against a running model.
type Generator interface {
	Chat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
}
