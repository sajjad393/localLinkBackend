package httpapi

import (
	"runtime"
	"sync"
	"time"
)

// processSnapshot holds process/host resource readings for the admin health
// panel. Every reading carries an "ok" flag: a value that cannot be measured on
// the current platform is reported as unavailable rather than fabricated.
type processSnapshot struct {
	CPUPercent  float64
	CPUOK       bool
	MemBytes    uint64
	MemTotal    uint64
	MemOK       bool
	DiskFree    uint64
	DiskTotal   uint64
	DiskOK      bool
	GOHeapBytes uint64
	Goroutines  int
}

var (
	procSampleMu  sync.Mutex
	procPrevAt    time.Time
	procPrevCPU   float64
	procPrevOK    bool
	procLastValue float64
)

// SampleProcess reads process CPU/memory and host disk usage for dir.
// CPU percent is derived from the delta between successive samples and is
// normalised across all cores (0-100).
func SampleProcess(dir string) processSnapshot {
	var snap processSnapshot

	cpuSeconds, cpuOK := platformCPUSeconds()
	memBytes, memTotal, memOK := platformMemory()
	diskFree, diskTotal, diskOK := platformDisk(dir)

	now := time.Now()
	procSampleMu.Lock()
	if cpuOK && procPrevOK {
		elapsed := now.Sub(procPrevAt).Seconds()
		if elapsed > 0.05 {
			delta := cpuSeconds - procPrevCPU
			if delta >= 0 {
				cores := float64(numCPUs())
				if cores < 1 {
					cores = 1
				}
				procLastValue = (delta / elapsed) * 100 / cores
				if procLastValue < 0 {
					procLastValue = 0
				}
			}
		}
		snap.CPUOK = true
		snap.CPUPercent = procLastValue
	}
	if cpuOK {
		procPrevCPU = cpuSeconds
		procPrevOK = true
	} else {
		procPrevOK = false
	}
	procPrevAt = now
	procSampleMu.Unlock()

	snap.MemBytes, snap.MemTotal, snap.MemOK = memBytes, memTotal, memOK
	snap.DiskFree, snap.DiskTotal, snap.DiskOK = diskFree, diskTotal, diskOK
	return snap
}

func numCPUs() int {
	if n := runtime.NumCPU(); n > 0 {
		return n
	}
	return 1
}
