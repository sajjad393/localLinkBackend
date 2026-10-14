//go:build windows

package httpapi

import (
	"syscall"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	psapi                 = syscall.NewLazyDLL("psapi.dll")
	procGetCurrentProcess = kernel32.NewProc("GetCurrentProcess")
	procGetProcessTimes   = kernel32.NewProc("GetProcessTimes")
	procGetProcessMemory  = psapi.NewProc("GetProcessMemoryInfo")
	procGlobalMemoryStat  = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeSpace  = kernel32.NewProc("GetDiskFreeSpaceExW")
)

type filetime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

func (f filetime) seconds() float64 {
	ticks := (uint64(f.HighDateTime) << 32) | uint64(f.LowDateTime)
	return float64(ticks) / 1e7 // 100-ns intervals
}

type processMemoryCounters struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// platformCPUSeconds returns cumulative CPU seconds consumed by this process.
func platformCPUSeconds() (float64, bool) {
	handle, _, _ := procGetCurrentProcess.Call()
	if handle == 0 {
		return 0, false
	}
	var creation, exit, kernel, user filetime
	ret, _, _ := procGetProcessTimes.Call(handle,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)))
	if ret == 0 {
		return 0, false
	}
	return kernel.seconds() + user.seconds(), true
}

func platformMemory() (uint64, uint64, bool) {
	var rss uint64
	handle, _, _ := procGetCurrentProcess.Call()
	if handle != 0 {
		var counters processMemoryCounters
		counters.Cb = uint32(unsafe.Sizeof(counters))
		ret, _, _ := procGetProcessMemory.Call(handle, uintptr(unsafe.Pointer(&counters)), uintptr(counters.Cb))
		if ret != 0 {
			rss = uint64(counters.WorkingSetSize)
		}
	}
	var total uint64
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	ret, _, _ := procGlobalMemoryStat.Call(uintptr(unsafe.Pointer(&status)))
	if ret != 0 {
		total = status.TotalPhys
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
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	ret, _, _ := procGetDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)))
	if ret == 0 {
		return 0, 0, false
	}
	return totalFreeBytes, totalBytes, true
}
