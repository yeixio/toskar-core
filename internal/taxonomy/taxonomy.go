// Package taxonomy is what models do, for browsing the store (#593):
// categories such as chat and coding or speech, the tasks in each, and the
// kinds of input and output a model takes. It says what a model does, never
// that Toskar can run it; a catalog entry's runtime says that.
package taxonomy

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/yeixio/toskar-core/manifests"
)

// Uncategorized is the category of a model nobody has checked yet, such as
// one added from a link (#467).
const Uncategorized = "uncategorized"

// Category is a kind of work, with the tasks in it.
type Category struct {
	ID    string   `json:"id"`
	Tasks []string `json:"tasks"`
}

// Taxonomy is categories.json.
type Taxonomy struct {
	Version    int        `json:"version"`
	Categories []Category `json:"categories"`
	Modalities []string   `json:"modalities"`
}

// Profile are what one model does. Each list holds IDs from the taxonomy.
type Profile struct {
	// PrimaryCategory is where the store shows the model first.
	PrimaryCategory string `json:"primary_category,omitempty"`
	// Categories are every category it belongs to, the primary one too.
	Categories []string `json:"categories,omitempty"`
	// Tasks are what it does, each from one of its categories.
	Tasks []string `json:"tasks,omitempty"`
	// InputModalities and OutputModalities are what it takes and gives,
	// such as text and image.
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
}

var builtin = mustParse(manifests.CategoriesJSON)

// Builtin is the taxonomy Toskar ships.
func Builtin() Taxonomy { return builtin }

func mustParse(raw []byte) Taxonomy {
	t, err := Parse(raw)
	if err != nil {
		panic(err)
	}
	return t
}

// Parse reads a taxonomy and checks that its IDs are unique.
func Parse(raw []byte) (Taxonomy, error) {
	var t Taxonomy
	if err := json.Unmarshal(raw, &t); err != nil {
		return Taxonomy{}, fmt.Errorf("categories.json: %w", err)
	}
	if t.Version <= 0 || len(t.Categories) == 0 || len(t.Modalities) == 0 {
		return Taxonomy{}, fmt.Errorf("categories.json needs a version, categories, and modalities")
	}
	seen := map[string]string{}
	for _, c := range t.Categories {
		if c.ID == "" || c.ID == Uncategorized || len(c.Tasks) == 0 {
			return Taxonomy{}, fmt.Errorf("categories.json: category %q needs an ID and tasks", c.ID)
		}
		for _, id := range append([]string{c.ID}, c.Tasks...) {
			if other, dup := seen[id]; dup {
				return Taxonomy{}, fmt.Errorf("categories.json: %q is in both %s and %s", id, other, c.ID)
			}
			seen[id] = c.ID
		}
	}
	return t, nil
}

func (t Taxonomy) category(id string) (Category, bool) {
	for _, c := range t.Categories {
		if c.ID == id {
			return c, true
		}
	}
	return Category{}, false
}

// Check says what's wrong with a model's profile, or nil. Every curated model
// needs a primary category among its categories, at least one task, and
// its inputs and outputs; each ID must be in the taxonomy, and each task in
// one of the model's categories.
func (t Taxonomy) Check(p Profile) error {
	if p.PrimaryCategory == "" || !slices.Contains(p.Categories, p.PrimaryCategory) {
		return fmt.Errorf("needs a primary_category that is among its categories")
	}
	if len(p.Tasks) == 0 || len(p.InputModalities) == 0 || len(p.OutputModalities) == 0 {
		return fmt.Errorf("needs tasks, input_modalities, and output_modalities")
	}
	var tasks []string
	for _, id := range p.Categories {
		c, ok := t.category(id)
		if !ok {
			return fmt.Errorf("category %q isn't in categories.json", id)
		}
		tasks = append(tasks, c.Tasks...)
	}
	for _, task := range p.Tasks {
		if !slices.Contains(tasks, task) {
			return fmt.Errorf("task %q isn't in its categories %v", task, p.Categories)
		}
	}
	for _, m := range append(slices.Clone(p.InputModalities), p.OutputModalities...) {
		if !slices.Contains(t.Modalities, m) {
			return fmt.Errorf("modality %q isn't in categories.json", m)
		}
	}
	for name, list := range map[string][]string{"categories": p.Categories, "tasks": p.Tasks, "input_modalities": p.InputModalities, "output_modalities": p.OutputModalities} {
		if dup := duplicate(list); dup != "" {
			return fmt.Errorf("%s lists %q twice", name, dup)
		}
	}
	return nil
}

func duplicate(list []string) string {
	seen := map[string]bool{}
	for _, v := range list {
		if seen[v] {
			return v
		}
		seen[v] = true
	}
	return ""
}

// OrUncategorized is a profile as the store shows them: a model nobody has
// categorized is Uncategorized.
func (p Profile) OrUncategorized() Profile {
	if p.PrimaryCategory == "" {
		p.PrimaryCategory = Uncategorized
		p.Categories = []string{Uncategorized}
	}
	return p
}
