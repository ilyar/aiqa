package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
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
	defaultGroup := normalizedOrigin(defaultClient.baseURL)
	if strings.Contains(defaultGroup, defaultClient.baseURL.Host) || strings.Contains(defaultGroup, "://") {
		t.Fatalf("default group leaks origin: %q", defaultGroup)
	}
	if _, ok := b.Snapshot().Groups[defaultGroup]; !ok {
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

func TestNormalizedOriginUsesEffectiveDefaultPort(t *testing.T) {
	for _, test := range []struct{ left, right string }{
		{"https://EXAMPLE.test", "https://example.test:443"},
		{"http://EXAMPLE.test", "http://example.test:80"},
		{"https://[2001:db8::1]", "https://[2001:db8::1]:443"},
	} {
		left, err := url.Parse(test.left)
		if err != nil {
			t.Fatal(err)
		}
		right, err := url.Parse(test.right)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := normalizedOrigin(right), normalizedOrigin(left); got != want {
			t.Fatalf("origin %q=%q, want %q", test.right, got, want)
		}
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

// A09: artifact creation is reserved in the same target/model bucket across
// native and SDK clients. Commit frees the short admission lease but retains
// the consumed artifact slot, so a possibly-dispatched timeout cannot obtain a
// free retry. Cancellation before dispatch is the only refund path.
func TestReserveArtifactSharesTargetModelBucketAndCommits(t *testing.T) {
	broker, err := scheduler.New(scheduler.Config{Capacity: 2, ArtifactBudget: 2})
	if err != nil {
		t.Fatal(err)
	}
	native, err := NewClient("https://provider.example", "", "test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := NewClient("https://provider.example", "", "test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	configureAdmission(t, native, broker, "target-a", "provider-a")
	configureAdmission(t, sdk, broker, "target-a", "provider-a")

	first, err := native.ReserveArtifact(context.Background(), "image-model")
	if err != nil {
		t.Fatal(err)
	}
	if got := broker.Snapshot(); got.InFlight != 1 || artifactTotal(got.Artifacts) != 1 {
		t.Fatalf("first reservation snapshot=%+v", got)
	}
	// This is the pre-dispatch boundary. A response timeout after this point is
	// intentionally not allowed to refund the artifact.
	first.Commit()
	if got := broker.Snapshot(); got.InFlight != 0 || artifactTotal(got.Artifacts) != 1 {
		t.Fatalf("committed reservation snapshot=%+v", got)
	}
	first.Commit()
	first.CancelBeforeDispatch()
	if got := artifactTotal(broker.Snapshot().Artifacts); got != 1 {
		t.Fatalf("post-commit cancellation refunded possible creation: %d", got)
	}

	second, err := sdk.ReserveArtifact(context.Background(), "image-model")
	if err != nil {
		t.Fatal(err)
	}
	second.Commit()
	if got := artifactTotal(broker.Snapshot().Artifacts); got != 2 {
		t.Fatalf("shared native/SDK reservations=%d, want 2", got)
	}

	if _, err := native.ReserveArtifact(context.Background(), "image-model"); !errors.Is(err, scheduler.ErrArtifactBudgetExhausted) {
		t.Fatalf("third reservation error=%v, want terminal artifact budget error", err)
	}
	if got := artifactTotal(broker.Snapshot().Artifacts); got != 2 {
		t.Fatalf("cancelled retry refunded possible creation: %d", got)
	}
}

func artifactTotal(items map[string]int) int {
	total := 0
	for _, item := range items {
		total += item
	}
	return total
}

func TestReserveArtifactRefundsOnlyBeforeDispatchAndIsNoopWithoutScheduler(t *testing.T) {
	broker, err := scheduler.New(scheduler.Config{Capacity: 1, ArtifactBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient("https://provider.example", "", "test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	configureAdmission(t, client, broker, "target-a", "provider-a")
	reservation, err := client.ReserveArtifact(context.Background(), "image-model")
	if err != nil {
		t.Fatal(err)
	}
	reservation.CancelBeforeDispatch()
	reservation.CancelBeforeDispatch()
	if got := broker.Snapshot(); got.InFlight != 0 || len(got.Artifacts) != 0 {
		t.Fatalf("pre-dispatch cancellation leaked reservation: %+v", got)
	}
	again, err := client.ReserveArtifact(context.Background(), "image-model")
	if err != nil {
		t.Fatalf("refunded reservation was not reusable: %v", err)
	}
	again.CancelBeforeDispatch()

	plain, err := NewClient("https://provider.example", "", "test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	noScheduler, err := plain.ReserveArtifact(context.Background(), "image-model")
	if err != nil {
		t.Fatalf("unscheduled reservation=%v", err)
	}
	noScheduler.Commit()
	noScheduler.CancelBeforeDispatch()
	if _, err := plain.ReserveArtifact(context.Background(), "https://unsafe.example"); err == nil {
		t.Fatal("unsafe artifact model identifier accepted")
	}
}

func TestOpaqueLeaseUsesSharedAdmissionCapacity(t *testing.T) {
	broker, err := scheduler.New(scheduler.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient("https://provider.example", "", "test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	configureAdmission(t, client, broker, "target-a", "provider-a")
	lease, err := client.AcquireOpaqueLease(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	blockedCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	blocked := make(chan error, 1)
	go func() {
		waitingLease, err := client.AcquireOpaqueLease(blockedCtx, 1)
		if waitingLease != nil {
			waitingLease.Release()
		}
		blocked <- err
	}()
	waitForSchedulerCondition(t, func() bool {
		return len(broker.Snapshot().Waiting) == 1 && broker.Snapshot().Waiting[0].Reason == scheduler.WaitGlobalCapacity
	})
	lease.Release()
	if err := <-blocked; err != nil {
		t.Fatalf("shared opaque admission=%v", err)
	}
}

// A04: an explicit Retry-After becomes a group-local broker cooldown. This
// covers native raw, SDK RoundTripper, and SSE stream attempts. The test uses
// loopback synchronization and cancellation rather than sleeping for the
// advertised (deliberately long) cooldown.
func TestSchedulerSharesRetryAfterOnlyWithinAliasAcrossTransportPaths(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(context.Context, *Client, string) error
	}{
		{
			name: "raw",
			invoke: func(ctx context.Context, c *Client, path string) error {
				_, err := c.DoJSON(ctx, http.MethodGet, path, nil)
				return err
			},
		},
		{
			name: "sdk",
			invoke: func(ctx context.Context, c *Client, path string) error {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL()+path, nil)
				if err != nil {
					return err
				}
				resp, err := c.PacedHTTPClient("model").Do(req)
				if resp != nil && resp.Body != nil {
					_ = resp.Body.Close()
				}
				return err
			},
		},
		{
			name: "stream",
			invoke: func(ctx context.Context, c *Client, path string) error {
				_, err := c.StreamJSONForModel(ctx, path, map[string]any{"model": "model"}, "model")
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/ready" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{}`)
					return
				}
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{}`)
			}))
			defer server.Close()

			broker := newAdmissionBroker(t, 2)
			primary, _ := NewClient(server.URL, "", "test", time.Nanosecond)
			shared, _ := NewClient(server.URL, "", "test", time.Nanosecond)
			ready, _ := NewClient(server.URL, "", "test", time.Nanosecond)
			configureAdmission(t, primary, broker, "primary", "provider-a")
			configureAdmission(t, shared, broker, "shared", "provider-a")
			configureAdmission(t, ready, broker, "ready", "provider-b")

			primaryCtx, stopPrimary := context.WithCancel(context.Background())
			primaryDone := make(chan error, 1)
			go func() { primaryDone <- test.invoke(primaryCtx, primary, "/cooldown") }()
			waitForSchedulerCondition(t, func() bool {
				group, ok := broker.Snapshot().Groups["provider-a"]
				return ok && !group.Cooldown.IsZero()
			})

			// A different alias remains immediately admissible.
			if err := test.invoke(context.Background(), ready, "/ready"); err != nil {
				t.Fatalf("independent alias was delayed: %v", err)
			}

			sharedCtx, stopShared := context.WithCancel(context.Background())
			sharedDone := make(chan error, 1)
			go func() { sharedDone <- test.invoke(sharedCtx, shared, "/blocked") }()
			waitForSchedulerCondition(t, func() bool {
				for _, wait := range broker.Snapshot().Waiting {
					if wait.Target == "shared" && wait.Group == "provider-a" && wait.Reason == scheduler.WaitRetryAfter {
						return true
					}
				}
				return false
			})
			if got := calls.Load(); got != 2 {
				t.Fatalf("shared cooldown dispatched a request: calls=%d, want 2", got)
			}

			stopShared()
			stopPrimary()
			if err := <-sharedDone; !errors.Is(err, context.Canceled) {
				t.Fatalf("shared request error=%v, want context cancellation", err)
			}
			if err := <-primaryDone; err == nil {
				t.Fatal("primary 429 request unexpectedly succeeded")
			}
		})
	}
}

func waitForSchedulerCondition(t *testing.T, condition func() bool) {
	t.Helper()
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	for !condition() {
		select {
		case <-timeout.C:
			t.Fatal("scheduler condition did not become true")
		default:
			runtime.Gosched()
		}
	}
}
