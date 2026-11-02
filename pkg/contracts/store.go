package contracts

import "time"

// StoreModel is a model on a shelf of the model store (#593): where it can be
// installed from, what people on Hugging Face make of it, and how it fits this
// computer.
type StoreModel struct {
	BrowseModel
	Likes     int       `json:"likes,omitempty"`
	CreatedAt time.Time `json:"created_at,omitzero"`
	// License is the license Hugging Face lists, such as apache-2.0, or empty.
	License string `json:"license,omitempty"`
	// DetailsURL is the model's page.
	DetailsURL string    `json:"details_url,omitempty"`
	Fit        *ModelFit `json:"fit,omitempty"`
	// CatalogID is its entry in Toskar's catalog, which has been tested with
	// Toskar and installs by that ID. Empty for everything else.
	CatalogID string `json:"catalog_id,omitempty"`
	Installed bool   `json:"installed,omitempty"`
}

// StoreShelf is a row of the store. ID says what it's for: picks, popular, top_rated,
// new, everyday, code, sees, small, or tested; others may be added.
type StoreShelf struct {
	ID     string       `json:"id"`
	Models []StoreModel `json:"models"`
}

// ModelStore is the store's front page. Live says Hugging Face answered: when
// it didn't, only models Toskar already knows are there.
type ModelStore struct {
	Shelves []StoreShelf `json:"shelves"`
	Live    bool         `json:"live"`
}

// QualityScore is how many cases a set passed.
type QualityScore struct {
	Passed int `json:"passed"`
	Of     int `json:"of"`
}

// QualityResult is one model, on one computer, on one date, as measured.
type QualityResult struct {
	Model              string                  `json:"model"`
	Name               string                  `json:"name"`
	Date               string                  `json:"date"`
	Hardware           string                  `json:"hardware"`
	MethodVersion      int                     `json:"method_version"`
	Submitted          bool                    `json:"submitted,omitempty"`
	SubmittedBy        string                  `json:"submitted_by,omitempty"`
	Source             string                  `json:"source,omitempty"`
	Scores             map[string]QualityScore `json:"scores"`
	SecondsPerQuestion float64                 `json:"seconds_per_question,omitempty"`
	// Performance is how the model ran on that computer, for rows measured
	// that way (contract 1.81). It's never part of a score.
	Performance *QualityPerformance `json:"performance,omitempty"`
}

// QualityPerformance is how a model ran, from a long prompt new each run.
type QualityPerformance struct {
	PromptTokens             int     `json:"prompt_tokens"`
	PromptTokensPerSecond    float64 `json:"prompt_tokens_per_second"`
	TokensPerSecond          float64 `json:"tokens_per_second"`
	FirstTokenMs             float64 `json:"first_token_ms"`
	EffectiveTokensPerSecond float64 `json:"effective_tokens_per_second"`
	GPULayers                int     `json:"gpu_layers"`
	GPULayersTotal           int     `json:"gpu_layers_total"`
	GPUMemoryBytes           uint64  `json:"gpu_memory_bytes,omitempty"`
	MemoryBytes              uint64  `json:"memory_bytes,omitempty"`
	ContextWindow            int     `json:"context_window,omitempty"`
	Quantization             string  `json:"quantization,omitempty"`
}

// QualityResults are Toskar's public quality results (#593). They draw no
// conclusions. Live says they could be read: when not, Results is empty.
type QualityResults struct {
	Live    bool              `json:"live"`
	Note    string            `json:"note,omitempty"`
	Method  string            `json:"method,omitempty"`
	Suites  map[string]string `json:"suites,omitempty"`
	Results []QualityResult   `json:"results"`
}
