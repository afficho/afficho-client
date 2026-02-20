package scheduler

import (
	"testing"
	"time"

	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/db"
)

// openTestDB creates a temporary SQLite database with all migrations applied.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// seedContent inserts a content item and returns its ID.
func seedContent(t *testing.T, d *db.DB, id, name, ctype, source string, dur int) {
	t.Helper()
	_, err := d.Exec(
		`INSERT INTO content_items (id, name, type, source, duration_s) VALUES (?, ?, ?, ?, ?)`,
		id, name, ctype, source, dur,
	)
	if err != nil {
		t.Fatalf("seeding content: %v", err)
	}
}

// seedPlaylistItem adds a content item to the default playlist.
func seedPlaylistItem(t *testing.T, d *db.DB, itemID, contentID string, position int) {
	t.Helper()
	_, err := d.Exec(
		`INSERT INTO playlist_items (id, playlist_id, content_id, position)
		 VALUES (?, '00000000-0000-0000-0000-000000000001', ?, ?)`,
		itemID, contentID, position,
	)
	if err != nil {
		t.Fatalf("seeding playlist item: %v", err)
	}
}

func newTestScheduler(t *testing.T, d *db.DB) *Scheduler {
	t.Helper()
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	mgr := content.NewManager(cfg, d)
	return New(d, mgr)
}

func TestCurrentEmptyQueue(t *testing.T) {
	d := openTestDB(t)
	sched := newTestScheduler(t, d)

	// No content, so reloadQueue should yield empty queue.
	if err := sched.reloadQueue(); err != nil {
		t.Fatalf("reloadQueue: %v", err)
	}

	item, ok := sched.Current()
	if ok {
		t.Errorf("expected ok=false for empty queue, got item %+v", item)
	}
}

func TestCurrentWithItems(t *testing.T) {
	d := openTestDB(t)
	seedContent(t, d, "c1", "Item A", "url", "https://a.com", 10)
	seedContent(t, d, "c2", "Item B", "url", "https://b.com", 20)
	seedPlaylistItem(t, d, "pi1", "c1", 0)
	seedPlaylistItem(t, d, "pi2", "c2", 1)

	sched := newTestScheduler(t, d)
	if err := sched.reloadQueue(); err != nil {
		t.Fatalf("reloadQueue: %v", err)
	}

	item, ok := sched.Current()
	if !ok {
		t.Fatal("expected item, got ok=false")
	}
	if item.ID != "c1" {
		t.Errorf("expected c1, got %q", item.ID)
	}
	if item.Name != "Item A" {
		t.Errorf("expected Item A, got %q", item.Name)
	}
	if item.DurationS != 10 {
		t.Errorf("expected duration 10, got %d", item.DurationS)
	}
}

func TestAdvance(t *testing.T) {
	d := openTestDB(t)
	seedContent(t, d, "c1", "A", "url", "https://a.com", 10)
	seedContent(t, d, "c2", "B", "url", "https://b.com", 10)
	seedContent(t, d, "c3", "C", "url", "https://c.com", 10)
	seedPlaylistItem(t, d, "pi1", "c1", 0)
	seedPlaylistItem(t, d, "pi2", "c2", 1)
	seedPlaylistItem(t, d, "pi3", "c3", 2)

	sched := newTestScheduler(t, d)
	if err := sched.reloadQueue(); err != nil {
		t.Fatal(err)
	}

	// Should start at c1.
	item, _ := sched.Current()
	if item.ID != "c1" {
		t.Errorf("expected c1, got %q", item.ID)
	}

	// Advance to c2.
	item, ok := sched.Advance()
	if !ok || item.ID != "c2" {
		t.Errorf("expected c2, got %q (ok=%v)", item.ID, ok)
	}

	// Advance to c3.
	item, _ = sched.Advance()
	if item.ID != "c3" {
		t.Errorf("expected c3, got %q", item.ID)
	}

	// Advance wraps to c1.
	item, _ = sched.Advance()
	if item.ID != "c1" {
		t.Errorf("expected wrap to c1, got %q", item.ID)
	}
}

func TestAdvanceEmptyQueue(t *testing.T) {
	d := openTestDB(t)
	sched := newTestScheduler(t, d)
	if err := sched.reloadQueue(); err != nil {
		t.Fatal(err)
	}

	_, ok := sched.Advance()
	if ok {
		t.Error("expected ok=false for empty queue advance")
	}
}

