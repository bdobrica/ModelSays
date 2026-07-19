package ops

import (
	"bytes"
	"strings"
	"testing"
)

func TestMetricsExposeOnlyCallerSelectedBoundedLabels(t *testing.T) {
	metrics := NewMetrics()
	metrics.Inc("modelsays_http_requests_total", "method", "GET", "route", "room", "status_class", "2xx")
	metrics.Observe("modelsays_http_request_duration_seconds", .01, []float64{.005, .01}, "route", "room")
	var output bytes.Buffer
	metrics.WritePrometheus(&output)
	text := output.String()
	for _, expected := range []string{
		`modelsays_http_requests_total{method="GET",route="room",status_class="2xx"} 1`,
		`modelsays_http_request_duration_seconds_bucket{route="room",le="0.01"} 1`,
		`modelsays_http_request_duration_seconds_count{route="room"} 1`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, text)
		}
	}
	for _, secret := range []string{"ROOMAA", "player-token", "192.0.2.1"} {
		if strings.Contains(text, secret) {
			t.Fatalf("metrics leaked %q", secret)
		}
	}
}
