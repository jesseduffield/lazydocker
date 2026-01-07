package internal

import "strings"

type EnvironmentSlice []string

func (e EnvironmentSlice) Keys() []string {
	keys := make([]string, 0, len(e))
	for _, kv := range e {
		k, _, _ := strings.Cut(kv, "=")
		keys = append(keys, k)
	}
	return keys
}

func (e EnvironmentSlice) Get(key string) (string, bool) {
	for _, kv := range e {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}