func TestQueueReturnsSnapshot(t *testing.T) {
	d := openTestDB(t)
	seedContent(t, d, "c1", "A", "url", "https://a.com", 10)
	seedPlaylistItem(t, d, "pi1", "c1", 0)

	sched := newTestScheduler(t, d)
	if err := sched.reloadQueue(); err != nil {
		t.Fatal(err)
	}

	q := sched.Queue()
	if len(q) != 1 {
		t.Fatalf("expected 1 item in queue, got %d", len(q))
	}

	// Mutate the returned slice — should not affect internal state.
	q[0].Name = "MUTATED"
	internal, _ := sched.Current()
	if internal.Name == "MUTATED" {
		t.Error("Queue() returned reference to internal slice, not a copy")
	}
}

func TestTriggerReloadNonBlocking(t *testing.T) {
	d := openTestDB(t)
	sched := newTestScheduler(t, d)

	// Should not deadlock even when called multiple times.
	sched.TriggerReload()
	sched.TriggerReload()
	sched.TriggerReload()
}

func TestOnChangeCalledOnAdvance(t *testing.T) {
	d := openTestDB(t)
	seedContent(t, d, "c1", "A", "url", "https://a.com", 10)
	seedContent(t, d, "c2", "B", "url", "https://b.com", 10)
	seedPlaylistItem(t, d, "pi1", "c1", 0)
	seedPlaylistItem(t, d, "pi2", "c2", 1)

	sched := newTestScheduler(t, d)
	if err := sched.reloadQueue(); err != nil {
		t.Fatal(err)
	}

	called := false
	sched.OnChange = func() { called = true }

	sched.Advance()
	if !called {
		t.Error("expected OnChange to be called on Advance")
	}
}

func TestSecondsUntilNext(t *testing.T) {
	d := openTestDB(t)
	seedContent(t, d, "c1", "A", "url", "https://a.com", 30)
	seedPlaylistItem(t, d, "pi1", "c1", 0)

	sched := newTestScheduler(t, d)
	if err := sched.reloadQueue(); err != nil {
		t.Fatal(err)
	}
	// advancedAt is set during reloadQueue, so SecondsUntilNext should be > 0.
	remaining := sched.SecondsUntilNext()
	if remaining <= 0 || remaining > 30 {
		t.Errorf("expected remaining between 0 and 30, got %f", remaining)
	}
}

func TestSecondsUntilNextEmptyQueue(t *testing.T) {
	d := openTestDB(t)
	sched := newTestScheduler(t, d)
	if err := sched.reloadQueue(); err != nil {
		t.Fatal(err)
	}
	if got := sched.SecondsUntilNext(); got != 0 {
		t.Errorf("expected 0 for empty queue, got %f", got)
	}
}

func TestActivePlaylistIDDefault(t *testing.T) {
	d := openTestDB(t)
	sched := newTestScheduler(t, d)
	if err := sched.reloadQueue(); err != nil {
		t.Fatal(err)
	}
	if id := sched.ActivePlaylistID(); id != "" {
		t.Errorf("expected empty string for default playlist, got %q", id)
	}
}

func TestScheduleEvaluation(t *testing.T) {
	d := openTestDB(t)

	// Create a second playlist.
	_, err := d.Exec(`INSERT INTO playlists (id, name, is_default) VALUES ('pl2', 'Special', 0)`)
	if err != nil {
		t.Fatal(err)
	}

	// Create a schedule that's always active.
	_, err = d.Exec(
		`INSERT INTO schedules (id, playlist_id, cron_expr, priority) VALUES ('s1', 'pl2', '00:00-23:59 everyday', 10)`,
	)
	if err != nil {
		t.Fatal(err)
	}

	sched := newTestScheduler(t, d)
	got := sched.evaluateSchedules(time.Now())
	if got != "pl2" {
		t.Errorf("expected pl2, got %q", got)
	}
}

func TestScheduleEvaluationNoSchedules(t *testing.T) {
	d := openTestDB(t)
	sched := newTestScheduler(t, d)

	got := sched.evaluateSchedules(time.Now())
	if got != "" {
		t.Errorf("expected empty string with no schedules, got %q", got)
	}
}

func TestDurationOverride(t *testing.T) {
	d := openTestDB(t)
	seedContent(t, d, "c1", "A", "url", "https://a.com", 10)

	// Add to playlist with a duration override.
	_, err := d.Exec(
		`INSERT INTO playlist_items (id, playlist_id, content_id, position, duration_override_s)
		 VALUES ('pi1', '00000000-0000-0000-0000-000000000001', 'c1', 0, 30)`,
	)
	if err != nil {
		t.Fatal(err)
	}

	sched := newTestScheduler(t, d)
	if err := sched.reloadQueue(); err != nil {
		t.Fatal(err)
	}

	item, ok := sched.Current()
	if !ok {
		t.Fatal("expected item")
	}
	if item.DurationS != 30 {
		t.Errorf("expected duration override 30, got %d", item.DurationS)
	}
}
