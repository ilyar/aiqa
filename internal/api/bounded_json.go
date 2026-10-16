package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

const maxJSONNesting = 64

type jsonCollectionLimitKey struct{}

// WithJSONCollectionLimit bounds every decoded JSON object and array before
// allocating its next member. It is intended for untrusted inventories; an
// invalid/non-positive value uses the conservative discovery default.
func WithJSONCollectionLimit(ctx context.Context, max int) context.Context {
	if max <= 0 {
		max = 4096
	}
	return context.WithValue(ctx, jsonCollectionLimitKey{}, max)
}

func jsonCollectionLimit(ctx context.Context) (int, bool) {
	limit, ok := ctx.Value(jsonCollectionLimitKey{}).(int)
	return limit, ok && limit > 0
}

func decodeResponseJSON(ctx context.Context, raw []byte) (map[string]any, error) {
	limit, bounded := jsonCollectionLimit(ctx)
	if !bounded {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var root map[string]any
		if err := dec.Decode(&root); err != nil {
			return nil, err
		}
		return root, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	root, err := decodeInventoryRoot(dec, limit)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected trailing JSON value")
	}
	return root, nil
}

func decodeInventoryRoot(dec *json.Decoder, inventoryLimit int) (map[string]any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, fmt.Errorf("JSON response is not an object")
	}
	out := make(map[string]any)
	for dec.More() {
		if len(out) >= 4096 {
			return nil, fmt.Errorf("%w: JSON object members", ErrWorkLimit)
		}
		keyToken, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("JSON object key is not a string")
		}
		var value any
		if key == "data" || key == "models" {
			value, err = decodeBoundedArray(dec, 1, inventoryLimit, 4096)
		} else {
			value, err = decodeBoundedJSONValue(dec, 1, 4096)
		}
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeBoundedArray(dec *json.Decoder, depth, limit, nestedLimit int) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('[') {
		return token, nil
	}
	out := make([]any, 0, minJSONCapacity(limit))
	for dec.More() {
		if len(out) >= limit {
			return nil, fmt.Errorf("%w: inventory collection members", ErrWorkLimit)
		}
		item, err := decodeBoundedJSONValue(dec, depth+1, nestedLimit)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeBoundedJSONValue(dec *json.Decoder, depth, limit int) (any, error) {
	if depth > maxJSONNesting {
		return nil, fmt.Errorf("%w: JSON nesting depth", ErrWorkLimit)
	}
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			out := make(map[string]any)
			for dec.More() {
				if len(out) >= limit {
					return nil, fmt.Errorf("%w: JSON object members", ErrWorkLimit)
				}
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("JSON object key is not a string")
				}
				item, err := decodeBoundedJSONValue(dec, depth+1, limit)
				if err != nil {
					return nil, err
				}
				out[key] = item
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return out, nil
		case '[':
			out := make([]any, 0, minJSONCapacity(limit))
			for dec.More() {
				if len(out) >= limit {
					return nil, fmt.Errorf("%w: JSON array members", ErrWorkLimit)
				}
				item, err := decodeBoundedJSONValue(dec, depth+1, limit)
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return out, nil
		}
	}
	return token, nil
}

func minJSONCapacity(limit int) int {
	if limit < 16 {
		return limit
	}
	return 16
}
