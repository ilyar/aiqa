package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/scheduler"
	"github.com/ilyar/aiqa/internal/telemetry"
	"go.opentelemetry.io/otel/propagation"
)

const maxResponseBody = 64 << 20

type Client struct {
	baseURL        *url.URL
	apiKey         string
	http           *http.Client
	userAgent      string
	pacer          *pacer
	headers        http.Header
	logMu          sync.RWMutex
	logger         *logging.Logger
	telemetry      *telemetry.Provider
	propagate      map[string]struct{}
	logFields      logging.Fields
	pacingObserver PacingObserver
	scheduler      schedulerConfig
	operation      atomic.Uint64
}

// SchedulerConfig attaches this client to a run-owned admission broker. Target
// is an operator-safe target identifier, never a URL.  An empty Group uses the
// client's normalized scheme/host origin; callers may instead provide a stable
// shared alias when several endpoints consume one provider quota. MemoryBytes
// is an optional conservative per-attempt reservation; otherwise the request
// body size is reserved. Artifact fields are opt-in because ordinary HTTP
// requests do not create remote artifacts.
type SchedulerConfig struct {
	Broker      *scheduler.Broker
	Target      string
	Group       string
	MemoryBytes int64
	ArtifactKey string
	Artifacts   int
	// QueueTimeout bounds scheduler admission only. It deliberately does not
	// start the HTTP attempt timeout or consume its budget.
	QueueTimeout time.Duration
}

type schedulerConfig struct {
	broker       *scheduler.Broker
	target       string
	group        string
	memory       int64
	key          string
	items        int
	queueTimeout time.Duration
}

// ArtifactReservation is a short-lived local admission for a billable remote
// artifact.  The caller must either Commit it immediately before the request
// can reach the provider, or CancelBeforeDispatch when no request was sent.
// Commit frees the transient admission capacity but deliberately keeps the
// artifact bucket consumed: a timeout after dispatch is not a free retry.
// Both terminal methods are idempotent.
//
// A reservation returned while scheduling is disabled is a no-op. This keeps
// direct API users backwards compatible while allowing runners to use one
// uniform pre-dispatch hook for native and SDK requests.
type ArtifactReservation struct {
	lease *scheduler.Lease
	once  sync.Once
}

// Commit marks the point after which remote creation may have happened.
func (r *ArtifactReservation) Commit() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.lease != nil {
			r.lease.Release()
		}
	})
}

// CancelBeforeDispatch refunds the artifact bucket only when the caller has
// established that it did not dispatch a remote creation request.
func (r *ArtifactReservation) CancelBeforeDispatch() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.lease != nil {
			r.lease.CancelBeforeDispatch()
		}
	})
}

// PacingObserver reports an actual client-side pacing wait. target is the
// caller-established, safe target identifier from SetLogFields; it is never a
// provider URL. A false event is the terminal state of the preceding wait.
// The callback must return promptly and must not call back into Client.
type PacingObserver func(target string, waiting bool, duration time.Duration)

func NewClient(baseURL, apiKey, userAgent string, initialInterval time.Duration) (*Client, error) {
	return newClient(baseURL, apiKey, userAgent, initialInterval, nil)
}

// NewClientWithHeaders creates a client for a non-Bearer protocol. Headers are
// copied and cannot be changed through ambient process environment variables.
func NewClientWithHeaders(baseURL, userAgent string, initialInterval time.Duration, headers http.Header) (*Client, error) {
	return newClient(baseURL, "", userAgent, initialInterval, headers)
}

func newClient(baseURL, apiKey, userAgent string, initialInterval time.Duration, headers http.Header) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 16
	transport.ResponseHeaderTimeout = 0
	client := &Client{
		baseURL:   u,
		apiKey:    apiKey,
		http:      &http.Client{Transport: transport},
		userAgent: userAgent,
		pacer:     newPacer(initialInterval),
		headers:   headers.Clone(),
		logFields: logging.Fields{},
	}
	client.http.CheckRedirect = func(request *http.Request, _ []*http.Request) error {
		client.injectTraceContext(request.Context(), request)
		return nil
	}
	return client, nil
}

func (c *Client) CloneWithKey(apiKey string) *Client {
	c.logMu.RLock()
	logger, provider, fields, propagate, pacingObserver, admission := c.logger, c.telemetry, cloneLogFields(c.logFields), cloneOrigins(c.propagate), c.pacingObserver, c.scheduler
	c.logMu.RUnlock()
	return &Client{baseURL: c.baseURL, apiKey: apiKey, http: c.http, userAgent: c.userAgent, pacer: c.pacer, headers: c.headers.Clone(), logger: logger, telemetry: provider, propagate: propagate, logFields: fields, pacingObserver: pacingObserver, scheduler: admission}
}

func (c *Client) CloneWithHeaders(headers http.Header) *Client {
	c.logMu.RLock()
	logger, provider, fields, propagate, pacingObserver, admission := c.logger, c.telemetry, cloneLogFields(c.logFields), cloneOrigins(c.propagate), c.pacingObserver, c.scheduler
	c.logMu.RUnlock()
	return &Client{baseURL: c.baseURL, apiKey: c.apiKey, http: c.http, userAgent: c.userAgent, pacer: c.pacer, headers: headers.Clone(), logger: logger, telemetry: provider, propagate: propagate, logFields: fields, pacingObserver: pacingObserver, scheduler: admission}
}

func cloneOrigins(origins map[string]struct{}) map[string]struct{} {
	if origins == nil {
		return nil
	}
	copy := make(map[string]struct{}, len(origins))
	for origin := range origins {
		copy[origin] = struct{}{}
	}
	return copy
}

func (c *Client) BaseURL() string          { return c.baseURL.String() }
func (c *Client) PacingStats() PacingStats { return c.pacer.snapshot() }
func (c *Client) PacingConstraints() PacingStats {
	return c.pacer.constraints()
}
func (c *Client) SetConcurrency(limit int) { c.pacer.setConcurrency(limit) }
func (c *Client) PacingStatsForModel(model string) PacingStats {
	return c.pacer.snapshotForModel(model)
}

// SetLogger enables private structured transport diagnostics. The logger is
// intentionally opt-in: callers that do not request --verbose retain no
// in-memory transport trace. It is safe to set before concurrent work starts.
func (c *Client) SetLogger(logger *logging.Logger) {
	c.logMu.Lock()
	c.logger = logger
	c.logMu.Unlock()
}

// SetTelemetry enables local-only OpenTelemetry lifecycle correlation. The
// provider never injects trace context into requests; this client deliberately
// leaves outbound headers unchanged.
func (c *Client) SetTelemetry(provider *telemetry.Provider) {
	c.logMu.Lock()
	c.telemetry = provider
	c.logMu.Unlock()
}

// SetOTelPropagationAllowlist enables W3C trace headers only for exact,
// caller-validated origins. An empty list is the secure default.
func (c *Client) SetOTelPropagationAllowlist(origins []string) {
	c.logMu.Lock()
	c.propagate = make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		c.propagate[origin] = struct{}{}
	}
	c.logMu.Unlock()
}

