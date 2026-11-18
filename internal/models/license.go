package models

import (
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// licenseErrors checks the catalog's licenses (#593): every curated model
// has one, checked by hand, that says what it allows.
func licenseErrors(list []CatalogEntry) []error {
	var errs []error
	for _, e := range list {
		if e.Dynamic {
			continue
		}
		l := e.License
		if l == nil {
			errs = append(errs, fmt.Errorf("%s has no license", e.ID))
			continue
		}
		if l.ID == "" || l.Name == "" || !strings.HasPrefix(l.URL, "https://") {
			errs = append(errs, fmt.Errorf("%s's license needs an id, a name, and an https link", e.ID))
		}
		switch l.Commercial {
		case contracts.CommercialYes, contracts.CommercialLimited, contracts.CommercialNo:
		default:
			errs = append(errs, fmt.Errorf("%s's license says commercial %q, not yes, limited, or no", e.ID, l.Commercial))
		}
		if l.Commercial != contracts.CommercialYes && l.Notes == "" {
			errs = append(errs, fmt.Errorf("%s's license limits business use without a note saying how", e.ID))
		}
		if l.Kind != "" {
			errs = append(errs, fmt.Errorf("%s's license sets kind, which follows from the other fields", e.ID))
		}
	}
	return errs
}

// hubCommercial is whether a business may use a model under a license
// Hugging Face lists, for the licenses that say so plainly. Empty is
// unknown: "other", a license this doesn't know, or none.
func hubCommercial(license string) (commercial, kind string) {
	id := strings.ToLower(strings.TrimSpace(license))
	switch {
	case id == "apache-2.0", id == "mit", id == "bsd-2-clause", id == "bsd-3-clause", id == "isc", id == "unlicense", id == "cc0-1.0":
		return contracts.CommercialYes, contracts.LicenseFree
	case id == "cc-by-4.0", id == "cc-by-sa-4.0", id == "gemma", strings.Contains(id, "openrail"):
		// Business use, with credit to give or use restrictions to accept.
		return contracts.CommercialYes, contracts.LicenseConditions
	case strings.HasPrefix(id, "llama"):
		return contracts.CommercialLimited, contracts.LicenseConditions
	case strings.HasPrefix(id, "cc-by-nc"):
		return contracts.CommercialNo, contracts.LicenseConditions
	}
	return "", ""
}

// storeLicense says what a store listing's license allows: the catalog's
// checked license for a tested model, otherwise what Hugging Face lists.
func storeLicense(m *contracts.StoreModel, entry *CatalogEntry) {
	if entry != nil && entry.License != nil {
		m.Commercial, m.LicenseKind = entry.License.Commercial, entry.License.KindOf()
		if m.License == "" {
			m.License = entry.License.Name
		}
		return
	}
	m.Commercial, m.LicenseKind = hubCommercial(m.License)
}
