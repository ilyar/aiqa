package suites

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/result"
)

const maxImageArtifactBytes = 20 << 20

// ImageCompatProbeCount counts the lifecycle probes started by RunImageCompat.
const ImageCompatProbeCount = 3

func RunImageCompat(parent context.Context, env Environment, model string) []result.TestResult {
	parent, suiteOp := env.startSuite(parent, "compat-images", logging.Fields{"model": model})
	defer endOperation(suiteOp, "completed", nil)
	groups := probe.Map(env.Probes,
		func() []result.TestResult {
			return env.runTestsProbe(parent, model, "compat-images", "images.generate.basic", func(probeCtx context.Context) []result.TestResult {
				return probeImageGeneration(probeCtx, env, model)
			})
		},
		func() []result.TestResult {
			return []result.TestResult{env.runTestProbe(parent, model, "compat-images", "images.generate.invalid_model", func(probeCtx context.Context) result.TestResult {
				return probeInvalidImageModel(probeCtx, env)
			})}
		},
		func() []result.TestResult {
			return []result.TestResult{env.runTestProbe(parent, model, "compat-images", "images.generate.invalid_json", func(probeCtx context.Context) result.TestResult {
				return probeInvalidImageJSON(probeCtx, env)
			})}
		},
	)
	out := flattenTests(groups)
	env.traceTests(parent, model, out)
	return out
}

func probeImageGeneration(parent context.Context, env Environment, model string) []result.TestResult {
	ctx, cancel := env.probeContext(parent)
	resp, err := env.Client.ImagesGenerate(ctx, map[string]any{
		"model":  model,
		"prompt": "A single blue square centered on a plain white background.",
	})
	cancel()
	basic := testBase(env.Profile, "images.generate.basic", "compat")
	basic.DurationMS = resp.Duration.Milliseconds()
	basic.HTTP = imageHTTPObservation(resp)
	if err != nil && resp.StatusCode == 0 {
		basic.Status, basic.Summary, basic.Error = result.StatusError, "image generation failed before an HTTP response", errorText(err)
	} else if providerFailureStatusCode(resp.StatusCode) {
		basic.Status, basic.Summary, basic.Error = result.StatusError, providerFailureSummary("image generation", resp.StatusCode), errorText(err)
	} else if !resp.Is2xx() {
		basic.Status, basic.Summary, basic.Error = result.StatusFail, fmt.Sprintf("image generation returned HTTP %d", resp.StatusCode), errorText(err)
	} else if resp.JSON == nil {
		basic.Status, basic.Summary = result.StatusFail, "image generation did not return a JSON object"
	} else {
		basic.Status, basic.Summary = result.StatusPass, "minimal image generation request succeeded"
	}

	schema := testBase(env.Profile, "images.generate.schema", "compat")
	schema.DerivedFrom = basic.ID
	schema.DurationMS, schema.HTTP = resp.Duration.Milliseconds(), imageHTTPObservation(resp)
	var item map[string]any
	if basic.Status != result.StatusPass {
		schema.Status, schema.Summary = result.StatusSkip, "schema was not checked because image generation failed"
		schema.Signals = []result.EvidenceSignal{{
			Code: "blocked.images.generate.schema", RootCause: "blocked-prerequisite",
			Parameters: map[string]any{"blocked_by_requirement": "images.generate.basic"},
		}}
	} else if data, ok := resp.JSON["data"].([]any); !ok || len(data) == 0 {
		schema.Status, schema.Summary = result.StatusFail, "image response data is missing, not an array, or empty"
	} else if item, ok = data[0].(map[string]any); !ok {
		schema.Status, schema.Summary = result.StatusFail, "image response data[0] is not an object"
	} else {
		schema.Status, schema.Summary = result.StatusPass, "image response contains a non-empty data array"
	}

	artifact := testBase(env.Profile, "images.generate.artifact", "compat")
	artifact.DerivedFrom = basic.ID
	artifact.DurationMS, artifact.HTTP = resp.Duration.Milliseconds(), imageHTTPObservation(resp)
	if schema.Status != result.StatusPass {
		artifact.Status, artifact.Summary = result.StatusSkip, "artifact was not checked because image response schema failed"
		artifact.Signals = []result.EvidenceSignal{{
			Code: "blocked.images.generate.artifact", RootCause: "blocked-prerequisite",
			Parameters: map[string]any{"blocked_by_requirement": "images.generate.schema"},
		}}
	} else {
		artifactCtx, artifactCancel := env.probeContext(parent)
		observation, artifactErr := inspectImageArtifact(artifactCtx, env, item)
		artifactCancel()
		if artifactErr != nil {
			if api.ArtifactFetchInconclusive(artifactErr) {
				artifact.Status, artifact.Summary, artifact.Error = result.StatusError, "image artifact could not be fetched; compatibility outcome is inconclusive", artifactErr.Error()
			} else {
				artifact.Status, artifact.Summary, artifact.Error = result.StatusFail, "image response did not contain a valid image artifact", artifactErr.Error()
			}
		} else {
			artifact.Status, artifact.Summary = result.StatusPass, fmt.Sprintf("%s %dx%d image artifact validated", strings.ToUpper(observation.Format), observation.Width, observation.Height)
			artifact.Artifacts = []result.ArtifactObservation{observation}
		}
	}

	usage := testBase(env.Profile, "images.generate.usage", "compat")
	usage.DerivedFrom = basic.ID
	usage.DurationMS, usage.HTTP, usage.Usage = resp.Duration.Milliseconds(), imageHTTPObservation(resp), api.ParseUsage(resp.JSON)
	if basic.Status != result.StatusPass {
		usage.Status, usage.Summary = result.StatusSkip, "usage was not checked because image generation failed"
		usage.Signals = []result.EvidenceSignal{{
			Code: "blocked.images.generate.usage", RootCause: "blocked-prerequisite",
			Parameters: map[string]any{"blocked_by_requirement": "images.generate.basic"},
		}}
	} else if usage.Usage.TotalTokens > 0 {
		usage.Status, usage.Summary = result.StatusPass, "image generation reported token usage"
	} else {
		usage.Status, usage.Summary = result.StatusWarn, "image generation succeeded without token usage metadata"
	}

	return []result.TestResult{basic, schema, artifact, usage}
}