func (c *Client) injectTraceContext(ctx context.Context, request *http.Request) {
	request.Header.Del("traceparent")
	request.Header.Del("tracestate")
	request.Header.Del("baggage")
	origin := request.URL.Scheme + "://" + request.URL.Host
	c.logMu.RLock()
	_, allowed := c.propagate[origin]
	c.logMu.RUnlock()
	if allowed {
		propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(request.Header))
	}
}

// SetLogFields establishes immutable target-level trace scope, for example
// target and protocol. Never pass a base URL or credential-derived value.
func (c *Client) SetLogFields(fields logging.Fields) {
	c.logMu.Lock()
	c.logFields = cloneLogFields(fields)
	c.logMu.Unlock()
}

// SetPacingObserver enables optional operator-facing pacing state. It is
// independent of diagnostics and transport attempt accounting.
func (c *Client) SetPacingObserver(observer PacingObserver) {
	c.logMu.Lock()
	c.pacingObserver = observer
	c.logMu.Unlock()
}

// SetScheduler configures optional run-owned admission. Passing a zero config
// disables it. The configuration is copied, so clones retain a consistent
// target/group binding. It is safe to call before concurrent work begins.
func (c *Client) SetScheduler(cfg SchedulerConfig) error {
	if cfg.Broker == nil {
		if cfg.Target != "" || cfg.Group != "" || cfg.MemoryBytes != 0 || cfg.ArtifactKey != "" || cfg.Artifacts != 0 || cfg.QueueTimeout != 0 {
			return errors.New("scheduler configuration requires a broker")
		}
		c.logMu.Lock()
		c.scheduler = schedulerConfig{}
		c.logMu.Unlock()
		return nil
	}
	if !safeSchedulerTarget(cfg.Target) || cfg.MemoryBytes < 0 || cfg.Artifacts < 0 || cfg.QueueTimeout < 0 || (cfg.Artifacts > 0 && cfg.ArtifactKey == "") || (cfg.ArtifactKey != "" && !safeSchedulerID(cfg.ArtifactKey)) {
		return errors.New("invalid scheduler configuration")
	}
	group := cfg.Group
	if group == "" {
		group = normalizedOrigin(c.baseURL)
	}
	if !safeSchedulerGroup(group) {
		return errors.New("invalid scheduler group")
	}
	c.logMu.Lock()
	c.scheduler = schedulerConfig{broker: cfg.Broker, target: cfg.Target, group: group, memory: cfg.MemoryBytes, key: cfg.ArtifactKey, items: cfg.Artifacts, queueTimeout: cfg.QueueTimeout}
	c.logMu.Unlock()
	return nil
}

func normalizedOrigin(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	// Scheduler groups can reach live execution snapshots, so the default must
	// preserve origin equivalence without retaining the provider origin itself.
	// An explicit group is already a validated local alias.
	origin := strings.ToLower(u.Scheme) + "://" + host
	sum := sha256.Sum256([]byte(origin))
	return "origin-" + hex.EncodeToString(sum[:])
}

// safeSchedulerID accepts non-secret local labels (including target/model
// artifact keys), never values copied from an HTTP request.
func safeSchedulerID(value string) bool {
	if value == "" || len(value) > 256 || strings.ContainsAny(value, "\r\n\t @?#") {
		return false
	}
	return true
}

func safeSchedulerTarget(value string) bool {
	return safeSchedulerID(value) && !strings.Contains(value, "://") && !strings.Contains(value, "/")
}

func safeSchedulerGroup(value string) bool {
	return safeSchedulerID(value) && !strings.Contains(value, "@") && !strings.Contains(value, "?") && !strings.Contains(value, "#")
}

func (c *Client) acquireScheduler(ctx context.Context, bodyBytes int64) (*scheduler.Lease, error) {
	c.logMu.RLock()
	admission := c.scheduler
	c.logMu.RUnlock()
	if admission.broker == nil {
		return nil, nil
	}
	memory := admission.memory
	if memory == 0 && bodyBytes > 0 {
		memory = bodyBytes
	}
	admissionCtx := ctx
	var cancel context.CancelFunc
	if admission.queueTimeout > 0 {
		admissionCtx, cancel = context.WithTimeout(ctx, admission.queueTimeout)
		defer cancel()
	}
	return admission.broker.Acquire(admissionCtx, scheduler.Request{Target: admission.target, Group: admission.group, MemoryBytes: memory, ArtifactKey: admission.key, Artifacts: admission.items})
}

// ReserveArtifact obtains one shared (target, model) creation reservation.
// It is deliberately separate from ordinary HTTP admission: retries and
// response reads must not create another billable-artifact reservation. Call
// Commit immediately before native or SDK dispatch; call
// CancelBeforeDispatch only if dispatch was definitely not attempted.
func (c *Client) ReserveArtifact(ctx context.Context, modelID string) (*ArtifactReservation, error) {
	// Model identifiers can legitimately contain provider punctuation such as
	// `vendor/model`. They are never placed in scheduler metadata: hash the
	// target/model pair into an opaque bucket key instead.
	if modelID == "" || len(modelID) > 1024 || strings.Contains(modelID, "://") || strings.ContainsAny(modelID, "\r\n\t") {
		return nil, errors.New("invalid artifact model identifier")
	}
	c.logMu.RLock()
	admission := c.scheduler
	c.logMu.RUnlock()
	if admission.broker == nil {
		return &ArtifactReservation{}, nil
	}
	admissionCtx := ctx
	var cancel context.CancelFunc
	if admission.queueTimeout > 0 {
		admissionCtx, cancel = context.WithTimeout(ctx, admission.queueTimeout)
		defer cancel()
	}
	keySum := sha256.Sum256([]byte(admission.target + "\x00" + modelID))
	lease, err := admission.broker.Acquire(admissionCtx, scheduler.Request{
		Target:      admission.target,
		Group:       admission.group,
		ArtifactKey: "artifact-" + hex.EncodeToString(keySum[:]),
		Artifacts:   1,
	})
	if err != nil {
		return nil, err
	}
	return &ArtifactReservation{lease: lease}, nil
}

// applySchedulerRetryAfter shares an explicit provider Retry-After signal with
// every client bound to this run's pacing group. It deliberately receives only
// a parsed header duration: neither a URL nor any response/request content is
// retained by the scheduler. A missing, malformed, expired, or zero delay is
// not a cooldown signal.
func (c *Client) applySchedulerRetryAfter(header http.Header) {
	delay := retryAfter(header)
	if delay <= 0 {
		return
	}
	c.logMu.RLock()
	admission := c.scheduler
	c.logMu.RUnlock()
	if admission.broker == nil {
		return
	}
	// SetScheduler validates group as an operator-safe alias. ApplyCooldown is
	// monotonic, so concurrent 429 responses can only extend this group-local
	// deadline and never affect another alias.
	_ = admission.broker.ApplyCooldown(admission.group, time.Now().Add(delay), scheduler.WaitRetryAfter)
}

func (c *Client) waitForPacing(ctx context.Context, model string) error {
	return c.pacer.wait(ctx, model, func(waiting bool, duration time.Duration) {
		c.logMu.RLock()
		observer := c.pacingObserver
		target, _ := c.logFields["target"].(string)
		c.logMu.RUnlock()
		if observer != nil {
			observer(target, waiting, duration)
		}
	})
}

