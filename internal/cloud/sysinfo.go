package cloud

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// cpuTemp reads the CPU temperature in degrees Celsius from the Linux
// thermal zone. Returns 0 if unavailable (non-Linux or no thermal zone).
func cpuTemp() float64 {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	milliC, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return float64(milliC) / 1000.0
}

// memUsedPct returns the percentage of physical memory in use (0–100).
func memUsedPct() float64 {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0
	}
	total := uint64(info.Totalram) * uint64(info.Unit)
	free := uint64(info.Freeram) * uint64(info.Unit)
	if total == 0 {
		return 0
	}
	return float64(total-free) / float64(total) * 100
}

// diskUsedPct returns the percentage of disk space used on the filesystem
// containing the given path (0–100).
func diskUsedPct(path string) float64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0
	}
	total := stat.Blocks * uint64(stat.Bsize) //nolint:unconvert // Bsize is int32 on 32-bit ARM
	free := stat.Bfree * uint64(stat.Bsize)   //nolint:unconvert // same as above
	if total == 0 {
		return 0
	}
	return float64(total-free) / float64(total) * 100
}

// storageUsedBytes returns the total bytes used in a directory by walking
// stat. This is a rough estimate using Statfs — for a more accurate value
// we'd walk the tree, but that's too expensive for a periodic heartbeat.
func storageUsedBytes(path string) int64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0
	}
	total := int64(stat.Blocks) * int64(stat.Bsize)
	free := int64(stat.Bfree) * int64(stat.Bsize)
	return total - free
}
