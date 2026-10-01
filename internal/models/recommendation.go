package models

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

const memorySafetyMargin = 0.85

// RecommendInput drives deterministic model recommendations.
type RecommendInput struct {
	Purpose  string
	Hardware contracts.HardwareInventory
}

// Recommend selects models and role assignments for a purpose given hardware.
func Recommend(catalog *Catalog, input RecommendInput) (contracts.Recommendation, error) {
	if catalog == nil {
		return contracts.Recommendation{}, fmt.Errorf("catalog unavailable")
	}
	purpose := strings.ToLower(strings.TrimSpace(input.Purpose))
	if purpose == "" {
		purpose = "general"
	}

	availableMem := effectiveMemory(input.Hardware)
	candidates := filterByPurpose(catalog.List(), purpose)
	if len(candidates) == 0 {
		candidates = catalog.List()
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].MemoryNeededBytes < candidates[j].MemoryNeededBytes
	})

	var fits []CatalogEntry
	for _, c := range candidates {
		if c.MemoryNeededBytes == 0 || uint64(float64(availableMem)*memorySafetyMargin) >= c.MemoryNeededBytes {
			fits = append(fits, c)
		}
	}
	if len(fits) == 0 && len(candidates) > 0 {
		fits = []CatalogEntry{candidates[0]}
	}
	if len(fits) == 0 {
		return contracts.Recommendation{}, fmt.Errorf("no models in catalog")
	}

	// Prefer the smallest fitting model for a snappy first-run experience.
	// Advanced users can swap models later in Profiles.
	primary := fits[0]
	for _, f := range fits {
		if purpose == "coding" && f.Capabilities.Coding {
			primary = f
			break
		}
	}

	secondary := primary
	if len(fits) >= 2 {
		// Optional larger worker only when it still fits and is coding-capable.
		for i := len(fits) - 1; i >= 0; i-- {
			f := fits[i]
			if f.ID == primary.ID {
				continue
			}
			if purpose == "coding" && !f.Capabilities.Coding {
				continue
			}
			// Keep secondary small unless there is clear headroom (2x primary).
			if availableMem >= primary.MemoryNeededBytes*4 && f.MemoryNeededBytes > primary.MemoryNeededBytes {
				secondary = f
			}
			break
		}
	}

	roles := buildRoles(purpose, primary, secondary)
	seen := map[string]struct{}{}
	var modelsOut []contracts.Model
	for _, role := range roles {
		if _, ok := seen[role.ModelID]; ok {
			continue
		}
		seen[role.ModelID] = struct{}{}
		switch role.ModelID {
		case primary.ID:
			modelsOut = append(modelsOut, entryToContract(primary, false))
		case secondary.ID:
			modelsOut = append(modelsOut, entryToContract(secondary, false))
		}
	}
	if len(modelsOut) == 0 {
		modelsOut = []contracts.Model{entryToContract(primary, false)}
	}

	reason := buildReason(purpose, primary, availableMem, input.Hardware)
	var storage uint64
	for _, m := range modelsOut {
		storage += m.SizeBytes
	}

	return contracts.Recommendation{
		Purpose:      purpose,
		Roles:        roles,
		Models:       modelsOut,
		Reason:       reason,
		StorageBytes: storage,
		VRAMBytes:    vramTotal(input.Hardware),
	}, nil
}

func effectiveMemory(hw contracts.HardwareInventory) uint64 {
	vram := vramTotal(hw)
	if vram > 0 {
		return vram
	}
	if hw.Memory.AvailableBytes > 0 {
		return hw.Memory.AvailableBytes
	}
	return hw.Memory.TotalBytes
}

func vramTotal(hw contracts.HardwareInventory) uint64 {
	var total uint64
	for _, a := range hw.Accelerators {
		if a.DedicatedVRAM > 0 {
			total += a.DedicatedVRAM
		} else if a.UnifiedMemory > 0 {
			total += a.UnifiedMemory
		}
	}
	return total
}

