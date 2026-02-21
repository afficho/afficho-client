package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Release represents a GitHub release.
type Release struct {
	TagName    string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset represents a downloadable release artifact.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

const (
	releasesURL = "https://api.github.com/repos/afficho/afficho-client/releases"
	userAgent   = "afficho-client-updater"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// fetchLatestRelease queries the GitHub Releases API and returns the newest
// release matching the channel. Returns nil, nil when no suitable release is
// found. Handles rate limiting gracefully.
func fetchLatestRelease(channel string) (*Release, error) {
	url := releasesURL + "/latest"
	if channel == "pre-release" {
		// The /latest endpoint skips pre-releases, so we list all and pick
		// the first one (which is the most recent).
		url = releasesURL + "?per_page=1"
	}

	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching releases: %w", err)
	}
	defer resp.Body.Close()

	// Handle rate limiting.
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		remaining := resp.Header.Get("X-RateLimit-Remaining")
		if remaining == "0" {
			return nil, fmt.Errorf("github rate limit exceeded, retry after %s", resp.Header.Get("X-RateLimit-Reset"))
		}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %d", resp.StatusCode)
	}

	if channel == "pre-release" {
		var releases []Release
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			return nil, fmt.Errorf("decoding releases list: %w", err)
		}
		if len(releases) == 0 {
			return nil, nil
		}
		return &releases[0], nil
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	return &release, nil
}

// normaliseVersion strips a leading "v" prefix for comparison.
func normaliseVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

// isNewer returns true when latest is a newer semantic version than current.
// Falls back to string comparison for non-semver tags.
func isNewer(current, latest string) bool {
	if current == "dev" || current == "" {
		return false // never auto-update a dev build
	}
	if current == latest {
		return false
	}

	curParts := parseSemver(current)
	latParts := parseSemver(latest)

	if curParts == nil || latParts == nil {
		return latest > current // fallback lexicographic
	}

	for i := 0; i < 3; i++ {
		if latParts[i] > curParts[i] {
			return true
		}
		if latParts[i] < curParts[i] {
			return false
		}
	}
	return false
}

// parseSemver extracts [major, minor, patch] from a version string.
// Returns nil if the string is not valid semver.
func parseSemver(v string) []int {
	// Strip pre-release suffix (e.g. "1.2.3-rc1" → "1.2.3").
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		v = v[:idx]
	}

	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	return nums
}
