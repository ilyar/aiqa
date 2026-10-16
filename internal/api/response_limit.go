package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

type cappedReadCloser struct {
	body      io.ReadCloser
	remaining int64
	exceeded  bool
}

func (r *cappedReadCloser) Read(p []byte) (int, error) {
	if r.exceeded {
		return 0, fmt.Errorf("%w: response body exceeds configured limit", ErrWorkLimit)
	}
	if int64(len(p)) > r.remaining+1 {
		p = p[:r.remaining+1]
	}
	n, err := r.body.Read(p)
	r.remaining -= int64(n)
	if r.remaining < 0 {
		r.exceeded = true
		return n, fmt.Errorf("%w: response body exceeds configured limit", ErrWorkLimit)
	}
	return n, err
}
func (r *cappedReadCloser) Close() error { return r.body.Close() }

func capResponseBody(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	return &cappedReadCloser{body: body, remaining: responseBodyLimit(ctx)}
}

// preflightSDKInventory reads a capped inventory response, validates bounded
// JSON structure before an SDK decoder allocates it, then gives the SDK a fresh
// byte reader for normal typed decoding.
func preflightSDKInventory(ctx context.Context, response *http.Response) error {
	if _, enabled := jsonCollectionLimit(ctx); !enabled || response == nil || response.Body == nil {
		return nil
	}
	limit := responseBodyLimit(ctx)
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	_ = response.Body.Close()
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit {
		return fmt.Errorf("%w: response body exceeds configured limit", ErrWorkLimit)
	}
	if _, err := decodeResponseJSON(ctx, raw); err != nil {
		return fmt.Errorf("decode SDK inventory: %w", err)
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
