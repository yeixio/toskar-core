package hardware

import (
	"context"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Detector gathers host hardware inventory.
type Detector struct {
	DataPath string
	// MaxAge keeps an inventory for this long: one chat turn asks several
	// times, and each look runs system tools that take a few hundred
	// milliseconds on macOS (#795). Zero looks every time.
	MaxAge time.Duration

	mu     sync.Mutex
	last   contracts.HardwareInventory
	lastAt time.Time
}

// Detect returns the best-effort hardware inventory for this host.
// Partial failures degrade fields rather than failing the whole call.
func (d *Detector) Detect(ctx context.Context) (contracts.HardwareInventory, error) {
	if d.MaxAge <= 0 {
		return d.detect(ctx)
	}
	// Callers at the same time share one look.
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lastAt.IsZero() || time.Since(d.lastAt) >= d.MaxAge {
		inv, err := d.detect(ctx)
		if err != nil {
			return inv, err
		}
		d.last, d.lastAt = inv, time.Now()
	}
	inv := d.last
	inv.Accelerators = slices.Clone(d.last.Accelerators)
	for i := range inv.Accelerators {
		inv.Accelerators[i].Backends = slices.Clone(inv.Accelerators[i].Backends)
	}
	return inv, nil
}

func (d *Detector) detect(ctx context.Context) (contracts.HardwareInventory, error) {
	inv := contracts.HardwareInventory{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		DetectedAt:   time.Now().UTC(),
		Accelerators: []contracts.Accelerator{},
	}
	if h, err := os.Hostname(); err == nil {
		inv.Hostname = h
	}

	cpu, err := detectCPU(ctx)
	if err == nil {
		inv.CPU = cpu
	} else if inv.CPU.Cores == 0 {
		inv.CPU = contracts.CPUInfo{Model: "Unknown CPU", Cores: runtime.NumCPU(), Threads: runtime.NumCPU()}
	}

	mem, err := detectMemory(ctx)
	if err == nil {
		inv.Memory = mem
	}

	path := d.DataPath
	if path == "" {
		path = "."
	}
	disk, err := detectDisk(ctx, path)
	if err == nil {
		inv.Disk = disk
	} else {
		inv.Disk = contracts.DiskInfo{Path: path}
	}

	accels, err := detectAccelerators(ctx)
	if err == nil && len(accels) > 0 {
		inv.Accelerators = accels
	}

	return inv, nil
}
