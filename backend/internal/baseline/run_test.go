package baseline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPercentileUsesNearestRank(t *testing.T) {
	values := []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond}
	if got := percentile(values, 0.50); got != 2*time.Millisecond {
		t.Fatalf("p50 = %s, want 2ms", got)
	}
	if got := percentile(values, 0.95); got != 4*time.Millisecond {
		t.Fatalf("p95 = %s, want 4ms", got)
	}
}

func TestPostgresWorkloads(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL baseline workloads")
	}
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("baseline"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), Options{
		DatabaseURL:  databaseURL,
		ClientDist:   dist,
		PlayerCounts: []int{3, 8, 12},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != 1 || len(report.Workloads) != 3 {
		t.Fatalf("report header = version %d, workloads %d", report.SchemaVersion, len(report.Workloads))
	}
	for _, workload := range report.Workloads {
		if workload.HTTPRequests != 5*workload.Players+2 {
			t.Errorf("%d players: requests = %d", workload.Players, workload.HTTPRequests)
		}
		if workload.PollRequests != 3*workload.Players {
			t.Errorf("%d players: polls = %d", workload.Players, workload.PollRequests)
		}
		if workload.DatabaseQueries <= 0 || workload.ResponseBytes <= 0 {
			t.Errorf("%d players: missing database or response measurements: %#v", workload.Players, workload)
		}
		if workload.MutationToVisibleStateLatencyMS <= 0 {
			t.Errorf("%d players: missing visibility latency", workload.Players)
		}
	}
}

func TestMeasureBundle(t *testing.T) {
	root := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "1234")
	write("app.js", "123456")
	got, err := measureBundle(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Files != 2 || got.Bytes != 10 {
		t.Fatalf("bundle = %#v, want 2 files and 10 bytes", got)
	}
}