// LogDiagnostic records a transport-correlated diagnostic emitted by a
// protocol validator. Fields remain subject to logging's allowlist.
func (c *Client) LogDiagnostic(ctx context.Context, level logging.Level, event string, fields logging.Fields) {
	c.trace(ctx, level, event, fields)
}

func (c *Client) trace(ctx context.Context, level logging.Level, event string, fields logging.Fields) {
	c.logMu.RLock()
	logger := c.logger
	base := cloneLogFields(c.logFields)
	c.logMu.RUnlock()
	if contextual := logging.FromContext(ctx); contextual != nil {
		logger = contextual
	}
	if logger == nil {
		return
	}
	for key, value := range logging.FieldsFromContext(ctx) {
		base[key] = value
	}
	for key, value := range fields {
		base[key] = value
	}
	logger.LogContext(ctx, level, event, base)
}

func (c *Client) transportContext(ctx context.Context, model string) context.Context {
	fields := logging.Fields{"transport_operation": fmt.Sprintf("http-%d", c.operation.Add(1))}
	if model != "" {
		fields["model"] = model
	}
	return logging.WithFields(ctx, fields)
}

func (c *Client) startOperation(ctx context.Context, name string, fields logging.Fields) (context.Context, *telemetry.Operation) {
	c.logMu.RLock()
	provider := c.telemetry
	c.logMu.RUnlock()
	if provider == nil {
		return ctx, nil
	}
	return provider.Start(ctx, name, fields)
}

func operationID(ctx context.Context) string {
	if id, _ := logging.FieldsFromContext(ctx)["operation_id"].(string); id != "" {
		return id
	}
	id, _ := logging.FieldsFromContext(ctx)["transport_operation"].(string)
	return id
}

// operationReadCloser closes a SDK logical-request span only when the SDK has
// finished consuming its response body. This preserves the real SDK lifecycle
// rather than ending telemetry at response-header receipt.
type operationReadCloser struct {
	io.ReadCloser
	once      sync.Once
	operation *telemetry.Operation
	parent    *telemetry.Operation
	fields    logging.Fields
	outcome   string
	onClose   func()
	cancel    context.CancelFunc
}

func (r *operationReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.once.Do(func() {
		if r.cancel != nil {
			defer r.cancel()
		}
		if r.onClose != nil {
			r.onClose()
		}
		fields := cloneLogFields(r.fields)
		if err != nil {
			fields["reason"] = errorClass(err)
			fields["error_present"] = true
		}
		outcome := r.outcome
		if err != nil {
			outcome = "close_error"
		}
		if r.operation != nil {
			r.operation.End(outcome, fields)
		}
		if r.parent != nil {
			r.parent.End(outcome, fields)
		}
	})
	return err
}

func cloneLogFields(fields logging.Fields) logging.Fields {
	copy := make(logging.Fields, len(fields))
	for key, value := range fields {
		copy[key] = value
	}
	return copy
}

// PacedHTTPClient exposes the target's transport and adaptive pacing to an
// official SDK without exposing AIQA credentials or request construction. The
// SDK must still disable its own retries; this transport owns the shared retry
// and rate-limit policy.
func (c *Client) PacedHTTPClient(model string) *http.Client {
	return &http.Client{Transport: &pacedRoundTripper{client: c, model: model}, CheckRedirect: func(request *http.Request, _ []*http.Request) error {
		// net/http copies ordinary headers across redirects; never inherit trace
		// propagation without checking the redirect destination again.
		c.injectTraceContext(request.Context(), request)
		return nil
	}}
}

type pacedRoundTripper struct {
	client *Client
	model  string
}

