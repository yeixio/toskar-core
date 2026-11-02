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

// StoreShelf is a row of the store. ID says what it's for: popular, top_rated,
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
