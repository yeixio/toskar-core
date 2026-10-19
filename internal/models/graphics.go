package models

// SharedGraphics is how much of Apple silicon's memory its graphics may
// use: macOS lets the GPU wire about two thirds of it, and three quarters
// above 36 GB (its recommendedMaxWorkingSetSize). 24 GB of memory isn't
// 24 GB of graphics memory (#550), and llama.cpp's Metal backend can't use
// more for a chat model either (#751).
func SharedGraphics(memory uint64) uint64 {
	if memory > 36<<30 {
		return memory / 4 * 3
	}
	return memory / 3 * 2
}
