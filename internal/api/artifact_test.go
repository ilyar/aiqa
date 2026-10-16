package api

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestArtifactURLRedactionRemovesSignedQuery(t *testing.T) {
	err := redactURLError(errors.New("GET https://cdn.example.test/image.png?signature=super-secret&expires=1: timeout"))
	if strings.Contains(err.Error(), "super-secret") || !strings.Contains(err.Error(), "?[redacted]") {
		t.Fatalf("unsafe redaction result: %q", err)
	}
}

func TestPublicIPRejectsNonPublicDestinations(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "192.0.0.11", "192.0.2.1", "198.18.0.1", "203.0.113.1", "::1", "::ffff:192.0.2.1", "64:ff9b::808:808", "100:0:0:1::1", "2001:db8::1", "2002::1", "fc00::1"} {
		if publicIP(net.ParseIP(raw)) {
			t.Fatalf("non-public address accepted: %s", raw)
		}
	}
	if !publicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address was rejected")
	}
}

func TestPublicIPRetainsIANAExceptionsAndGlobalUnicast(t *testing.T) {
	for _, raw := range []string{"8.8.8.8", "192.0.0.9", "192.0.0.10", "2001:1::1", "2001:3::1", "2001:20::1", "2606:4700:4700::1111"} {
		if !publicIP(net.ParseIP(raw)) {
			t.Fatalf("global address rejected: %s", raw)
		}
	}
}

func TestPrivateArtifactURLIsACompatibilityFailure(t *testing.T) {
	err := validatePublicURL(context.Background(), "http://127.0.0.1/signed?token=secret")
	if err == nil || ArtifactFetchInconclusive(err) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unexpected private-URL classification: %v", err)
	}
}

func TestArtifactFetchErrorsAreClassifiedAsInconclusive(t *testing.T) {
	err := inconclusiveArtifactError("artifact returned HTTP %d", 500)
	if !ArtifactFetchInconclusive(err) {
		t.Fatalf("provider error was not classified as inconclusive: %v", err)
	}
	if ArtifactFetchInconclusive(errors.New("artifact is malformed")) {
		t.Fatal("malformed artifact was classified as an infrastructure error")
	}
	if ArtifactFetchInconclusive(artifactPolicyViolation("private address")) {
		t.Fatal("unsafe artifact URL was classified as an infrastructure error")
	}
}
