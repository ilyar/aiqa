package suites

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestImageSuiteValidatesArtifactWithoutReportingBase64(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeImageError(w, 400, "malformed JSON")
			return
		}
		model, _ := body["model"].(string)
		if model != "image-model" {
			writeImageError(w, 404, "unknown model")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"created": 1, "data": []any{map[string]any{"b64_json": png}}})
	}))
	defer server.Close()
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := api.NewClient(server.URL, "key", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	env := Environment{Client: client, Profile: p, Config: config.Config{Timeout: time.Second, MaxArtifactsPerModel: 1}}
	tests := RunImageCompat(context.Background(), env, "image-model")
	for _, test := range tests {
		if test.Required && test.Status != result.StatusPass {
			t.Fatalf("required image probe failed: %+v", test)
		}
		if strings.Contains(test.HTTP.Snippet, png) {
			t.Fatal("base64 artifact leaked into report snippet")
		}
	}
	var artifact result.TestResult
	for _, test := range tests {
		if test.ID == "images.generate.artifact" {
			artifact = test
		}
	}
	if len(artifact.Artifacts) != 1 || artifact.Artifacts[0].Width != 1 || artifact.Artifacts[0].Height != 1 || artifact.Artifacts[0].SHA256 == "" {
		t.Fatalf("invalid artifact observation: %+v", artifact)
	}
}

func TestImageGenerationDependentsStayBlockedAfterTransientFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeImageError(w, http.StatusServiceUnavailable, "temporarily unavailable")
	}))
	defer server.Close()
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := api.NewClient(server.URL, "key", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	env := Environment{Client: client, Profile: p, Config: config.Config{Timeout: time.Second, MaxArtifactsPerModel: 1}}
	tests := RunImageCompat(context.Background(), env, "image-model")
	byID := make(map[string]result.TestResult, len(tests))
	for _, test := range tests {
		byID[test.ID] = test
	}
	for _, item := range []struct {
		id      string
		blocked string
	}{
		{id: "images.generate.schema", blocked: "images.generate.basic"},
		{id: "images.generate.artifact", blocked: "images.generate.schema"},
		{id: "images.generate.usage", blocked: "images.generate.basic"},
	} {
		test, ok := byID[item.id]
		if !ok || test.Status != result.StatusSkip || len(test.Signals) != 1 || test.Signals[0].Parameters["blocked_by_requirement"] != item.blocked {
			t.Fatalf("%s was not blocked by %s: %+v", item.id, item.blocked, test)
		}
	}
}

func TestImageHTTPObservationRedactsSignedURL(t *testing.T) {
	const signedURL = "https://cdn.example.test/image.png?signature=super-secret&expires=999999"
	observation := imageHTTPObservation(api.Response{JSON: map[string]any{
		"data": []any{map[string]any{"url": signedURL, "revised_prompt": "safe"}},
	}})
	if strings.Contains(observation.Snippet, signedURL) || strings.Contains(observation.Snippet, "super-secret") {
		t.Fatalf("signed URL leaked into observation: %s", observation.Snippet)
	}
	if !strings.Contains(observation.Snippet, "[URL redacted]") {
		t.Fatalf("redaction marker is missing: %s", observation.Snippet)
	}
}

func TestWebPDimensionsSupportsLossyLosslessAndExtendedHeaders(t *testing.T) {
	makeHeader := func(chunk string) []byte {
		body := make([]byte, 30)
		copy(body[0:4], "RIFF")
		copy(body[8:12], "WEBP")
		copy(body[12:16], chunk)
		return body
	}

	lossy := makeHeader("VP8 ")
	copy(lossy[23:26], []byte{0x9d, 0x01, 0x2a})
	binary.LittleEndian.PutUint16(lossy[26:28], 320)
	binary.LittleEndian.PutUint16(lossy[28:30], 240)

	lossless := makeHeader("VP8L")
	lossless[20] = 0x2f
	binary.LittleEndian.PutUint32(lossless[21:25], uint32(319)|uint32(239)<<14)

	extended := makeHeader("VP8X")
	widthMinusOne, heightMinusOne := 319, 239
	extended[24], extended[25], extended[26] = byte(widthMinusOne), byte(widthMinusOne>>8), byte(widthMinusOne>>16)
	extended[27], extended[28], extended[29] = byte(heightMinusOne), byte(heightMinusOne>>8), byte(heightMinusOne>>16)

	for name, body := range map[string][]byte{"lossy": lossy, "lossless": lossless, "extended": extended} {
		width, height, err := webpDimensions(body)
		if err != nil || width != 320 || height != 240 {
			t.Fatalf("%s dimensions=%dx%d err=%v", name, width, height, err)
		}
	}
}

func TestImageMediaTypeUsesDetectedFormat(t *testing.T) {
	if got := mediaTypeForFormat("jpeg"); got != "image/jpeg" {
		t.Fatalf("JPEG media type=%q", got)
	}
	if got := mediaTypeForFormat("webp"); got != "image/webp" {
		t.Fatalf("WebP media type=%q", got)
	}
}

func writeImageError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": message, "type": "invalid_request_error"}})
}