func (r *pacedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx := r.client.transportContext(request.Context(), r.model)
	// Direct SDK callers need a body-lifetime SDK boundary. Built-in suites
	// already establish their one report-visible named probe; nesting another
	// sdk.probe would give one HTTP attempt two competing owners.
	var sdkProbe *telemetry.Operation
	if _, named := logging.FieldsFromContext(ctx)["probe"]; !named {
		ctx, sdkProbe = r.client.startOperation(ctx, "sdk.probe", logging.Fields{"method": request.Method, "path": safeRequestPath(request.URL)})
	}
	handedOff := false
	defer func() {
		if !handedOff && sdkProbe != nil {
			sdkProbe.End("completed", nil)
		}
	}()
	ctx, logical := r.client.startOperation(ctx, "sdk.logical_request", logging.Fields{"method": request.Method, "path": safeRequestPath(request.URL)})
	request = request.Clone(ctx)
	path := safeRequestPath(request.URL)
	endLogical := func(outcome string, response *http.Response, err error) {
		if logical != nil {
			logical.End(outcome, responseFields(request.Method, path, 0, response, err, 0))
		}
	}
	r.client.pacer.logical(r.model)
	r.client.trace(ctx, logging.Trace, "sdk.logical_request.begin", logging.Fields{"method": request.Method, "path": path})
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 && request.GetBody == nil {
			r.client.trace(ctx, logging.Error, "sdk.retry.unavailable", logging.Fields{"method": request.Method, "path": path, "attempt": attempt + 1, "reason": "non_replayable_body"})
			err := errors.New("cannot retry SDK request with a non-replayable body")
			endLogical("retry_unavailable", nil, err)
			return nil, err
		}
		waitStarted := time.Now()
		r.client.trace(ctx, logging.Trace, "pacing.wait.begin", logging.Fields{"method": request.Method, "path": path, "attempt": attempt + 1})
		if err := r.client.waitForPacing(request.Context(), r.model); err != nil {
			r.client.trace(ctx, logging.Debug, "pacing.wait.cancelled", logging.Fields{"method": request.Method, "path": path, "attempt": attempt + 1, "wait_ms": time.Since(waitStarted).Milliseconds(), "reason": errorClass(err)})
			r.client.trace(ctx, logging.Debug, "sdk.logical_request.result", logging.Fields{"method": request.Method, "path": path, "outcome": "pacing_cancelled", "reason": errorClass(err)})
			endLogical("pacing_cancelled", nil, err)
			return nil, err
		}
		r.client.trace(ctx, logging.Trace, "pacing.wait.end", logging.Fields{"method": request.Method, "path": path, "attempt": attempt + 1, "wait_ms": time.Since(waitStarted).Milliseconds()})
		lease, err := r.client.acquireScheduler(ctx, request.ContentLength)
		if err != nil {
			r.client.pacer.release(r.model)
			r.client.trace(ctx, logging.Debug, "scheduler.acquire.cancelled", logging.Fields{"method": request.Method, "path": path, "attempt": attempt + 1, "reason": errorClass(err)})
			endLogical("scheduler_cancelled", nil, err)
			return nil, err
		}
		current := request
		attemptCtx, cancelAttempt := attemptContext(ctx)
		if attempt > 0 {
			body, err := request.GetBody()
			if err != nil {
				cancelAttempt()
				if lease != nil {
					lease.CancelBeforeDispatch()
				}
				r.client.pacer.release(r.model)
				r.client.trace(ctx, logging.Error, "sdk.retry.body_error", logging.Fields{"method": request.Method, "path": path, "attempt": attempt + 1, "reason": errorClass(err)})
				endLogical("retry_body_error", nil, err)
				return nil, err
			}
			current = request.Clone(attemptCtx)
			current.Body = body
		}
		if attempt > 0 {
			r.client.pacer.retry(r.model)
		}
		attemptCtx, attemptOp := r.client.startOperation(attemptCtx, "sdk.http_attempt", logging.Fields{"method": request.Method, "path": path, "attempt": attempt + 1})
		current = current.Clone(attemptCtx)
		r.client.injectTraceContext(attemptCtx, current)
		r.client.trace(attemptCtx, logging.Trace, "sdk.attempt.begin", logging.Fields{"method": request.Method, "path": path, "attempt": attempt + 1})
		started := time.Now()
		response, err := r.client.http.Transport.RoundTrip(current)
		attemptFields := responseFields(request.Method, path, attempt+1, response, err, time.Since(started))
		r.client.trace(attemptCtx, logging.Trace, "sdk.attempt.result", attemptFields)
		if attemptOp != nil {
			outcome := "success"
			if err != nil || response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
				outcome = "response"
			}
			attemptOp.End(outcome, attemptFields)
		}
		retry := err != nil || response != nil && (response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500)
		if response != nil && response.StatusCode == http.StatusTooManyRequests {
			r.client.applySchedulerRetryAfter(response.Header)
			if attempt == 2 {
				r.client.pacer.limited(r.model, retryAfter(response.Header))
			}
		}
		if !retry || attempt == 2 {
			if err == nil && response != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
				r.client.pacer.success(r.model, time.Since(started))
				if attempt > 0 {
					r.client.pacer.recovered(r.model)
				}
			} else if attempt == 2 && retry {
				r.client.pacer.exhausted(r.model)
			}
			r.client.pacer.release(r.model)
			endFields := responseFields(request.Method, path, attempt+1, response, err, time.Since(started))
			if response != nil && response.Body != nil {
				handedOff = true
				var admissionDone chan struct{}
				if lease != nil {
					admissionDone = make(chan struct{})
					go releaseLeaseOnCancel(attemptCtx, admissionDone, lease)
				}
				response.Body = &operationReadCloser{ReadCloser: response.Body, operation: logical, parent: sdkProbe, fields: endFields, outcome: "response_closed", cancel: cancelAttempt, onClose: func() {
					if lease != nil {
						close(admissionDone)
						lease.Release()
					}
					r.client.trace(ctx, logging.Debug, "sdk.logical_request.result", endFields)
				}}
			} else {
				cancelAttempt()
				if lease != nil {
					lease.Release()
				}
				r.client.trace(ctx, logging.Debug, "sdk.logical_request.result", endFields)
				endLogical("response", response, err)
			}
			return response, err
		}
		cancelAttempt()
		if lease != nil {
			lease.Release()
		}
		r.client.pacer.release(r.model)
		delay := time.Duration(1<<attempt) * 100 * time.Millisecond
		if response != nil {
			if response.StatusCode == http.StatusTooManyRequests {
				delay = r.client.pacer.limited(r.model, retryAfter(response.Header))
			}
			if response.Body != nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
				_ = response.Body.Close()
			}
		}
		r.client.trace(ctx, logging.Debug, "sdk.retry.scheduled", logging.Fields{"method": request.Method, "path": path, "attempt": attempt + 1, "next_attempt": attempt + 2, "reason": retryReason(response, err), "delay_ms": delay.Milliseconds()})
		if response == nil || response.StatusCode != http.StatusTooManyRequests {
			r.client.pacer.backoff(r.model, delay)
			if err := waitDuration(request.Context(), delay); err != nil {
				r.client.trace(ctx, logging.Debug, "sdk.retry.cancelled", logging.Fields{"method": request.Method, "path": path, "attempt": attempt + 1, "reason": errorClass(err)})
				endLogical("retry_cancelled", nil, err)
				return nil, err
			}
		}
	}
	panic("unreachable")
}

func releaseLeaseOnCancel(ctx context.Context, done <-chan struct{}, lease *scheduler.Lease) {
	select {
	case <-ctx.Done():
		lease.Release()
	case <-done:
	}
}

func (c *Client) ListModels(ctx context.Context) ([]Model, Response, error) {
	resp, err := c.DoJSON(ctx, http.MethodGet, "models", nil)
	if err != nil {
		return nil, resp, err
	}
	if !resp.Is2xx() {
		return nil, resp, fmt.Errorf("list models: HTTP %d", resp.StatusCode)
	}
	data, ok := resp.JSON["data"].([]any)
	if !ok {
		return nil, resp, errors.New("list models: data is missing or not an array")
	}
	models := make([]Model, 0, len(data))
	for i, item := range data {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, resp, fmt.Errorf("list models: data[%d] is not an object", i)
		}
		id, _ := m["id"].(string)
		if id == "" {
			return nil, resp, fmt.Errorf("list models: data[%d].id is missing", i)
		}
		models = append(models, Model{ID: id, Raw: m})
	}
	return models, resp, nil
}

func (c *Client) RetrieveModel(ctx context.Context, model string) (Response, error) {
	endpoint := strings.TrimRight(c.baseURL.String(), "/") + "/models/" + url.PathEscape(model)
	return c.doRawURL(ctx, http.MethodGet, endpoint, nil, "application/json", model)
}

func (c *Client) Chat(ctx context.Context, body map[string]any) (Response, error) {
	return c.doJSON(ctx, http.MethodPost, "chat/completions", body, modelFromBody(body))
}

func (c *Client) ChatRaw(ctx context.Context, raw []byte, contentType string) (Response, error) {
	return c.DoRaw(ctx, http.MethodPost, "chat/completions", raw, contentType)
}

func (c *Client) ImagesGenerate(ctx context.Context, body map[string]any) (Response, error) {
	return c.doJSON(ctx, http.MethodPost, "images/generations", body, modelFromBody(body))
}

func (c *Client) ImagesRaw(ctx context.Context, raw []byte, contentType string) (Response, error) {
	return c.DoRaw(ctx, http.MethodPost, "images/generations", raw, contentType)
}

func (c *Client) DoJSON(ctx context.Context, method, path string, body any) (Response, error) {
	return c.DoJSONForModel(ctx, method, path, body, modelFromBody(body))
}

// DoJSONForModel is the model-scoped variant of DoJSON.  Protocols whose
// model identifier lives in the URL (rather than the JSON body), such as
// Google GenAI, use this method so adaptive pacing and per-model telemetry are
// attributed to the right model.
func (c *Client) DoJSONForModel(ctx context.Context, method, path string, body any, model string) (Response, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return Response{}, fmt.Errorf("marshal request: %w", err)
		}
	}
	return c.doRaw(ctx, method, path, raw, "application/json", model)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, model string) (Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}
	return c.doRaw(ctx, method, path, raw, "application/json", model)
}

func (c *Client) DoRaw(ctx context.Context, method, path string, body []byte, contentType string) (Response, error) {
	return c.DoRawForModel(ctx, method, path, body, contentType, modelFromRaw(body))
}

// DoRawForModel is the model-scoped variant of DoRaw.  It preserves the raw
// request bytes while still associating pacing, retries, and telemetry with a
// URL-addressed model.
func (c *Client) DoRawForModel(ctx context.Context, method, path string, body []byte, contentType, model string) (Response, error) {
	return c.doRaw(ctx, method, path, body, contentType, model)
}