func probeInvalidImageModel(parent context.Context, env Environment) result.TestResult {
	t := testBase(env.Profile, "images.generate.invalid_model", "compat")
	ctx, cancel := env.probeContext(parent)
	defer cancel()
	resp, err := env.Client.ImagesGenerate(ctx, map[string]any{"model": fmt.Sprintf("__aiqa_missing_image_model_%d__", time.Now().UnixNano()), "prompt": "test"})
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), imageHTTPObservation(resp)
	return evaluateErrorProbe(t, resp, err, []int{400, 404, 422}, "invalid image model")
}

func probeInvalidImageJSON(parent context.Context, env Environment) result.TestResult {
	t := testBase(env.Profile, "images.generate.invalid_json", "compat")
	ctx, cancel := env.probeContext(parent)
	defer cancel()
	resp, err := env.Client.ImagesRaw(ctx, []byte(`{"model":`), "application/json")
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), imageHTTPObservation(resp)
	return evaluateErrorProbe(t, resp, err, []int{400, 422}, "malformed image-generation JSON")
}

func imageHTTPObservation(resp api.Response) result.HTTPObservation {
	observation := httpObservation(resp)
	observation.Snippet = ""
	if resp.JSON == nil {
		return observation
	}
	copyRoot := map[string]any{}
	for key, value := range resp.JSON {
		copyRoot[key] = value
	}
	if data, ok := resp.JSON["data"].([]any); ok {
		redacted := make([]any, 0, len(data))
		for _, raw := range data {
			item, _ := raw.(map[string]any)
			clean := map[string]any{}
			for key, value := range item {
				if key != "b64_json" && key != "url" {
					clean[key] = value
				}
			}
			if _, ok := item["b64_json"]; ok {
				clean["artifact"] = "[base64 redacted]"
			}
			if _, ok := item["url"]; ok {
				clean["artifact"] = "[URL redacted]"
			}
			redacted = append(redacted, clean)
		}
		copyRoot["data"] = redacted
	}
	if encoded, err := json.Marshal(copyRoot); err == nil {
		observation.Snippet = truncate(string(encoded), 1200)
	}
	return observation
}

