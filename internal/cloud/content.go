package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	types "github.com/afficho/afficho-types"

	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/db"
)

// ContentSyncer handles sync_content messages from the cloud.
type ContentSyncer struct {
	db      *db.DB
	content *content.Manager
	conn    *Connector
}

// NewContentSyncer creates a ContentSyncer and registers it as a handler
// on the connector for TypeSyncContent messages.
func NewContentSyncer(conn *Connector, database *db.DB, mgr *content.Manager) *ContentSyncer {
	cs := &ContentSyncer{
		db:      database,
		content: mgr,
		conn:    conn,
	}
	conn.Handle(types.TypeSyncContent, cs.handle)
	return cs
}

// handle is the MessageHandler for TypeSyncContent messages.
func (cs *ContentSyncer) handle(payload json.RawMessage) {
	var items []types.ContentSyncItem
	if err := json.Unmarshal(payload, &items); err != nil {
		slog.Error("cloud: invalid sync_content payload", "error", err)
		return
	}

	slog.Info("cloud: content sync received", "items", len(items))

	if err := cs.sync(context.Background(), items); err != nil {
		slog.Error("cloud: content sync failed", "error", err)
		return
	}

	// Send ack.
	cs.sendAck()
}

// sync processes the content manifest from the cloud.
func (cs *ContentSyncer) sync(ctx context.Context, items []types.ContentSyncItem) error {
	// Build a set of cloud item IDs for deletion detection.
	cloudIDs := make(map[string]struct{}, len(items))
	for _, item := range items {
		cloudIDs[item.ID] = struct{}{}
	}

	// Process each item: skip if checksum matches, download otherwise.
	for _, item := range items {
		if err := cs.syncItem(ctx, item); err != nil {
			slog.Error("cloud: sync item failed", "id", item.ID, "name", item.Name, "error", err)
			// Continue with other items rather than aborting the whole sync.
		}
	}

	// Delete cloud-origin items that are no longer in the manifest.
	if err := cs.deleteStale(cloudIDs); err != nil {
		slog.Error("cloud: deleting stale content", "error", err)
	}

	return nil
}

// syncItem handles a single content item from the manifest.
func (cs *ContentSyncer) syncItem(ctx context.Context, item types.ContentSyncItem) error {
	// Check if already cached with matching checksum.
	var existingChecksum string
	err := cs.db.QueryRow(
		`SELECT checksum FROM content_items WHERE id = ?`, item.ID,
	).Scan(&existingChecksum)

	if err == nil && existingChecksum == item.Checksum && item.Checksum != "" {
		slog.Debug("cloud: content already cached", "id", item.ID)
		return nil
	}

	// Download media types; URL and HTML don't need downloading.
	var source string
	var sizeBytes int64
	var checksum string

	switch item.Type {
	case "image", "video":
		localPath, size, dlChecksum, dlErr := cs.downloadAndVerify(ctx, item)
		if dlErr != nil {
			return dlErr
		}
		source = "/media/" + filepath.Base(localPath)
		sizeBytes = size
		checksum = dlChecksum
	default:
		// URL, HTML — store the source directly, no download needed.
		source = item.Source
		sizeBytes = item.SizeBytes
		checksum = item.Checksum
	}

	// Upsert into content_items.
	popups := 0
	if item.AllowPopups {
		popups = 1
	}

	_, err = cs.db.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, size_bytes, allow_popups, checksum, origin)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'cloud')
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			type = excluded.type,
			source = excluded.source,
			duration_s = excluded.duration_s,
			size_bytes = excluded.size_bytes,
			allow_popups = excluded.allow_popups,
			checksum = excluded.checksum,
			origin = 'cloud',
			updated_at = CURRENT_TIMESTAMP`,
		item.ID, item.Name, item.Type, source, item.DurationS, sizeBytes, popups, checksum,
	)
	if err != nil {
		return fmt.Errorf("upserting content item %s: %w", item.ID, err)
	}

	slog.Info("cloud: content synced", "id", item.ID, "name", item.Name)
	return nil
}

// downloadAndVerify downloads a media file from the signed URL, verifies the
// SHA-256 checksum, and stores it locally. Returns the local path, size, and
// computed checksum.
func (cs *ContentSyncer) downloadAndVerify(ctx context.Context, item types.ContentSyncItem) (localPath string, size int64, checksum string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.Source, http.NoBody)
	if err != nil {
		return "", 0, "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, "", fmt.Errorf("downloading %q: %w", item.Source, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, "", fmt.Errorf("unexpected HTTP %d for %q", resp.StatusCode, item.Source)
	}

	// Determine file extension from the source URL.
	ext := filepath.Ext(item.Source)
	if ext == "" || len(ext) > 5 {
		// Fallback based on type.
		switch item.Type {
		case "image":
			ext = ".jpg"
		case "video":
			ext = ".mp4"
		}
	}

	localPath = cs.content.LocalPath(item.ID, ext)

	f, err := os.Create(localPath)
	if err != nil {
		return "", 0, "", fmt.Errorf("creating file: %w", err)
	}

	hasher := sha256.New()
	size, err = io.Copy(f, io.TeeReader(resp.Body, hasher))
	_ = f.Close()
	if err != nil {
		_ = os.Remove(localPath)
		return "", 0, "", fmt.Errorf("writing file: %w", err)
	}

	checksum = hex.EncodeToString(hasher.Sum(nil))

	// Verify checksum if the cloud provided one.
	if item.Checksum != "" && checksum != item.Checksum {
		_ = os.Remove(localPath)
		return "", 0, "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", item.ID, item.Checksum, checksum)
	}

	return localPath, size, checksum, nil
}

// deleteStale removes cloud-origin content items that are no longer in the manifest.
func (cs *ContentSyncer) deleteStale(cloudIDs map[string]struct{}) error {
	rows, err := cs.db.Query(`SELECT id, source FROM content_items WHERE origin = 'cloud'`)
	if err != nil {
		return fmt.Errorf("listing cloud content: %w", err)
	}

	// Collect stale items first, then close rows before executing deletes.
	// SQLite has MaxOpenConns=1 so we cannot hold rows open and Exec simultaneously.
	type staleItem struct{ id, source string }
	var stale []staleItem

	for rows.Next() {
		var id, source string
		if err := rows.Scan(&id, &source); err != nil {
			rows.Close()
			return fmt.Errorf("scanning cloud content row: %w", err)
		}

		if _, keep := cloudIDs[id]; keep {
			continue
		}
		stale = append(stale, staleItem{id, source})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	// Now delete with no open rows iterator.
	for _, item := range stale {
		slog.Info("cloud: removing stale content", "id", item.id)

		if cs.content.IsLocalMedia(item.source) {
			_ = cs.content.Delete(item.source)
		}

		if _, err := cs.db.Exec(`DELETE FROM content_items WHERE id = ?`, item.id); err != nil {
			slog.Error("cloud: deleting stale content item", "id", item.id, "error", err)
		}
	}

	return nil
}

// sendAck sends a sync_ack message for content sync.
func (cs *ContentSyncer) sendAck() {
	ack := types.SyncAck{SyncType: "content"}
	payload, err := json.Marshal(ack)
	if err != nil {
		slog.Error("cloud: marshal content sync ack", "error", err)
		return
	}

	if err := cs.conn.SendMessage(context.Background(), types.WSMessage{
		Type:    types.TypeSyncAck,
		Payload: payload,
	}); err != nil {
		slog.Warn("cloud: failed to send content sync ack", "error", err)
	}
}
