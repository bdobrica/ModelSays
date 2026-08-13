package db

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLatestSchemaVersionMatchesMigrations(t *testing.T) {
	paths, err := filepath.Glob("../../migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	var highest int64
	for _, path := range paths {
		prefix := strings.SplitN(filepath.Base(path), "_", 2)[0]
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Fatalf("parse migration %q: %v", path, err)
		}
		if version > highest {
			highest = version
		}
	}
	if highest != LatestSchemaVersion {
		t.Fatalf("LatestSchemaVersion = %d, highest migration = %d", LatestSchemaVersion, highest)
	}
}
