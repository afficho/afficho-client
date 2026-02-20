package scheduler

import (
	"testing"

	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/db"
)

func BenchmarkSchedulerCurrent(b *testing.B) {
	dir := b.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer d.Close()

	// Seed 10 items.
	for i := range 10 {
		id := "c" + string(rune('0'+i))
		_, _ = d.Exec(
			`INSERT INTO content_items (id, name, type, source, duration_s) VALUES (?, ?, 'url', ?, 10)`,
			id, "Item "+id, "https://"+id+".com",
		)
		_, _ = d.Exec(
			`INSERT INTO playlist_items (id, playlist_id, content_id, position)
			 VALUES (?, '00000000-0000-0000-0000-000000000001', ?, ?)`,
			"pi"+id, id, i,
		)
	}

	cfg := config.Default()
	cfg.Storage.DataDir = dir
	mgr := content.NewManager(cfg, d)
	sched := New(d, mgr)
	if err := sched.reloadQueue(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sched.Current()
		}
	})
}
