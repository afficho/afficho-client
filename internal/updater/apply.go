package updater

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// applyUpdate downloads the release binary, verifies its checksum, and stages
// it for the next service restart.
func (u *Updater) applyUpdate(release *Release) error {
	suffix := assetSuffix(u.goarm)
	archiveName := ""
	checksumName := "checksums.txt"

	var archiveURL, checksumURL string
	for _, a := range release.Assets {
		if strings.Contains(a.Name, suffix) && strings.HasSuffix(a.Name, ".tar.gz") {
			archiveName = a.Name
			archiveURL = a.BrowserDownloadURL
		}
		if a.Name == checksumName {
			checksumURL = a.BrowserDownloadURL
		}
	}
	if archiveURL == "" {
		return fmt.Errorf("no asset found for architecture %s", suffix)
	}
	if checksumURL == "" {
		return fmt.Errorf("checksums.txt not found in release assets")
	}

	// Staging directory.
	stageDir := filepath.Join(u.cfg.Storage.DataDir, ".update")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("creating staging dir: %w", err)
	}

	// Download and verify checksum.
	expectedHash, err := fetchExpectedChecksum(checksumURL, archiveName)
	if err != nil {
		return fmt.Errorf("fetching checksum: %w", err)
	}

	archivePath := filepath.Join(stageDir, archiveName)
	if err := downloadFile(archiveURL, archivePath); err != nil {
		return fmt.Errorf("downloading archive: %w", err)
	}

	if err := verifyChecksum(archivePath, expectedHash); err != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("checksum verification: %w", err)
	}

	// Extract the binary from the tarball.
	binaryPath := filepath.Join(stageDir, "afficho.new")
	if err := extractBinary(archivePath, binaryPath); err != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("extracting binary: %w", err)
	}
	_ = os.Remove(archivePath)

	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return fmt.Errorf("setting binary permissions: %w", err)
	}

	// Write signal file for the ExecStartPre script to pick up.
	pendingPath := filepath.Join(stageDir, "pending")
	if err := os.WriteFile(pendingPath, []byte(release.TagName+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing pending signal: %w", err)
	}

	slog.Info("update staged", "version", release.TagName, "path", binaryPath)

	// Attempt to restart via systemctl. If that fails (e.g. not running under
	// systemd), log the error — the update will be applied on next manual restart.
	if err := restartService(); err != nil {
		slog.Warn("could not trigger restart, update will apply on next restart", "error", err)
	}

	return nil
}

// assetSuffix returns the archive name suffix for the current architecture.
func assetSuffix(goarm string) string {
	switch runtime.GOARCH {
	case "amd64":
		return "linux-amd64"
	case "arm64":
		return "linux-arm64"
	case "arm":
		if goarm == "" {
			goarm = "7"
		}
		return "linux-armv" + goarm
	default:
		return "linux-" + runtime.GOARCH
	}
}

// fetchExpectedChecksum downloads the checksums file and finds the hash for
// the given archive name.
func fetchExpectedChecksum(checksumURL, archiveName string) (string, error) {
	resp, err := httpClient.Get(checksumURL)
	if err != nil {
		return "", fmt.Errorf("downloading checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums request returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading checksums: %w", err)
	}

	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == archiveName {
			return parts[0], nil
		}
	}

	return "", fmt.Errorf("checksum not found for %s", archiveName)
}

// downloadFile fetches a URL and writes the content to disk.
func downloadFile(url, dest string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Close()
}

// verifyChecksum computes the SHA256 of filePath and compares it to expected.
func verifyChecksum(filePath, expected string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))

	if actual != expected {
		return fmt.Errorf("expected %s, got %s", expected, actual)
	}
	return nil
}

// extractBinary opens a .tar.gz archive and writes the first regular file
// named "afficho" to dest.
func extractBinary(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("opening gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		// The binary is the file named "afficho" inside the archive.
		base := filepath.Base(hdr.Name)
		if hdr.Typeflag == tar.TypeReg && base == "afficho" {
			out, err := os.Create(dest)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			return out.Close()
		}
	}

	return fmt.Errorf("binary 'afficho' not found in archive")
}

// restartService asks systemd to restart the afficho service.
func restartService() error {
	cmd := exec.Command("systemctl", "restart", "afficho")
	return cmd.Run()
}
