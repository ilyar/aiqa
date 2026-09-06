package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/telemetry"
)

type Artifact struct {
	Body        []byte
	ContentType string
}

type artifactFetchError struct{ err error }

func (e artifactFetchError) Error() string { return e.err.Error() }
func (e artifactFetchError) Unwrap() error { return e.err }

// ArtifactFetchInconclusive reports whether artifact validation was blocked by
// transport or provider availability rather than disproving the artifact.
func ArtifactFetchInconclusive(err error) bool {
	var target artifactFetchError
	return errors.As(err, &target)
}

func inconclusiveArtifactError(format string, args ...any) error {
	return artifactFetchError{err: fmt.Errorf(format, args...)}
}

type artifactPolicyError struct{ err error }

func (e artifactPolicyError) Error() string { return e.err.Error() }
func (e artifactPolicyError) Unwrap() error { return e.err }

func artifactPolicyViolation(format string, args ...any) error {
	return artifactPolicyError{err: fmt.Errorf(format, args...)}
}

func FetchPublicArtifact(ctx context.Context, rawURL string, maxBytes int64) (Artifact, error) {
	return FetchPublicArtifactWithTelemetry(ctx, nil, rawURL, maxBytes)
}

// FetchPublicArtifactWithTelemetry records the validation download without
// retaining its URL, redirect targets, or bytes in the diagnostic trace.
func FetchPublicArtifactWithTelemetry(ctx context.Context, provider *telemetry.Provider, rawURL string, maxBytes int64) (artifact Artifact, err error) {
	var logical *telemetry.Operation
	if provider != nil {
		ctx, logical = provider.Start(ctx, "artifact.logical_request", logging.Fields{"method": http.MethodGet})
		defer func() {
			outcome := "success"
			if err != nil {
				outcome = "error"
			}
			logical.End(outcome, logging.Fields{"error_present": err != nil})
		}()
	}
	if maxBytes <= 0 {
		return Artifact{}, fmt.Errorf("artifact size limit must be positive")
	}
	if err := validatePublicURL(ctx, rawURL); err != nil {
		return Artifact{}, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.DialContext = publicDialContext
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return artifactPolicyViolation("artifact redirect limit exceeded")
			}
			return validatePublicURL(req.Context(), req.URL.String())
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Artifact{}, fmt.Errorf("create artifact request: %w", err)
	}
	var attempt *telemetry.Operation
	if provider != nil {
		_, attempt = provider.Start(ctx, "artifact.attempt", logging.Fields{"method": http.MethodGet, "attempt": 1})
	}
	resp, err := client.Do(req)
	if attempt != nil {
		fields := logging.Fields{"error_present": err != nil}
		if resp != nil {
			fields["http_status"] = resp.StatusCode
		}
		attempt.End("response", fields)
	}
	if err != nil {
		var policy artifactPolicyError
		if errors.As(err, &policy) {
			return Artifact{}, fmt.Errorf("fetch artifact: %w", policy)
		}
		return Artifact{}, inconclusiveArtifactError("fetch artifact: %w", redactURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return Artifact{}, inconclusiveArtifactError("artifact returned HTTP %d", resp.StatusCode)
		}
		return Artifact{}, fmt.Errorf("artifact returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return Artifact{}, inconclusiveArtifactError("read artifact: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return Artifact{}, fmt.Errorf("artifact exceeds %d bytes", maxBytes)
	}
	return Artifact{Body: body, ContentType: resp.Header.Get("Content-Type")}, nil
}

func validatePublicURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return artifactPolicyViolation("artifact URL must be absolute HTTP(S)")
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return inconclusiveArtifactError("resolve artifact host: %w", err)
	}
	if len(addresses) == 0 {
		return inconclusiveArtifactError("artifact host has no addresses")
	}
	for _, address := range addresses {
		if !publicIP(address.IP) {
			return artifactPolicyViolation("artifact URL resolves to a non-public address")
		}
	}
	return nil
}

func publicDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse artifact address: %w", err)
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact host: %w", err)
	}
	dialer := net.Dialer{Timeout: 15 * time.Second}
	for _, candidate := range addresses {
		if !publicIP(candidate.IP) {
			continue
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
	}
	return nil, fmt.Errorf("artifact host has no public address")
}

func publicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}

func redactURLError(err error) error {
	text := err.Error()
	if index := strings.Index(text, "?"); index >= 0 {
		text = text[:index] + "?[redacted]"
	}
	return fmt.Errorf("%s", text)
}
