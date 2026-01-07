package env

// ParseSlice parses the specified slice and transforms it into an environment
// map.
func ParseSlice(s []string) (map[string]string, error) {
	env := make(map[string]string, len(s))
	for _, e := range s {
		if len(e) > 0 && e[0] == '=' {
			// The legacy Windows CMD command interpreter uses a hack, where to emulate
			// DOS semantics, it uses an illegal (by windows definition) env name for
			// state storage to avoid conflicting with user defined env names. This is
			// used to preserve drive letter paths. E.g., typing c: from another drive
			// will remember the last CWD because CMD stores it in an env named "=C:".
			// Since these are illegal, they are filtered from standard user access but
			// are still available in the underlying win32 API calls. Since they have
			// zero value to a container, we filter as well.
			continue
		}

		if err := parseEnv(env, e); err != nil {
			return nil, err
		}
	}
	return env, nil
}
