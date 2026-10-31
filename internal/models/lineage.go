package models

import "fmt"

// lineageErrors checks the catalog's families: every model has a family and
// a generation, and a model supersedes only others of its own family from
// another generation, without going round in a circle (#593).
func lineageErrors(list []CatalogEntry) []error {
	byID := make(map[string]CatalogEntry, len(list))
	for _, e := range list {
		byID[e.ID] = e
	}
	var errs []error
	for _, e := range list {
		if e.Family == "" || e.Generation == "" {
			errs = append(errs, fmt.Errorf("%s has no family or generation", e.ID))
		}
		for _, id := range e.Supersedes {
			old, ok := byID[id]
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("%s supersedes %s, which isn't in the catalog", e.ID, id))
			case old.Family != e.Family:
				errs = append(errs, fmt.Errorf("%s supersedes %s, of another family", e.ID, id))
			case old.Generation == e.Generation:
				errs = append(errs, fmt.Errorf("%s supersedes %s, of the same generation", e.ID, id))
			case old.Supporting() != e.Supporting():
				errs = append(errs, fmt.Errorf("%s supersedes %s, which does another job", e.ID, id))
			}
		}
	}
	for _, e := range list {
		if supersedesItself(byID, e.ID) {
			errs = append(errs, fmt.Errorf("%s supersedes itself, by way of others", e.ID))
		}
	}
	return errs
}

// supersedesItself follows what id supersedes, and what those supersede,
// looking for id.
func supersedesItself(byID map[string]CatalogEntry, id string) bool {
	seen := map[string]bool{}
	next := byID[id].Supersedes
	for len(next) > 0 {
		cur := next[0]
		next = next[1:]
		if cur == id {
			return true
		}
		if seen[cur] {
			continue
		}
		seen[cur] = true
		next = append(next, byID[cur].Supersedes...)
	}
	return false
}
