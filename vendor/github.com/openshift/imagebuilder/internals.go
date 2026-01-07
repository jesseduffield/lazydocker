package imagebuilder

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// hasEnvName returns true if the provided environment contains the named ENV var.
func hasEnvName(env []string, name string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, name+"=") {
			return true
		}
	}
	return false
}

// platformSupports is a short-term function to give users a quality error
// message if a Dockerfile uses a command not supported on the platform.
func platformSupports(command string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	switch command {
	case "expose", "user", "stopsignal", "arg":
		return fmt.Errorf("The daemon on this platform does not support the command '%s'", command)
	}
	return nil
}

func handleJSONArgs(args []string, attributes map[string]bool) []string {
	if len(args) == 0 {
		return []string{}
	}

	if attributes != nil && attributes["json"] {
		return args
	}

	// literal string command, not an exec array
	return []string{strings.Join(args, " ")}
}

func hasSlash(input string) bool {
	return strings.HasSuffix(input, string(os.PathSeparator)) || strings.HasSuffix(input, string(os.PathSeparator)+".")
}

// makeAbsolute ensures that the provided path is absolute.
func makeAbsolute(dest, workingDir string) string {
	// Twiddle the destination when it's a relative path - meaning, make it
	// relative to the WORKINGDIR
	if dest == "." {
		if !hasSlash(workingDir) {
			workingDir += string(os.PathSeparator)
		}
		dest = workingDir
	}

	if !filepath.IsAbs(dest) {
		hasSlash := hasSlash(dest)
		dest = filepath.Join(string(os.PathSeparator), filepath.FromSlash(workingDir), dest)

		// Make sure we preserve any trailing slash
		if hasSlash {
			dest += string(os.PathSeparator)
		}
	}
	return dest
}

// parseOptInterval(flag) is the duration of flag.Value, or 0 if
// empty. An error is reported if the value is given and is not positive.
func parseOptInterval(f *flag.Flag) (time.Duration, error) {
	if f == nil {
		return 0, fmt.Errorf("No flag defined")
	}
	s := f.Value.String()
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("Interval %#v must be positive", f.Name)
	}
	return d, nil
}

// mergeEnv merges two lists of environment variables, avoiding duplicates.
func mergeEnv(defaults, overrides []string) []string {
	s := make([]string, 0, len(defaults)+len(overrides))
	index := make(map[string]int)
	for _, envSpec := range append(defaults, overrides...) {
		envVar := strings.SplitN(envSpec, "=", 2)
		if i, ok := index[envVar[0]]; ok {
			s[i] = envSpec
			continue
		}
		s = append(s, envSpec)
		index[envVar[0]] = len(s) - 1
	}
	return s
}

// envMapAsSlice returns the contents of a map[string]string as a slice of keys
// and values joined with "=".
func envMapAsSlice(m map[string]string) []string {
	s := make([]string, 0, len(m))
	for k, v := range m {
		s = append(s, k+"="+v)
	}
	return s
}
