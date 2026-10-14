//go:build linux

package httpapi

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// platformCPUSeconds returns cumulative CPU seconds consumed by this process.
func platformCPUSeconds() (float64, bool) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return 0, false
	}
	utime, err1 := strconv.ParseFloat(fields[13], 64)
	stime, err2 := strconv.ParseFloat(fields[14], 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	// /proc reports clock ticks; USER_HZ is 100 on effectively all Linux builds.
	return (utime + stime) / 100.0, true
}

func platformMemory() (uint64, uint64, bool) {
	var rss uint64
	if data, err := os.ReadFile("/proc/self/statm"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 2 {
			if pages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				rss = pages * uint64(os.Getpagesize())
			}
		}
	}
	var total uint64
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
						total = kb * 1024
					}
				}
				break
			}
		}
	}
	if rss == 0 && total == 0 {
		return 0, 0, false
	}
	return rss, total, true
}

func platformDisk(path string) (uint64, uint64, bool) {
	if path == "" {
		path = "."
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	bsize := uint64(st.Bsize)
	return st.Bavail * bsize, st.Blocks * bsize, true
}
