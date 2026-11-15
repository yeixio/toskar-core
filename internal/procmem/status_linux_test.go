package procmem

import "testing"

// On Linux only what a process allocated counts, not the files it maps
// (#1066).
func TestAllocated(t *testing.T) {
	status := "Name:\tllama-server\nVmRSS:\t 9115560 kB\nRssAnon:\t  2539140 kB\nRssFile:\t 6553600 kB\nRssShmem:\t   22820 kB\nVmSwap:\t0 kB\n"
	n, ok := allocated(status)
	if !ok || n != (2539140+22820)*1024 {
		t.Errorf("allocated = %d, %v", n, ok)
	}
	if _, ok := allocated("Name:\tkthreadd\n"); ok {
		t.Error("a status without RssAnon was read")
	}
}
