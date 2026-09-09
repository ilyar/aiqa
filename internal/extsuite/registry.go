package extsuite

import (
	"fmt"
	"sort"
	"sync"
)

type Registry struct {
	byID        map[string]Extension
	unsafeLocks *unsafeInvocationLocks
}

func NewRegistry(extensions ...Extension) (*Registry, error) {
	// A registry is constructed once for an application run.  Keep unsafe
	// executable serialization here, rather than in a target invocation, so
	// separately frozen targets in the same run share the same lock scope.
	r := &Registry{byID: map[string]Extension{}, unsafeLocks: &unsafeInvocationLocks{byExecutable: map[string]*sync.Mutex{}}}
	for _, extension := range extensions {
		if err := r.Register(extension); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Registry) Register(extension Extension) error {
	if extension == nil {
		return fmt.Errorf("cannot register a nil extension")
	}
	manifest := extension.Manifest()
	if err := ValidateManifest(&manifest); err != nil {
		return err
	}
	if _, exists := r.byID[manifest.Extension.ID]; exists {
		return fmt.Errorf("duplicate extension id %q", manifest.Extension.ID)
	}
	r.byID[manifest.Extension.ID] = extension
	return nil
}

func (r *Registry) Get(id string) (Extension, bool) {
	extension, ok := r.byID[id]
	return extension, ok
}

func (r *Registry) All() []Extension {
	ids := make([]string, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Extension, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.byID[id])
	}
	return out
}
