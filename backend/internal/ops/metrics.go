package ops

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// Metrics is a deliberately small, dependency-free Prometheus registry. Every
// label value is selected by server code; user, room, token, and IP values must
// never be passed here.
type Metrics struct {
	mu         sync.RWMutex
	counters   map[string]uint64
	histograms map[string]*histogram
	gauges     map[string]float64
}

type histogram struct {
	buckets []float64
	counts  []uint64
	count   uint64
	sum     float64
}

func NewMetrics() *Metrics {
	return &Metrics{
		counters:   make(map[string]uint64),
		histograms: make(map[string]*histogram),
		gauges:     make(map[string]float64),
	}
}

func labelKey(name string, labels ...string) string {
	if len(labels)%2 != 0 {
		panic("metrics labels must be key/value pairs")
	}
	var builder strings.Builder
	builder.WriteString(name)
	if len(labels) == 0 {
		return builder.String()
	}
	builder.WriteByte('{')
	for index := 0; index < len(labels); index += 2 {
		if index > 0 {
			builder.WriteByte(',')
		}
		fmt.Fprintf(&builder, `%s=%q`, labels[index], labels[index+1])
	}
	builder.WriteByte('}')
	return builder.String()
}

func (metrics *Metrics) Inc(name string, labels ...string) {
	metrics.Add(name, 1, labels...)
}

func (metrics *Metrics) Add(name string, value uint64, labels ...string) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.counters[labelKey(name, labels...)] += value
}

func (metrics *Metrics) Set(name string, value float64, labels ...string) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.gauges[labelKey(name, labels...)] = value
}

func (metrics *Metrics) Observe(name string, value float64, buckets []float64, labels ...string) {
	key := labelKey(name, labels...)
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	current := metrics.histograms[key]
	if current == nil {
		current = &histogram{buckets: append([]float64(nil), buckets...), counts: make([]uint64, len(buckets))}
		metrics.histograms[key] = current
	}
	current.count++
	current.sum += value
	for index, upper := range current.buckets {
		if value <= upper {
			current.counts[index]++
		}
	}
}

func (metrics *Metrics) WritePrometheus(writer io.Writer) {
	metrics.mu.RLock()
	defer metrics.mu.RUnlock()
	lines := make([]string, 0, len(metrics.counters)+len(metrics.gauges)+len(metrics.histograms)*4)
	for key, value := range metrics.counters {
		lines = append(lines, fmt.Sprintf("%s %d", key, value))
	}
	for key, value := range metrics.gauges {
		lines = append(lines, fmt.Sprintf("%s %g", key, value))
	}
	for key, current := range metrics.histograms {
		base, labels := splitKey(key)
		for index, upper := range current.buckets {
			lines = append(lines, fmt.Sprintf("%s_bucket%s %d", base, addLabel(labels, "le", fmt.Sprintf("%g", upper)), current.counts[index]))
		}
		lines = append(lines, fmt.Sprintf("%s_bucket%s %d", base, addLabel(labels, "le", "+Inf"), current.count))
		lines = append(lines, fmt.Sprintf("%s_sum%s %g", base, labels, current.sum))
		lines = append(lines, fmt.Sprintf("%s_count%s %d", base, labels, current.count))
	}
	sort.Strings(lines)
	for _, line := range lines {
		_, _ = fmt.Fprintln(writer, line)
	}
}

func splitKey(key string) (string, string) {
	if index := strings.IndexByte(key, '{'); index >= 0 {
		return key[:index], key[index:]
	}
	return key, ""
}

func addLabel(labels, name, value string) string {
	item := fmt.Sprintf(`%s=%q`, name, value)
	if labels == "" {
		return "{" + item + "}"
	}
	return strings.TrimSuffix(labels, "}") + "," + item + "}"
}

var HTTPDurationBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5}

func Seconds(duration time.Duration) float64 { return duration.Seconds() }
