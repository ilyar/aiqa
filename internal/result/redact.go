package result

import (
	"reflect"
	"sort"
	"strings"
)

// RedactSecrets replaces exact configured credentials in every exported string
// reachable from a report, including provider-supplied metadata and map keys.
// It is a final output boundary, not a substitute for avoiding secret capture.
func RedactSecrets(value any, secrets ...string) {
	unique := make([]string, 0, len(secrets))
	seen := map[string]bool{}
	for _, secret := range secrets {
		if secret != "" && !seen[secret] {
			seen[secret] = true
			unique = append(unique, secret)
		}
	}
	// Replace longer values first: replacing a configured prefix must not leave
	// the suffix of another configured credential visible.
	sort.SliceStable(unique, func(i, j int) bool { return len(unique[i]) > len(unique[j]) })
	if len(unique) == 0 {
		return
	}
	redactReflect(reflect.ValueOf(value), unique)
}

func redactReflect(value reflect.Value, secrets []string) {
	if !value.IsValid() {
		return
	}
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() {
			redactReflect(value.Elem(), secrets)
		}
	case reflect.Interface:
		if value.IsNil() || !value.CanSet() {
			return
		}
		copy := reflect.New(value.Elem().Type()).Elem()
		copy.Set(value.Elem())
		redactReflect(copy, secrets)
		value.Set(copy)
	case reflect.String:
		if value.CanSet() {
			value.SetString(redactedString(value.String(), secrets))
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if value.Field(index).CanSet() {
				redactReflect(value.Field(index), secrets)
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			redactReflect(value.Index(index), secrets)
		}
	case reflect.Map:
		if value.IsNil() || value.Type().Key().Kind() != reflect.String {
			return
		}
		for _, key := range value.MapKeys() {
			item := value.MapIndex(key)
			copy := reflect.New(item.Type()).Elem()
			copy.Set(item)
			redactReflect(copy, secrets)
			newKey := reflect.ValueOf(redactedString(key.String(), secrets)).Convert(value.Type().Key())
			if newKey.String() != key.String() {
				value.SetMapIndex(key, reflect.Value{})
			}
			value.SetMapIndex(newKey, copy)
		}
	}
}

func redactedString(value string, secrets []string) string {
	for _, secret := range secrets {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}
