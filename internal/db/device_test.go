package db

import (
	"testing"
)

func TestDeviceIDGeneratedOnFirstCall(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	id, err := d.DeviceID()
	if err != nil {
		t.Fatalf("DeviceID: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty device ID")
	}
	// Should look like a UUID (36 chars with dashes).
	if len(id) != 36 {
		t.Errorf("expected UUID-length string (36), got %d: %q", len(id), id)
	}
}

func TestDeviceIDStableAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	id1, err := d.DeviceID()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	id2, err := d.DeviceID()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if id1 != id2 {
		t.Errorf("device ID changed between calls: %q → %q", id1, id2)
	}
}

func TestDeviceIDStableAcrossReopen(t *testing.T) {
	dir := t.TempDir()

	// First open — generate the ID.
	d1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id1, err := d1.DeviceID()
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	d1.Close()

	// Second open — should return the same ID.
	d2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()

	id2, err := d2.DeviceID()
	if err != nil {
		t.Fatalf("second open: %v", err)
	}

	if id1 != id2 {
		t.Errorf("device ID changed after reopen: %q → %q", id1, id2)
	}
}
