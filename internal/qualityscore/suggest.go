package qualityscore

// Suggest picks the model a person on this computer measures: the first on the
// list, which is in rough order of what's most worth measuring, that fits and
// has no result from this hardware yet. So each run adds something new. It falls
// back to the first that fits, and false when none does.
func Suggest(list []ListedModel, results []Row, hardware string, roomBytes uint64) (ListedModel, bool) {
	done := map[string]bool{}
	for _, r := range results {
		if r.Hardware == hardware {
			done[r.Model] = true
		}
	}
	fits := Fitting(list, roomBytes)
	for _, m := range fits {
		if !done[m.ID] {
			return m, true
		}
	}
	if len(fits) > 0 {
		return fits[0], true
	}
	return ListedModel{}, false
}

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
