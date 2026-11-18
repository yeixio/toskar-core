package models

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Every catalog model says what its license allows, checked by hand
// (#593): a model without one fails here.
func TestCatalogLicenses(t *testing.T) {
	c, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range licenseErrors(c.List()) {
		t.Error(err)
	}
}

func TestLicenseErrors(t *testing.T) {
	apache := &contracts.ModelLicense{ID: "apache-2.0", Name: "Apache 2.0", URL: "https://www.apache.org/licenses/LICENSE-2.0", Commercial: "yes"}
	cases := []struct {
		name string
		lic  *contracts.ModelLicense
		want string
	}{
		{"fine", apache, ""},
		{"none", nil, "has no license"},
		{"no link", &contracts.ModelLicense{ID: "mit", Name: "MIT", Commercial: "yes"}, "https link"},
		{"unknown commercial", &contracts.ModelLicense{ID: "mit", Name: "MIT", URL: "https://x", Commercial: "maybe"}, "not yes, limited, or no"},
		{"limit without a note", &contracts.ModelLicense{ID: "x", Name: "X", URL: "https://x", Commercial: "no"}, "without a note"},
		{"kind set", &contracts.ModelLicense{ID: "mit", Name: "MIT", URL: "https://x", Commercial: "yes", Kind: "free"}, "sets kind"},
	}
	for _, tc := range cases {
		errs := licenseErrors([]CatalogEntry{{ID: "m", License: tc.lic}})
		if tc.want == "" {
			if len(errs) > 0 {
				t.Errorf("%s: %v", tc.name, errs)
			}
			continue
		}
		if len(errs) == 0 || !strings.Contains(errs[0].Error(), tc.want) {
			t.Errorf("%s: want %q, got %v", tc.name, tc.want, errs)
		}
	}
	if errs := licenseErrors([]CatalogEntry{{ID: "added", Dynamic: true}}); len(errs) > 0 {
		t.Errorf("a model added from a link needs no license: %v", errs)
	}
}

// Free is any use with nothing to accept or credit; anything else is free
// with conditions.
func TestLicenseKind(t *testing.T) {
	for _, c := range []struct {
		lic  contracts.ModelLicense
		want string
	}{
		{contracts.ModelLicense{Commercial: "yes"}, "free"},
		{contracts.ModelLicense{Commercial: "yes", Accept: true}, "conditions"},
		{contracts.ModelLicense{Commercial: "limited", Attribution: true, Accept: true}, "conditions"},
		{contracts.ModelLicense{Commercial: "no"}, "conditions"},
	} {
		if got := c.lic.KindOf(); got != c.want {
			t.Errorf("%+v: %s, want %s", c.lic, got, c.want)
		}
	}
}

// A store listing says what its license allows: the catalog's checked
// license for a tested model, otherwise what Hugging Face lists, and
// nothing when that doesn't say.
func TestStoreLicense(t *testing.T) {
	llama := &CatalogEntry{License: &contracts.ModelLicense{Name: "Llama 3.2 Community License", Commercial: "limited", Attribution: true, Accept: true}}
	for _, c := range []struct {
		hub              string
		entry            *CatalogEntry
		commercial, kind string
	}{
		{"apache-2.0", nil, "yes", "free"},
		{"MIT", nil, "yes", "free"},
		{"llama3.1", nil, "limited", "conditions"},
		{"gemma", nil, "yes", "conditions"},
		{"cc-by-nc-4.0", nil, "no", "conditions"},
		{"other", nil, "", ""},
		{"", nil, "", ""},
		{"llama3.2", llama, "limited", "conditions"},
		{"", llama, "limited", "conditions"},
	} {
		m := contracts.StoreModel{License: c.hub}
		storeLicense(&m, c.entry)
		if m.Commercial != c.commercial || m.LicenseKind != c.kind {
			t.Errorf("%q: %q %q, want %q %q", c.hub, m.Commercial, m.LicenseKind, c.commercial, c.kind)
		}
		if c.entry != nil && c.hub == "" && m.License != "Llama 3.2 Community License" {
			t.Errorf("a tested model's license name: %q", m.License)
		}
	}
}

// A catalog model's license reaches clients with its kind.
func TestEntryCarriesLicense(t *testing.T) {
	e := CatalogEntry{ID: "g", License: &contracts.ModelLicense{ID: "gemma", Commercial: "yes", Accept: true}}
	m := entryToContract(e, false)
	if m.License == nil || m.License.Kind != "conditions" || m.License.ID != "gemma" {
		t.Fatalf("license %+v", m.License)
	}
	if e.License.Kind != "" {
		t.Error("the catalog entry's license was changed")
	}
}
