package models

import (
	"fmt"

	"github.com/yeixio/toskar-core/internal/taxonomy"
)

// categoryErrors checks the catalog's categories (#593): every curated
// model says what it does, with IDs from categories.json. A model added
// from a link is uncategorized until someone checks it.
func categoryErrors(list []CatalogEntry) []error {
	tax := taxonomy.Builtin()
	var errs []error
	for _, e := range list {
		if e.Dynamic {
			continue
		}
		if err := tax.Check(e.Profile); err != nil {
			errs = append(errs, fmt.Errorf("%s %w", e.ID, err))
		}
	}
	return errs
}
