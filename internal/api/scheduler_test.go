package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/scheduler"
)

func newAdmissionBroker(t *testing.T, capacity int) *scheduler.Broker {
	t.Helper()
	b, err := scheduler.New(scheduler.Config{Capacity: capacity})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func configureAdmission(t *testing.T, c *Client, b *scheduler.Broker, target, group string) {
	t.Helper()
	if err := c.SetScheduler(SchedulerConfig{Broker: b, Target: target, Group: group}); err != nil {
		t.Fatal(err)
	}
}

// A02: two independent clients sharing C=1 cannot dispatch two physical HTTP
// attempts. The second attempt times out in admission and never reaches the
// loopback server while the first response is held open.
func TestSchedulerCapsNativePhysicalAttempts(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	b := newAdmissionBroker(t, 1)
	first, _ := NewClient(server.URL, "", "test", time.Nanosecond)
	second, _ := NewClient(server.URL, "", "test", time.Nanosecond)
	configureAdmission(t, first, b, "one", "shared")
	configureAdmission(t, second, b, "two", "shared")
	firstDone := make(chan error, 1)
	go func() { _, err := first.DoJSON(context.Background(), http.MethodGet, "models", nil); firstDone <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := second.DoJSON(ctx, http.MethodGet, "models", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second error=%v, want admission deadline", err)
	}
	if got := len(entered); got != 0 {
		t.Fatalf("second request reached server while C=1 lease held")
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if got := b.Snapshot().InFlight; got != 0 {
		t.Fatalf("in flight after native response=%d, want 0", got)
	}
}

// A03/A04: a default group is the normalized origin, and a cooldown in that
// group does not delay a ready request in an explicitly different group.
func TestSchedulerGroupsDefaultOriginAndIsolation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	b := newAdmissionBroker(t, 2)
	defaultClient, _ := NewClient(server.URL, "", "test", time.Nanosecond)
	configureAdmission(t, defaultClient, b, "default", "")
	if _, err := defaultClient.DoJSON(context.Background(), http.MethodGet, "models", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Snapshot().Groups[normalizedOrigin(defaultClient.baseURL)]; !ok {
		t.Fatalf("missing normalized origin group: %#v", b.Snapshot().Groups)
	}
	if err := b.ApplyCooldown("blocked", time.Now().Add(time.Hour), scheduler.WaitRetryAfter); err != nil {
		t.Fatal(err)
	}
	ready, _ := NewClient(server.URL, "", "test", time.Nanosecond)
	configureAdmission(t, ready, b, "ready", "ready")
	if _, err := ready.DoJSON(context.Background(), http.MethodGet, "models", nil); err != nil {
		t.Fatalf("ready group delayed by another group's cooldown: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d, want 2", calls.Load())
	}
}

// A05/A07: a pacing/cooldown wait has no broker slot, and an SDK lease stays
// live until its response body is closed, then releases exactly once.
func TestSchedulerSDKBodyCloseAndWaitingDoesNotUseSlot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	b := newAdmissionBroker(t, 1)
	if err := b.ApplyCooldown("waiting", time.Now().Add(time.Hour), scheduler.WaitRetryAfter); err != nil {
		t.Fatal(err)
	}
	waiting, _ := NewClient(server.URL, "", "test", time.Nanosecond)
	configureAdmission(t, waiting, b, "waiting-target", "waiting")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := waiting.DoJSON(ctx, http.MethodGet, "models", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting request error=%v, want deadline", err)
	}
	if got := b.Snapshot().InFlight; got != 0 {
		t.Fatalf("waiting group consumed slot: %d", got)
	}
	client, _ := NewClient(server.URL, "", "test", time.Nanosecond)
	configureAdmission(t, client, b, "sdk-target", "ready")
	req, err := http.NewRequest(http.MethodGet, server.URL+"/sdk", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.PacedHTTPClient("model").Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Snapshot().InFlight; got != 1 {
		t.Fatalf("SDK headers released lease early: %d", got)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if got := b.Snapshot().InFlight; got != 0 {
		t.Fatalf("SDK close did not release exactly once: %d", got)
	}
}

func TestSchedulerConfigRejectsUnsafeTarget(t *testing.T) {
	c, err := NewClient("https://provider.example", "", "test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetScheduler(SchedulerConfig{Broker: newAdmissionBroker(t, 1), Target: "https://provider.example"}); err == nil {
		t.Fatal("URL target accepted")
	}
}
