//go:build !windows

package env

// ParseSlice parses the specified slice and transforms it into an environment
// map.
func ParseSlice(s []string) (map[string]string, error) {
	env := make(map[string]string, len(s))
	for _, e := range s {
		if err := parseEnv(env, e); err != nil {
			return nil, err
		}
	}
	return env, nil
}