func inspectImageArtifact(ctx context.Context, env Environment, item map[string]any) (result.ArtifactObservation, error) {
	var body []byte
	source := ""
	mediaType := ""
	if encoded, _ := item["b64_json"].(string); encoded != "" {
		if len(encoded) > base64.StdEncoding.EncodedLen(maxImageArtifactBytes) {
			return result.ArtifactObservation{}, fmt.Errorf("base64 artifact exceeds size limit")
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return result.ArtifactObservation{}, fmt.Errorf("decode base64 artifact: %w", err)
		}
		if len(decoded) > maxImageArtifactBytes {
			return result.ArtifactObservation{}, fmt.Errorf("decoded artifact exceeds size limit")
		}
		body, source = decoded, "b64_json"
	} else if rawURL, _ := item["url"].(string); rawURL != "" {
		artifact, err := api.FetchPublicArtifactWithTelemetry(ctx, env.Telemetry, rawURL, maxImageArtifactBytes)
		if err != nil {
			return result.ArtifactObservation{}, err
		}
		body, mediaType, source = artifact.Body, artifact.ContentType, "url"
	} else {
		return result.ArtifactObservation{}, fmt.Errorf("data[0] has neither b64_json nor url")
	}
	if len(body) == 0 {
		return result.ArtifactObservation{}, fmt.Errorf("image artifact is empty")
	}
	format, width, height, err := imageInfo(body)
	if err != nil {
		return result.ArtifactObservation{}, err
	}
	detected := mediaTypeForFormat(format)
	if detected == "" {
		detected = http.DetectContentType(body)
	}
	if mediaType != "" {
		declared, _, parseErr := mime.ParseMediaType(mediaType)
		if parseErr != nil || !strings.EqualFold(declared, detected) {
			return result.ArtifactObservation{}, fmt.Errorf("artifact Content-Type %q does not match detected %s", mediaType, detected)
		}
	}
	mediaType = detected
	digest := sha256.Sum256(body)
	return result.ArtifactObservation{Kind: "image", Source: source, MediaType: mediaType, Format: format, Bytes: int64(len(body)), SHA256: hex.EncodeToString(digest[:]), Width: width, Height: height}, nil
}

func mediaTypeForFormat(format string) string {
	switch strings.ToLower(format) {
	case "png":
		return "image/png"
	case "jpeg", "jpg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}

func imageInfo(body []byte) (string, int, int, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err == nil {
		return format, config.Width, config.Height, nil
	}
	if len(body) >= 30 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WEBP" {
		width, height, parseErr := webpDimensions(body)
		return "webp", width, height, parseErr
	}
	return "", 0, 0, fmt.Errorf("unsupported or malformed image artifact")
}

func webpDimensions(body []byte) (int, int, error) {
	switch string(body[12:16]) {
	case "VP8 ":
		if len(body) < 30 || body[23] != 0x9d || body[24] != 0x01 || body[25] != 0x2a {
			break
		}
		width := int(binary.LittleEndian.Uint16(body[26:28]) & 0x3fff)
		height := int(binary.LittleEndian.Uint16(body[28:30]) & 0x3fff)
		if width == 0 || height == 0 {
			break
		}
		return width, height, nil
	case "VP8X":
		if len(body) < 30 {
			break
		}
		return 1 + int(body[24]) + int(body[25])<<8 + int(body[26])<<16, 1 + int(body[27]) + int(body[28])<<8 + int(body[29])<<16, nil
	case "VP8L":
		if len(body) < 25 || body[20] != 0x2f {
			break
		}
		bits := binary.LittleEndian.Uint32(body[21:25])
		return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1, nil
	}
	return 0, 0, fmt.Errorf("unsupported or malformed WebP header")
}