func filterByPurpose(entries []CatalogEntry, purpose string) []CatalogEntry {
	var out []CatalogEntry
	for _, e := range entries {
		for _, p := range e.Purpose {
			if strings.EqualFold(p, purpose) || (purpose == "general" && p == "assistant") {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

func buildRoles(purpose string, primary, secondary CatalogEntry) []contracts.ModelRole {
	switch purpose {
	case "coding":
		return []contracts.ModelRole{
			{Role: "coordinator", ModelID: secondary.ID, Required: true},
			{Role: "worker", ModelID: primary.ID, Required: true},
			{Role: "reviewer", ModelID: primary.ID, Required: true},
		}
	case "research":
		return []contracts.ModelRole{
			{Role: "coordinator", ModelID: primary.ID, Required: true},
			{Role: "researcher", ModelID: primary.ID, Required: true},
		}
	default:
		return []contracts.ModelRole{
			{Role: "assistant", ModelID: primary.ID, Required: true},
		}
	}
}

func buildReason(purpose string, primary CatalogEntry, mem uint64, hw contracts.HardwareInventory) string {
	vram := vramTotal(hw)
	if vram > 0 && primary.MemoryNeededBytes > 0 && vram >= primary.MemoryNeededBytes {
		return fmt.Sprintf("Recommended %s because it fits in your GPU memory (%d MB) and supports %s tasks.",
			primary.DisplayName, vram/(1024*1024), purpose)
	}
	if mem > 0 && primary.MemoryNeededBytes > 0 && mem >= primary.MemoryNeededBytes {
		return fmt.Sprintf("Recommended %s because it fits in available system memory for %s use.",
			primary.DisplayName, purpose)
	}
	return fmt.Sprintf("Recommended %s as the best available match for %s on this hardware.",
		primary.DisplayName, purpose)
}

// Prefer preset order when filtering candidates for a purpose.
func preferPresetOrder(candidates []CatalogEntry, preferred []string) []CatalogEntry {
	if len(preferred) == 0 {
		return candidates
	}
	byID := map[string]CatalogEntry{}
	for _, c := range candidates {
		byID[c.ID] = c
	}
	var ordered []CatalogEntry
	seen := map[string]struct{}{}
	for _, id := range preferred {
		if c, ok := byID[id]; ok {
			ordered = append(ordered, c)
			seen[id] = struct{}{}
		}
	}
	for _, c := range candidates {
		if _, ok := seen[c.ID]; ok {
			continue
		}
		ordered = append(ordered, c)
	}
	return ordered
}

// RecommendWithPresets selects models using purpose presets when available.
func RecommendWithPresets(catalog *Catalog, presets []PurposePreset, input RecommendInput) (contracts.Recommendation, error) {
	if catalog == nil {
		return contracts.Recommendation{}, fmt.Errorf("catalog unavailable")
	}
	purpose := strings.ToLower(strings.TrimSpace(input.Purpose))
	if purpose == "" {
		purpose = "general"
	}
	var preferred []string
	for _, p := range presets {
		if strings.EqualFold(p.ID, purpose) {
			preferred = p.PreferredModels
			break
		}
	}

	availableMem := effectiveMemory(input.Hardware)
	candidates := filterByPurpose(catalog.List(), purpose)
	if len(candidates) == 0 {
		candidates = catalog.List()
	}
	candidates = preferPresetOrder(candidates, preferred)

	sort.SliceStable(candidates, func(i, j int) bool {
		// Keep preset order primary; within non-preferred, smaller first.
		pi, pj := -1, -1
		for idx, id := range preferred {
			if candidates[i].ID == id {
				pi = idx
			}
			if candidates[j].ID == id {
				pj = idx
			}
		}
		if pi >= 0 || pj >= 0 {
			if pi < 0 {
				return false
			}
			if pj < 0 {
				return true
			}
			return pi < pj
		}
		return candidates[i].MemoryNeededBytes < candidates[j].MemoryNeededBytes
	})

	var fits []CatalogEntry
	for _, c := range candidates {
		if c.MemoryNeededBytes == 0 || uint64(float64(availableMem)*memorySafetyMargin) >= c.MemoryNeededBytes {
			fits = append(fits, c)
		}
	}
	if len(fits) == 0 && len(candidates) > 0 {
		fits = []CatalogEntry{candidates[0]}
	}
	if len(fits) == 0 {
		return contracts.Recommendation{}, fmt.Errorf("no models in catalog")
	}

	primary := fits[0]
	for _, f := range fits {
		if purpose == "coding" && f.Capabilities.Coding {
			primary = f
			break
		}
	}

	secondary := primary
	if len(fits) >= 2 {
		for i := len(fits) - 1; i >= 0; i-- {
			f := fits[i]
			if f.ID == primary.ID {
				continue
			}
			if purpose == "coding" && !f.Capabilities.Coding {
				continue
			}
			if availableMem >= primary.MemoryNeededBytes*4 && f.MemoryNeededBytes > primary.MemoryNeededBytes {
				secondary = f
			}
			break
		}
	}

	roles := buildRoles(purpose, primary, secondary)
	seen := map[string]struct{}{}
	var modelsOut []contracts.Model
	for _, role := range roles {
		if _, ok := seen[role.ModelID]; ok {
			continue
		}
		seen[role.ModelID] = struct{}{}
		switch role.ModelID {
		case primary.ID:
			modelsOut = append(modelsOut, entryToContract(primary, false))
		case secondary.ID:
			modelsOut = append(modelsOut, entryToContract(secondary, false))
		}
	}
	if len(modelsOut) == 0 {
		modelsOut = []contracts.Model{entryToContract(primary, false)}
	}

	reason := buildReason(purpose, primary, availableMem, input.Hardware)
	var storage uint64
	for _, m := range modelsOut {
		storage += m.SizeBytes
	}

	return contracts.Recommendation{
		Purpose:      purpose,
		Roles:        roles,
		Models:       modelsOut,
		Reason:       reason,
		StorageBytes: storage,
		VRAMBytes:    vramTotal(input.Hardware),
	}, nil
}

func entryToContract(e CatalogEntry, installed bool) contracts.Model {
	m := contracts.Model{
		ID:           e.ID,
		DisplayName:  e.DisplayName,
		Summary:      e.Summary,
		Family:       e.Family,
		Variant:      e.Variant,
		Parameters:   e.Parameters,
		SizeBytes:    e.SizeBytes,
		MemoryNeeded: e.MemoryNeededBytes,
		Context:      e.Context,
		Capabilities: e.Capabilities,
		Source:       e.Source,
		Purpose:      e.Purpose,
		Tags:         e.Tags,
		Runtime:      e.Runtime,
		Roles:        e.RecommendedRoles,
		Installed:    installed,
		Dynamic:      e.Dynamic,
		SupportRole:  e.SupportRole,
	}
	m.SupportRole = contracts.SupportRoleOf(m)
	return m
}
