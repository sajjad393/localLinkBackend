//go:build !linux && !windows

package httpapi

// Platforms without a built-in reader report unavailable metrics instead of
// fabricated values; the health panel renders these as "unknown".
func platformCPUSeconds() (float64, bool)  { return 0, false }
func platformMemory() (uint64, uint64, bool) { return 0, 0, false }
func platformDisk(string) (uint64, uint64, bool) { return 0, 0, false }
