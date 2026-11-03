package qualityscore

import "regexp"

// Popular is the most downloaded listed model that fits, and false when none does.
func Popular(list []ListedModel, roomBytes uint64) (ListedModel, bool) {
	var best ListedModel
	found := false
	for _, m := range Fitting(list, roomBytes) {
		if !found || m.Downloads > best.Downloads {
			best, found = m, true
		}
	}
	return best, found
}

// Unmeasured is the most downloaded listed model that fits and that nobody has a
// result for yet, on any computer, and false when there's none.
func Unmeasured(list []ListedModel, results []Row, roomBytes uint64) (ListedModel, bool) {
	done := map[string]bool{}
	for _, r := range results {
		done[r.Model] = true
	}
	var open []ListedModel
	for _, m := range list {
		if !done[m.ID] {
			open = append(open, m)
		}
	}
	return Popular(open, roomBytes)
}

// ModelLink is a link to one model file a person can measure: a GGUF file on
// Hugging Face, as Toskar's own install from a link takes.
func ModelLink(link string) bool {
	return modelLink.MatchString(link)
}

var modelLink = regexp.MustCompile(`^https://huggingface\.co/[^/\s]+/[^/\s]+/resolve/[^\s]+\.gguf(\?[^\s]*)?$`)

// Fitting are the listed models this much memory can hold: a model takes more
// than its file once it's running, so each needs a quarter more room.
func Fitting(list []ListedModel, roomBytes uint64) []ListedModel {
	var fits []ListedModel
	for _, m := range list {
		if m.SizeBytes+m.SizeBytes/4 <= roomBytes {
			fits = append(fits, m)
		}
	}
	return fits
}

// RoomFor is how much memory a model can use: a graphics card's own, or, as on
// a computer whose memory is shared, most of what's installed, whichever is more.
func RoomFor(dedicatedBytes, memoryBytes uint64) uint64 {
	shared := memoryBytes / 10 * 6
	if dedicatedBytes > shared {
		return dedicatedBytes
	}
	return shared
}