func (c *Client) doRaw(ctx context.Context, method, path string, body []byte, contentType, model string) (Response, error) {
	u := *c.baseURL
	rel, err := url.Parse(path)
	if err != nil {
		return Response{}, fmt.Errorf("parse request path: %w", err)
	}
	u.Path = joinURLPath(c.baseURL.Path, rel.Path)
	u.RawPath = joinURLPath(c.baseURL.EscapedPath(), rel.EscapedPath())
	u.RawQuery = rel.RawQuery
	return c.doRawURL(ctx, method, u.String(), body, contentType, model)
}

func (c *Client) doRawURL(ctx context.Context, method, endpoint string, body []byte, contentType, model string) (response Response, err error) {
	ctx = c.transportContext(ctx, model)
	path := safeEndpointPath(endpoint)
	if _, hasSuite := logging.FieldsFromContext(ctx)["suite"]; !hasSuite {
		var suiteOp *telemetry.Operation
		ctx, suiteOp = c.startOperation(ctx, "suite", logging.Fields{"suite": "native"})
		if suiteOp != nil {
			defer suiteOp.End("completed", nil)
		}
	}
	// Transport owns the fallback probe boundary. Individual suites may create
	// a more specific parent later; this boundary ensures every physical
	// provider operation is still attributable to a probe now.
	if _, named := logging.FieldsFromContext(ctx)["probe"]; !named {
		var probeOp *telemetry.Operation
		ctx, probeOp = c.startOperation(ctx, "probe", logging.Fields{"method": method, "path": path})
		if probeOp != nil {
			defer probeOp.End("completed", nil)
		}
	}
	ctx, logical := c.startOperation(ctx, "http.logical_request", logging.Fields{"method": method, "path": path})
	defer func() {
		// The shape builder parses only transient bytes and returns a fixed
		// allowlisted fingerprint. In particular it never logs Body or err.Error.
		c.trace(ctx, logging.Trace, "diagnostic.response_shape", logging.ResponseShapeFields(method, path, contentType, body, response.StatusCode, response.ContentType, response.RequestID, response.Body))
	}()
	traceID := operationID(ctx)
	endLogical := func(outcome string, response Response, err error) {
		if logical != nil {
			logical.End(outcome, rawResponseFields(method, path, 0, response, err))
		}
	}
	c.pacer.logical(model)
	c.trace(ctx, logging.Trace, "http.logical_request.begin", logging.Fields{"method": method, "path": path})
	for attempt := 0; attempt < 3; attempt++ {
		waitStarted := time.Now()
		c.trace(ctx, logging.Trace, "pacing.wait.begin", logging.Fields{"method": method, "path": path, "attempt": attempt + 1})
		if err := c.waitForPacing(ctx, model); err != nil {
			c.trace(ctx, logging.Debug, "pacing.wait.cancelled", logging.Fields{"method": method, "path": path, "attempt": attempt + 1, "wait_ms": time.Since(waitStarted).Milliseconds(), "reason": errorClass(err)})
			c.trace(ctx, logging.Debug, "http.logical_request.result", logging.Fields{"method": method, "path": path, "outcome": "pacing_cancelled", "reason": errorClass(err)})
			response := Response{TraceID: traceID}
			endLogical("pacing_cancelled", response, err)
			return response, err
		}
		c.trace(ctx, logging.Trace, "pacing.wait.end", logging.Fields{"method": method, "path": path, "attempt": attempt + 1, "wait_ms": time.Since(waitStarted).Milliseconds()})
		lease, err := c.acquireScheduler(ctx, int64(len(body)))
		if err != nil {
			c.pacer.release(model)
			c.trace(ctx, logging.Debug, "scheduler.acquire.cancelled", logging.Fields{"method": method, "path": path, "attempt": attempt + 1, "reason": errorClass(err)})
			response := Response{TraceID: traceID}
			endLogical("scheduler_cancelled", response, err)
			return response, err
		}
		if attempt > 0 {
			c.pacer.retry(model)
		}
		c.trace(ctx, logging.Trace, "http.attempt.begin", logging.Fields{"method": method, "path": path, "attempt": attempt + 1})
		attemptCtx, cancelAttempt := attemptContext(ctx)
		resp, err := c.doRawURLOnce(attemptCtx, method, endpoint, body, contentType, attempt+1)
		cancelAttempt()
		if lease != nil {
			lease.Release()
		}
		resp.TraceID = traceID
		c.trace(ctx, logging.Trace, "http.attempt.result", rawResponseFields(method, path, attempt+1, resp, err))
		if !shouldRetry(resp, err) {
			if resp.Is2xx() {
				c.pacer.success(model, resp.Duration)
				if attempt > 0 {
					c.pacer.recovered(model)
				}
			}
			c.pacer.release(model)
			c.trace(ctx, logging.Debug, "http.logical_request.result", rawResponseFields(method, path, attempt+1, resp, err))
			endLogical("response", resp, err)
			return resp, err
		}
		c.pacer.release(model)
		delay := retryDelay(resp, attempt)
		if resp.StatusCode == http.StatusTooManyRequests {
			c.applySchedulerRetryAfter(http.Header(resp.Header))
			c.pacer.limited(model, delay)
		}
		if attempt == 2 {
			c.pacer.exhausted(model)
			c.trace(ctx, logging.Warn, "http.retry.exhausted", logging.Fields{"method": method, "path": path, "attempt": attempt + 1, "reason": retryReasonResponse(resp, err), "status": resp.StatusCode})
			c.trace(ctx, logging.Debug, "http.logical_request.result", rawResponseFields(method, path, attempt+1, resp, err))
			endLogical("retry_exhausted", resp, err)
			return resp, err
		}
		c.trace(ctx, logging.Debug, "http.retry.scheduled", logging.Fields{"method": method, "path": path, "attempt": attempt + 1, "next_attempt": attempt + 2, "reason": retryReasonResponse(resp, err), "status": resp.StatusCode, "delay_ms": delay.Milliseconds()})
		if resp.StatusCode != http.StatusTooManyRequests {
			c.pacer.backoff(model, delay)
			select {
			case <-ctx.Done():
				c.trace(ctx, logging.Debug, "http.retry.cancelled", logging.Fields{"method": method, "path": path, "attempt": attempt + 1, "reason": errorClass(ctx.Err())})
				c.trace(ctx, logging.Debug, "http.logical_request.result", logging.Fields{"method": method, "path": path, "attempt": attempt + 1, "outcome": "retry_cancelled", "reason": errorClass(ctx.Err())})
				err := ctx.Err()
				endLogical("retry_cancelled", resp, err)
				return resp, err
			case <-time.After(delay):
			}
		}
	}
	panic("unreachable")
}

