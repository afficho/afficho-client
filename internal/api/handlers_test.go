package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/db"
	"github.com/afficho/afficho-client/internal/scheduler"
)

// testAPIServer creates a fully wired server with an in-memory DB for integration tests.
// Auth is disabled (empty password) unless explicitly set.
func testAPIServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Storage.DataDir = dir
	cfg.Admin.Password = "" // Disable auth for tests.

	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	mgr := content.NewManager(cfg, database)
	if err := mgr.Init(); err != nil {
		t.Fatal(err)
	}

	sched := scheduler.New(database, mgr)
	// Don't call sched.Run — just reload queue manually when needed.

	return NewServer(cfg, database, mgr, sched, "test-version")
}

func doRequest(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, http.NoBody)
	}
	srv.mux.ServeHTTP(w, r)
	return w
}

// --- Status & Health ---

func TestHealthz(t *testing.T) {
	srv := testAPIServer(t)
	w := doRequest(srv, "GET", "/healthz", "")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestStatus(t *testing.T) {
	srv := testAPIServer(t)
	w := doRequest(srv, "GET", "/api/v1/status", "")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["version"] != "test-version" {
		t.Errorf("expected version test-version, got %v", resp["version"])
	}
}

// --- Content CRUD ---

func TestContentCRUD(t *testing.T) {
	srv := testAPIServer(t)

	// Create.
	createBody := `{"name":"My Page","type":"url","url":"https://example.com","duration_s":15}`
	w := doRequest(srv, "POST", "/api/v1/content", createBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatal("expected id in create response")
	}

	// List.
	w = doRequest(srv, "GET", "/api/v1/content", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 item, got %d", len(list))
	}

	// Get single.
	w = doRequest(srv, "GET", "/api/v1/content/"+id, "")
	if w.Code != http.StatusOK {
		t.Errorf("get: expected 200, got %d", w.Code)
	}

	// Update.
	updateBody := `{"name":"Updated Page"}`
	w = doRequest(srv, "PATCH", "/api/v1/content/"+id, updateBody)
	if w.Code != http.StatusOK {
		t.Errorf("update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify update.
	w = doRequest(srv, "GET", "/api/v1/content/"+id, "")
	var updated map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated["name"] != "Updated Page" {
		t.Errorf("expected updated name, got %v", updated["name"])
	}

	// Delete.
	w = doRequest(srv, "DELETE", "/api/v1/content/"+id, "")
	if w.Code != http.StatusNoContent {
		t.Errorf("delete: expected 204, got %d", w.Code)
	}

	// Verify deletion.
	w = doRequest(srv, "GET", "/api/v1/content/"+id, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("after delete: expected 404, got %d", w.Code)
	}
}

// --- Playlist CRUD ---

func TestPlaylistCRUD(t *testing.T) {
	srv := testAPIServer(t)

	// List — should have the seeded default playlist.
	w := doRequest(srv, "GET", "/api/v1/playlists", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	var playlists []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &playlists); err != nil {
		t.Fatal(err)
	}
	if len(playlists) < 1 {
		t.Fatal("expected at least the default playlist")
	}

	// Create a new playlist.
	w = doRequest(srv, "POST", "/api/v1/playlists", `{"name":"Test Playlist"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var createdPL map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &createdPL); err != nil {
		t.Fatal(err)
	}
	plID := createdPL["id"].(string)

	// Get.
	w = doRequest(srv, "GET", "/api/v1/playlists/"+plID, "")
	if w.Code != http.StatusOK {
		t.Errorf("get: expected 200, got %d", w.Code)
	}

	// Delete the non-default playlist (should succeed since it's not the default).
	w = doRequest(srv, "DELETE", "/api/v1/playlists/"+plID, "")
	if w.Code != http.StatusNoContent {
		t.Errorf("delete: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteLastPlaylistPrevented(t *testing.T) {
	srv := testAPIServer(t)

	// Try to delete the default playlist — should fail.
	w := doRequest(srv, "DELETE", "/api/v1/playlists/00000000-0000-0000-0000-000000000001", "")
	if w.Code == http.StatusNoContent {
		t.Error("expected error when deleting the last playlist")
	}
}

// --- Display ---

func TestDisplayCurrentEmpty(t *testing.T) {
	srv := testAPIServer(t)
	w := doRequest(srv, "GET", "/display/current", "")
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for empty queue, got %d", w.Code)
	}
}

func TestDisplayPage(t *testing.T) {
	srv := testAPIServer(t)
	w := doRequest(srv, "GET", "/display", "")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
}

// --- Storage ---

func TestStorageStatus(t *testing.T) {
	srv := testAPIServer(t)
	w := doRequest(srv, "GET", "/api/v1/storage", "")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- Security Headers ---

func TestSecurityHeaders(t *testing.T) {
	srv := testAPIServer(t)
	w := doRequest(srv, "GET", "/healthz", "")

	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("expected X-Content-Type-Options: nosniff, got %q", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("expected X-Frame-Options: SAMEORIGIN, got %q", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("expected Referrer-Policy, got %q", got)
	}
}

// --- Cloud Status ---

// mockCloudStatus implements CloudStatus for testing.
type mockCloudStatus struct {
	connected       bool
	lastConnectedAt time.Time
	deviceID        string
}

func (m *mockCloudStatus) Connected() bool          { return m.connected }
func (m *mockCloudStatus) LastConnectedAt() time.Time { return m.lastConnectedAt }
func (m *mockCloudStatus) DeviceID() string          { return m.deviceID }

// mockPendingCounter implements PendingCounter for testing.
type mockPendingCounter struct {
	count int
}

func (m *mockPendingCounter) PendingCount() int { return m.count }

func TestCloudStatusDisabled(t *testing.T) {
	srv := testAPIServer(t)
	srv.cfg.Cloud.Enabled = false

	w := doRequest(srv, "GET", "/api/v1/cloud/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", resp["enabled"])
	}
	if resp["connected"] != false {
		t.Errorf("expected connected=false, got %v", resp["connected"])
	}
}

func TestCloudStatusConnected(t *testing.T) {
	srv := testAPIServer(t)
	srv.cfg.Cloud.Enabled = true

	now := time.Now().UTC().Truncate(time.Second)
	srv.cloudStatus = &mockCloudStatus{
		connected:       true,
		lastConnectedAt: now,
		deviceID:        "device-abc",
	}
	srv.pendingCounter = &mockPendingCounter{count: 5}

	w := doRequest(srv, "GET", "/api/v1/cloud/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if resp["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", resp["enabled"])
	}
	if resp["connected"] != true {
		t.Errorf("expected connected=true, got %v", resp["connected"])
	}
	if resp["device_id"] != "device-abc" {
		t.Errorf("expected device_id=device-abc, got %v", resp["device_id"])
	}
	if resp["last_connected_at"] == nil {
		t.Error("expected last_connected_at to be set")
	}
	if pending, ok := resp["pending_proof_of_play"].(float64); !ok || int(pending) != 5 {
		t.Errorf("expected pending_proof_of_play=5, got %v", resp["pending_proof_of_play"])
	}
}

func TestCloudStatusNoPendingCounter(t *testing.T) {
	srv := testAPIServer(t)
	srv.cfg.Cloud.Enabled = true
	srv.cloudStatus = &mockCloudStatus{connected: false, deviceID: "dev-1"}

	w := doRequest(srv, "GET", "/api/v1/cloud/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if _, ok := resp["pending_proof_of_play"]; ok {
		t.Error("expected no pending_proof_of_play when counter is nil")
	}
}

// --- Auth on write routes ---

func TestAuthRequiredWhenPasswordSet(t *testing.T) {
	srv := testAPIServer(t)
	srv.cfg.Admin.Password = "secret"

	// POST to content without auth should be rejected.
	w := doRequest(srv, "POST", "/api/v1/content", `{"name":"test","type":"url","url":"https://a.com"}`)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}

	// GET /api/v1/status should still work (unauthenticated endpoint).
	w = doRequest(srv, "GET", "/api/v1/status", "")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for status, got %d", w.Code)
	}
}
