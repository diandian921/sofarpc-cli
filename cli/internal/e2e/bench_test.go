//go:build e2e

package e2e

import (
	"os"
	"testing"
	"time"

	"github.com/sofarpc/cli/internal/launcher"
	"github.com/sofarpc/cli/internal/protocol"
)

// TestBenchColdVsWarm is a latency report, not a correctness test — it will almost never
// fail. It prints three numbers the designers actually care about:
//
//   - cold: time from "no daemon" to first response (dominated by JVM boot)
//   - warm first: time for the first call on an already-running daemon (reuse path)
//   - warm p50: median time for N subsequent calls (measures steady-state overhead)
//
// Run with: go test -tags e2e -v -run TestBenchColdVsWarm ./internal/e2e/...
func TestBenchColdVsWarm(t *testing.T) {
	jar := resolveJar(t)
	home := tempHome(t)
	defer os.RemoveAll(home)

	cfg := newConfig(t, jar)
	cfg.SpawnBudget = 45 * time.Second

	coldStart := time.Now()
	conn, err := launcher.Connect(cfg)
	if err != nil {
		t.Fatalf("cold connect: %v", err)
	}
	cold := time.Since(coldStart)
	defer shutdown(t, conn.Client)

	warmFirstStart := time.Now()
	warmConn, err := launcher.Connect(cfg)
	if err != nil {
		t.Fatalf("warm connect: %v", err)
	}
	warmFirst := time.Since(warmFirstStart)

	const samples = 20
	durations := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		req, err := protocol.NewRequest(protocol.OpHealth, struct{}{})
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		start := time.Now()
		resp, err := warmConn.Client.Call(req)
		elapsed := time.Since(start)
		if err != nil || !resp.OK {
			t.Fatalf("health call %d: err=%v resp=%+v", i, err, resp)
		}
		durations = append(durations, elapsed)
	}

	t.Logf("cold(connect+health) = %s", cold)
	t.Logf("warm first connect = %s", warmFirst)
	t.Logf("warm health p50 = %s, min = %s, max = %s", median(durations), durations[argmin(durations)], durations[argmax(durations)])
}

func median(xs []time.Duration) time.Duration {
	sorted := make([]time.Duration, len(xs))
	copy(sorted, xs)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[len(sorted)/2]
}

func argmin(xs []time.Duration) int {
	idx := 0
	for i, v := range xs {
		if v < xs[idx] {
			idx = i
		}
	}
	return idx
}

func argmax(xs []time.Duration) int {
	idx := 0
	for i, v := range xs {
		if v > xs[idx] {
			idx = i
		}
	}
	return idx
}