func (c *Client) doRawURLOnce(ctx context.Context, method, endpoint string, body []byte, contentType string, attemptNumber int) (resp Response, err error) {
	path := safeEndpointPath(endpoint)
	ctx, attempt := c.startOperation(ctx, "http.attempt", logging.Fields{"method": method, "path": path, "attempt": attemptNumber})
	if attempt != nil {
		defer func() {
			outcome := "success"
			if err != nil || !resp.Is2xx() {
				outcome = "response"
			}
			attempt.End(outcome, rawResponseFields(method, path, attemptNumber, resp, err))
		}()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return Response{}, err
	}
	c.applyHeaders(req, contentType)
	c.injectTraceContext(ctx, req)
	started := time.Now()
	httpResp, err := c.http.Do(req)
	if err != nil {
		return Response{Duration: time.Since(started)}, err
	}
	defer httpResp.Body.Close()
	limited := io.LimitReader(httpResp.Body, maxResponseBody+1)
	responseBody, readErr := io.ReadAll(limited)
	duration := time.Since(started)
	resp = Response{
		StatusCode:  httpResp.StatusCode,
		Header:      httpResp.Header.Clone(),
		ContentType: httpResp.Header.Get("Content-Type"),
		RequestID:   requestID(httpResp.Header),
		Body:        responseBody,
		Duration:    duration,
	}
	if len(responseBody) > maxResponseBody {
		return resp, fmt.Errorf("response body exceeds %d bytes", maxResponseBody)
	}
	if readErr != nil {
		return resp, fmt.Errorf("read response: %w", readErr)
	}
	if len(bytes.TrimSpace(responseBody)) > 0 && strings.Contains(strings.ToLower(resp.ContentType), "json") {
		dec := json.NewDecoder(bytes.NewReader(responseBody))
		dec.UseNumber()
		if err := dec.Decode(&resp.JSON); err != nil {
			return resp, fmt.Errorf("decode JSON response: %w", err)
		}
	} else if len(bytes.TrimSpace(responseBody)) > 0 {
		var generic map[string]any
		dec := json.NewDecoder(bytes.NewReader(responseBody))
		dec.UseNumber()
		if err := dec.Decode(&generic); err == nil {
			resp.JSON = generic
		}
	}
	return resp, nil
}

