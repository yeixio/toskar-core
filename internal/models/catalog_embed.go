package models

import (
	"encoding/json"

	rootmanifests "github.com/yeixio/toskar-core/manifests"
)

// NewCatalogEmbedded loads the embedded catalog from repo manifests.
func NewCatalogEmbedded() (*Catalog, error) {
	var manifest CatalogManifest
	if err := json.Unmarshal(rootmanifests.CatalogJSON, &manifest); err != nil {
		return nil, err
	}
	c := &Catalog{
		path:    "embedded:catalog.json",
		entries: make(map[string]CatalogEntry, len(manifest.Models)),
	}
	for _, m := range manifest.Models {
		c.entries[m.ID] = m
	}
	return c, nil
}

// TestedEntries are the entries Toskar ships, left out of which are models a
// person added, from a link or the store. Those are in the catalog too, once
// installed, and haven't been tested for being there.
func TestedEntries(all []CatalogEntry) []CatalogEntry {
	shipped, err := NewCatalogEmbedded()
	if err != nil {
		return nil
	}
	var out []CatalogEntry
	for _, e := range all {
		if _, ok := shipped.Get(e.ID); ok {
			out = append(out, e)
		}
	}
	return out
}

// LoadPresetsEmbedded loads embedded presets.
func LoadPresetsEmbedded() ([]PurposePreset, error) {
	var manifest PresetsManifest
	if err := json.Unmarshal(rootmanifests.PresetsJSON, &manifest); err != nil {
		return nil, err
	}
	return manifest.Purposes, nil
}
