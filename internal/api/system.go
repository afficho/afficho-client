package api

import (
	"bufio"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// handleSystemInfo returns system information including CPU temperature,
// memory usage, disk usage, uptime, and network details.
func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]any{
		"version":    s.version,
		"uptime_s":   time.Since(s.startedAt).Seconds(),
		"cpu_temp_c": readCPUTemp(),
		"memory":     readMemoryInfo(),
		"disk":       readDiskInfo(s.cfg.Storage.DataDir),
		"local_ip":   getLocalIP(),
		"hostname":   getHostname(),
		"arch":       runtime.GOARCH,
		"os":         runtime.GOOS,
	})
}

// handleHealthz is a simple health check endpoint for load balancers and watchdogs.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	respond(w, http.StatusOK, map[string]any{"status": "ok"})
}

// readCPUTemp reads the CPU temperature from the thermal zone (Linux/RPi).
// Returns 0 if not available.
func readCPUTemp() float64 {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	milliC, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return float64(milliC) / 1000.0
}

// memoryInfo contains memory statistics.
type memoryInfo struct {
	TotalMB     int     `json:"total_mb"`
	AvailableMB int     `json:"available_mb"`
	UsedPct     float64 `json:"used_pct"`
}

// readMemoryInfo parses /proc/meminfo for total and available memory.
func readMemoryInfo() memoryInfo {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return memoryInfo{}
	}
	defer f.Close()

	var totalKB, availableKB int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			totalKB = parseMemInfoKB(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			availableKB = parseMemInfoKB(line)
		}
		if totalKB > 0 && availableKB > 0 {
			break
		}
	}

	if totalKB == 0 {
		return memoryInfo{}
	}

	usedKB := totalKB - availableKB
	return memoryInfo{
		TotalMB:     totalKB / 1024,
		AvailableMB: availableKB / 1024,
		UsedPct:     float64(usedKB) / float64(totalKB) * 100,
	}
}

// parseMemInfoKB extracts the kB value from a /proc/meminfo line like "MemTotal:  8000000 kB".
func parseMemInfoKB(line string) int {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	val, _ := strconv.Atoi(fields[1])
	return val
}

// diskInfo contains disk usage statistics.
type diskInfo struct {
	TotalGB float64 `json:"total_gb"`
	FreeGB  float64 `json:"free_gb"`
	UsedPct float64 `json:"used_pct"`
}

// readDiskInfo uses statfs to get disk usage for the given path.
func readDiskInfo(path string) diskInfo {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return diskInfo{}
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	usedBytes := totalBytes - freeBytes

	totalGB := float64(totalBytes) / (1024 * 1024 * 1024)
	freeGB := float64(freeBytes) / (1024 * 1024 * 1024)
	usedPct := 0.0
	if totalBytes > 0 {
		usedPct = float64(usedBytes) / float64(totalBytes) * 100
	}

	return diskInfo{
		TotalGB: float64(int(totalGB*10)) / 10, // round to 1 decimal
		FreeGB:  float64(int(freeGB*10)) / 10,
		UsedPct: float64(int(usedPct*10)) / 10,
	}
}

// getLocalIP returns the first non-loopback IPv4 address.
func getLocalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && ip.To4() != nil {
				return ip.String()
			}
		}
	}
	return ""
}

// getHostname returns the system hostname.
func getHostname() string {
	name, _ := os.Hostname()
	return name
}