func retryAfter(header http.Header) time.Duration {
	v := strings.TrimSpace(header.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(v); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(v); err == nil {
		return time.Until(when)
	}
	return 0
}

func (c *Client) ChatStream(ctx context.Context, body map[string]any) (StreamResponse, error) {
	body = cloneMap(body)
	body["stream"] = true
	return c.StreamJSON(ctx, "chat/completions", body)
}

// StreamJSON sends a JSON request and returns parsed SSE events without
// requiring OpenAI-specific event framing. OpenAI fields are still accumulated
// when present for backwards compatibility with ChatStream.
func (c *Client) StreamJSON(ctx context.Context, path string, body map[string]any) (StreamResponse, error) {
	return c.StreamJSONForModel(ctx, path, body, modelFromBody(body))
}

// StreamJSONForModel is the model-scoped variant of StreamJSON for protocols
// that put the model in the request path rather than in the body.
func (c *Client) StreamJSONForModel(ctx context.Context, path string, body map[string]any, model string) (result StreamResponse, err error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return StreamResponse{}, fmt.Errorf("marshal stream request: %w", err)
	}
	c.pacer.logical(model)
	ctx = c.transportContext(ctx, model)
	u := *c.baseURL
	pathPart, queryPart, _ := strings.Cut(path, "?")
	u.Path = joinURLPath(c.baseURL.Path, pathPart)
	u.RawQuery = queryPart
	requestPath := safeRequestPath(&u)
	if _, hasSuite := logging.FieldsFromContext(ctx)["suite"]; !hasSuite {
		var suiteOp *telemetry.Operation
		ctx, suiteOp = c.startOperation(ctx, "suite", logging.Fields{"suite": "native"})
		if suiteOp != nil {
			defer suiteOp.End("completed", nil)
		}
	}
	if _, named := logging.FieldsFromContext(ctx)["probe"]; !named {
		var probeOp *telemetry.Operation
		ctx, probeOp = c.startOperation(ctx, "probe", logging.Fields{"method": http.MethodPost, "path": requestPath, "stream": true})
		if probeOp != nil {
			defer probeOp.End("completed", nil)
		}
	}
	ctx, logical := c.startOperation(ctx, "http.stream_logical_request", logging.Fields{"method": http.MethodPost, "path": requestPath, "stream": true})
	defer func() {
		shape := logging.ResponseShapeFields(http.MethodPost, requestPath, "application/json", raw, result.StatusCode, result.ContentType, result.RequestID, []byte(result.RawSnippet))
		events := make([]logging.SSEShapeEvent, 0, len(result.Events))
		for _, event := range result.Events {
			events = append(events, logging.SSEShapeEvent{Event: event.Event, JSON: event.JSON, Done: event.Data == "[DONE]"})
		}
		for key, value := range logging.SSEShapeFields(events, len(result.InvalidEvents), result.Done) {
			shape[key] = value
		}
		c.trace(ctx, logging.Trace, "diagnostic.response_shape", shape)
	}()
	traceID := operationID(ctx)
	endLogical := func(outcome string, response StreamResponse, err error) {
		if logical != nil {
			fields := logging.Fields{"method": http.MethodPost, "path": requestPath, "stream": true, "status": response.StatusCode, "duration_ms": response.Duration.Milliseconds(), "event_count": len(response.Events)}
			if err != nil {
				fields["reason"] = errorClass(err)
			}
			logical.End(outcome, fields)
		}
	}
	c.trace(ctx, logging.Trace, "http.stream_logical_request.begin", logging.Fields{"method": http.MethodPost, "path": requestPath, "stream": true})
	var httpResp *http.Response
	var started time.Time
	var finalAttemptCancel context.CancelFunc
	var finalLease *scheduler.Lease
	attempts := 0
	for attempt := 0; attempt < 3; attempt++ {
		requestPath := safeRequestPath(&u)
		waitStarted := time.Now()
		c.trace(ctx, logging.Trace, "pacing.wait.begin", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "stream": true})
		if err := c.waitForPacing(ctx, model); err != nil {
			c.trace(ctx, logging.Debug, "pacing.wait.cancelled", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "stream": true, "wait_ms": time.Since(waitStarted).Milliseconds(), "reason": errorClass(err)})
			c.trace(ctx, logging.Debug, "http.stream_logical_request.result", logging.Fields{"method": http.MethodPost, "path": requestPath, "stream": true, "outcome": "pacing_cancelled", "reason": errorClass(err)})
			result := StreamResponse{TraceID: traceID}
			endLogical("pacing_cancelled", result, err)
			return result, err
		}
		c.trace(ctx, logging.Trace, "pacing.wait.end", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "stream": true, "wait_ms": time.Since(waitStarted).Milliseconds()})
		lease, err := c.acquireScheduler(ctx, int64(len(raw)))
		if err != nil {
			c.pacer.release(model)
			result := StreamResponse{TraceID: traceID}
			c.trace(ctx, logging.Debug, "scheduler.acquire.cancelled", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "stream": true, "reason": errorClass(err)})
			endLogical("scheduler_cancelled", result, err)
			return result, err
		}
		attemptCtx, cancelAttempt := attemptContext(ctx)
		attemptCtx, attemptOp := c.startOperation(attemptCtx, "http.stream_attempt", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "stream": true})
		started = time.Now()
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, u.String(), bytes.NewReader(raw))
		if err != nil {
			cancelAttempt()
			if lease != nil {
				lease.CancelBeforeDispatch()
			}
			c.pacer.release(model)
			fields := logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "stream": true, "duration_ms": time.Since(started).Milliseconds(), "reason": errorClass(err)}
			c.trace(attemptCtx, logging.Trace, "http.stream_attempt.result", fields)
			if attemptOp != nil {
				attemptOp.End("request_error", fields)
			}
			result := StreamResponse{TraceID: traceID}
			endLogical("request_error", result, err)
			return result, err
		}
		c.applyHeaders(req, "application/json")
		c.injectTraceContext(attemptCtx, req)
		req.Header.Set("Accept", "text/event-stream")
		if attempt > 0 {
			attempts++
			c.pacer.retry(model)
		}
		c.trace(attemptCtx, logging.Trace, "http.stream_attempt.begin", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "stream": true})
		httpResp, err = c.http.Do(req)
		if err != nil {
			cancelAttempt()
			if lease != nil {
				lease.Release()
			}
			c.pacer.release(model)
			fields := logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "stream": true, "duration_ms": time.Since(started).Milliseconds(), "reason": errorClass(err)}
			c.trace(attemptCtx, logging.Trace, "http.stream_attempt.result", fields)
			if attemptOp != nil {
				attemptOp.End("transport_error", fields)
			}
			if attempt == 2 {
				c.pacer.exhausted(model)
				c.trace(ctx, logging.Warn, "http.stream_retry.exhausted", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "stream": true, "reason": errorClass(err)})
				c.trace(ctx, logging.Debug, "http.stream_logical_request.result", logging.Fields{"method": http.MethodPost, "path": requestPath, "stream": true, "duration_ms": time.Since(started).Milliseconds(), "outcome": "transport_error", "reason": errorClass(err)})
				result := StreamResponse{Duration: time.Since(started), TraceID: traceID}
				endLogical("transport_error", result, err)
				return result, err
			}
			delay := time.Duration(1<<attempt) * 100 * time.Millisecond
			c.pacer.backoff(model, delay)
			c.trace(ctx, logging.Debug, "http.stream_retry.scheduled", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "next_attempt": attempt + 2, "stream": true, "reason": errorClass(err), "delay_ms": delay.Milliseconds()})
			select {
			case <-ctx.Done():
				c.trace(ctx, logging.Debug, "http.stream_retry.cancelled", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "stream": true, "reason": errorClass(ctx.Err())})
				c.trace(ctx, logging.Debug, "http.stream_logical_request.result", logging.Fields{"method": http.MethodPost, "path": requestPath, "stream": true, "duration_ms": time.Since(started).Milliseconds(), "outcome": "retry_cancelled", "reason": errorClass(ctx.Err())})
				err := ctx.Err()
				result := StreamResponse{Duration: time.Since(started), TraceID: traceID}
				endLogical("retry_cancelled", result, err)
				return result, err
			case <-time.After(delay):
			}
			continue
		}
		attemptFields := responseFields(http.MethodPost, requestPath, attempt+1, httpResp, nil, time.Since(started))
		c.trace(attemptCtx, logging.Trace, "http.stream_attempt.result", attemptFields)
		if attemptOp != nil {
			attemptOp.End("response", attemptFields)
		}
		if !shouldRetryStatus(httpResp.StatusCode, httpResp.Header) {
			finalAttemptCancel = cancelAttempt
			finalLease = lease
			break
		}
		delay := retryAfter(httpResp.Header)
		if delay <= 0 {
			delay = time.Duration(1<<attempt) * 100 * time.Millisecond
		}
		if httpResp.StatusCode == http.StatusTooManyRequests {
			c.applySchedulerRetryAfter(httpResp.Header)
		}
		if attempt == 2 {
			finalAttemptCancel = cancelAttempt
			finalLease = lease
			if httpResp.StatusCode == http.StatusTooManyRequests {
				c.pacer.limited(model, delay)
			}
			c.pacer.exhausted(model)
			c.trace(ctx, logging.Warn, "http.stream_retry.exhausted", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "status": httpResp.StatusCode, "reason": "retryable_status"})
			break
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResp.Body, 32<<10))
		httpResp.Body.Close()
		cancelAttempt()
		if lease != nil {
			lease.Release()
		}
		c.pacer.release(model)
		if httpResp.StatusCode == http.StatusTooManyRequests {
			c.pacer.limited(model, delay)
		} else {
			c.pacer.backoff(model, delay)
			c.trace(ctx, logging.Debug, "http.stream_retry.scheduled", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "next_attempt": attempt + 2, "status": httpResp.StatusCode, "reason": "retryable_status", "delay_ms": delay.Milliseconds()})
			select {
			case <-ctx.Done():
				c.trace(ctx, logging.Debug, "http.stream_retry.cancelled", logging.Fields{"method": http.MethodPost, "path": requestPath, "attempt": attempt + 1, "reason": errorClass(ctx.Err())})
				err := ctx.Err()
				result := StreamResponse{Duration: time.Since(started), TraceID: traceID}
				endLogical("retry_cancelled", result, err)
				return result, err
			case <-time.After(delay):
			}
		}
	}
	if finalAttemptCancel != nil {
		defer finalAttemptCancel()
	}
	if finalLease != nil {
		defer finalLease.Release()
	}
	defer c.pacer.release(model)
	defer httpResp.Body.Close()
	result = StreamResponse{
		StatusCode:  httpResp.StatusCode,
		Header:      httpResp.Header.Clone(),
		ContentType: httpResp.Header.Get("Content-Type"),
		RequestID:   requestID(httpResp.Header),
		TraceID:     traceID,
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(httpResp.Body, 32<<10))
		result.Duration = time.Since(started)
		result.RawSnippet = strings.TrimSpace(string(b))
		c.trace(ctx, logging.Debug, "http.stream_logical_request.result", logging.Fields{"method": http.MethodPost, "path": safeRequestPath(&u), "stream": true, "status": result.StatusCode, "duration_ms": result.Duration.Milliseconds(), "outcome": "http_error"})
		err := fmt.Errorf("stream request returned HTTP %d", httpResp.StatusCode)
		endLogical("http_error", result, err)
		return result, err
	}
	c.pacer.success(model, time.Since(started))
	if attempts > 0 {
		c.pacer.recovered(model)
	}

	limitedBody := &io.LimitedReader{R: httpResp.Body, N: maxResponseBody + 1}
	reader := bufio.NewReaderSize(limitedBody, 64*1024)
	var event SSEEvent
	var dataLines []string
	var snippet strings.Builder
	eventIndex := 0
	flush := func() {
		if len(dataLines) == 0 && event.Event == "" && event.ID == "" {
			return
		}
		event.Data = strings.Join(dataLines, "\n")
		if event.Data == "[DONE]" {
			result.Done = true
		} else if event.Data != "" {
			dec := json.NewDecoder(strings.NewReader(event.Data))
			dec.UseNumber()
			if err := dec.Decode(&event.JSON); err != nil {
				result.InvalidEvents = append(result.InvalidEvents, truncate(event.Data, 512)+": "+err.Error())
			} else {
				consumeStreamJSON(&result, event.JSON)
			}
		}
		eventIndex++
		kind := "empty"
		if event.Data == "[DONE]" {
			kind = "done"
		} else if event.JSON != nil {
			kind = "json"
		} else if event.Data != "" {
			kind = "invalid"
		}
		c.trace(ctx, logging.Trace, "http.sse.event", logging.Fields{"method": http.MethodPost, "path": safeRequestPath(&u), "stream": true, "event_index": eventIndex, "has_event_id": event.ID != "", "data_kind": kind, "json_key_count": len(event.JSON)})
		if len(result.Events) < 2000 {
			result.Events = append(result.Events, event)
		}
		if snippet.Len() < 8192 {
			snippet.WriteString("data: ")
			snippet.WriteString(truncate(event.Data, 512))
			snippet.WriteByte('\n')
		}
		event = SSEEvent{}
		dataLines = nil
	}

	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			switch {
			case line == "":
				flush()
			case strings.HasPrefix(line, ":"):
				// SSE comment / heartbeat.
			case strings.HasPrefix(line, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			case strings.HasPrefix(line, "event:"):
				event.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "id:"):
				event.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			default:
				result.InvalidEvents = append(result.InvalidEvents, "invalid SSE line: "+truncate(line, 256))
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				flush()
				if limitedBody.N <= 0 {
					result.Duration = time.Since(started)
					result.RawSnippet = snippet.String()
					c.trace(ctx, logging.Debug, "http.stream_logical_request.result", logging.Fields{"method": http.MethodPost, "path": safeRequestPath(&u), "stream": true, "status": result.StatusCode, "duration_ms": result.Duration.Milliseconds(), "outcome": "size_limit", "event_count": eventIndex})
					err := fmt.Errorf("stream response exceeds %d bytes", maxResponseBody)
					endLogical("size_limit", result, err)
					return result, err
				}
				break
			}
			result.Duration = time.Since(started)
			result.RawSnippet = snippet.String()
			c.trace(ctx, logging.Debug, "http.stream_logical_request.result", logging.Fields{"method": http.MethodPost, "path": safeRequestPath(&u), "stream": true, "status": result.StatusCode, "duration_ms": result.Duration.Milliseconds(), "outcome": "read_error", "reason": errorClass(readErr), "event_count": eventIndex})
			err := fmt.Errorf("read SSE stream: %w", readErr)
			endLogical("read_error", result, err)
			return result, err
		}
	}
	result.Duration = time.Since(started)
	result.RawSnippet = snippet.String()
	c.trace(ctx, logging.Debug, "http.stream_logical_request.result", logging.Fields{"method": http.MethodPost, "path": safeRequestPath(&u), "stream": true, "status": result.StatusCode, "duration_ms": result.Duration.Milliseconds(), "outcome": "success", "event_count": eventIndex, "done": result.Done, "invalid_event_count": len(result.InvalidEvents)})
	endLogical("success", result, nil)
	return result, nil
}

func shouldRetry(resp Response, err error) bool {
	if err != nil && resp.StatusCode == 0 {
		return true
	}
	return shouldRetryStatus(resp.StatusCode, http.Header(resp.Header))
}

func safeEndpointPath(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "[invalid-path]"
	}
	return safeRequestPath(u)
}

func safeRequestPath(u *url.URL) string {
	if u == nil {
		return "/"
	}
	path := u.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

func responseFields(method, path string, attempt int, response *http.Response, err error, duration time.Duration) logging.Fields {
	fields := logging.Fields{"method": method, "path": path, "attempt": attempt, "duration_ms": duration.Milliseconds()}
	if response != nil {
		fields["status"] = response.StatusCode
	}
	if err != nil {
		fields["reason"] = errorClass(err)
	}
	return fields
}

func rawResponseFields(method, path string, attempt int, response Response, err error) logging.Fields {
	fields := logging.Fields{"method": method, "path": path, "attempt": attempt, "status": response.StatusCode, "duration_ms": response.Duration.Milliseconds()}
	if err != nil {
		fields["reason"] = errorClass(err)
	}
	return fields
}

func retryReason(response *http.Response, err error) string {
	if err != nil {
		return errorClass(err)
	}
	if response == nil {
		return "no_response"
	}
	return "http_" + strconv.Itoa(response.StatusCode)
}

func retryReasonResponse(response Response, err error) string {
	if err != nil {
		return errorClass(err)
	}
	return "http_" + strconv.Itoa(response.StatusCode)
}

func errorClass(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	default:
		return fmt.Sprintf("%T", err)
	}
}

func shouldRetryStatus(status int, headers http.Header) bool {
	if strings.EqualFold(strings.TrimSpace(headers.Get("x-should-retry")), "false") {
		return false
	}
	switch status {
	case 408, 429, 500, 502, 503, 504, 529:
		return true
	}
	return false
}

func retryDelay(resp Response, attempt int) time.Duration {
	if delay := retryAfter(http.Header(resp.Header)); delay > 0 {
		return delay
	}
	return time.Duration(1<<attempt) * 100 * time.Millisecond
}

func modelFromBody(body any) string {
	if m, ok := body.(map[string]any); ok {
		model, _ := m["model"].(string)
		return model
	}
	return ""
}

func modelFromRaw(raw []byte) string {
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return modelFromBody(body)
}

func (c *Client) applyHeaders(req *http.Request, contentType string) {
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for name, values := range c.headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
}

func joinURLPath(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func requestID(h http.Header) string {
	for _, name := range []string{"request-id", "x-request-id", "openai-request-id", "x-correlation-id", "cf-ray"} {
		if value := h.Get(name); value != "" {
			return value
		}
	}
	return ""
}

func consumeStreamJSON(out *StreamResponse, root map[string]any) {
	if u := ParseUsage(root); u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
		out.Usage = u
	}
	choices, _ := root["choices"].([]any)
	for _, raw := range choices {
		choice, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if reason, ok := choice["finish_reason"].(string); ok && reason != "" {
			out.FinishReasons = append(out.FinishReasons, reason)
		}
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		out.Content += contentString(delta["content"])
		if calls, ok := delta["tool_calls"].([]any); ok {
			for _, call := range calls {
				if m, ok := call.(map[string]any); ok {
					mergeStreamToolCall(out, m)
				}
			}
		}
	}
}

func mergeStreamToolCall(out *StreamResponse, delta map[string]any) {
	index := int(int64Value(delta["index"]))
	if index < 0 {
		index = len(out.ToolCalls)
	}
	for len(out.ToolCalls) <= index {
		out.ToolCalls = append(out.ToolCalls, map[string]any{"index": len(out.ToolCalls)})
	}
	target := out.ToolCalls[index]
	for _, key := range []string{"id", "type"} {
		if value, exists := delta[key]; exists && value != nil && stringValue(value) != "" {
			target[key] = value
		}
	}
	functionDelta, _ := delta["function"].(map[string]any)
	if functionDelta == nil {
		return
	}
	function, _ := target["function"].(map[string]any)
	if function == nil {
		function = map[string]any{}
		target["function"] = function
	}
	if name := stringValue(functionDelta["name"]); name != "" {
		function["name"] = name
	}
	if args, exists := functionDelta["arguments"]; exists {
		function["arguments"] = stringValue(function["arguments"]) + stringValue(args)
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
