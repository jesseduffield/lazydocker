// most of these validate and parse functions have been taken from projectatomic/docker
// and modified for cri-o
package parse

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"go.podman.io/common/libnetwork/etchosts"
	"go.podman.io/storage/pkg/regexp"
)

const (
	LabelType string = "label"
	ENVType   string = "env"
)

// Note: for flags that are in the form <number><unit>, use the RAMInBytes function
// from the units package in docker/go-units/size.go

var (
	whiteSpaces  = " \t"
	alphaRegexp  = regexp.Delayed(`[a-zA-Z]`)
	domainRegexp = regexp.Delayed(`^(:?(:?[a-zA-Z0-9]|(:?[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9]))(:?\.(:?[a-zA-Z0-9]|(:?[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])))*)\.?\s*$`)
)

// ValidateExtraHost validates that the specified string is a valid extrahost and returns it.
// ExtraHost is in the form of name1;name2;name3:ip where the ip has to be a valid ip (ipv4 or ipv6) or the special string HostGateway.
// for add-host flag
func ValidateExtraHost(val string) (string, error) {
	// allow for IPv6 addresses in extra hosts by only splitting on first ":"
	names, ip, hasIP := strings.Cut(val, ":")
	if !hasIP || len(names) == 0 {
		return "", fmt.Errorf("bad format for add-host: %q", val)
	}

	// Split the hostnames by semicolon and validate each one
	for name := range strings.SplitSeq(names, ";") {
		if len(name) == 0 {
			return "", fmt.Errorf("hostname in add-host %q is empty", val)
		}
	}

	if ip == etchosts.HostGateway {
		return val, nil
	}
	if _, err := validateIPAddress(ip); err != nil {
		return "", fmt.Errorf("invalid IP address in add-host: %q", ip)
	}
	return val, nil
}

// validateIPAddress validates an Ip address.
// for dns, ip, and ip6 flags also
func validateIPAddress(val string) (string, error) {
	var ip = net.ParseIP(strings.TrimSpace(val))
	if ip != nil {
		return ip.String(), nil
	}
	return "", fmt.Errorf("%s is not an ip address", val)
}

func ValidateDomain(val string) (string, error) {
	if alphaRegexp.FindString(val) == "" {
		return "", fmt.Errorf("%s is not a valid domain", val)
	}
	ns := domainRegexp.FindSubmatch([]byte(val))
	if len(ns) > 0 && len(ns[1]) < 255 {
		return string(ns[1]), nil
	}
	return "", fmt.Errorf("%s is not a valid domain", val)
}

// GetAllLabels retrieves all labels given a potential label file and a number
// of labels provided from the command line.
func GetAllLabels(labelFile, inputLabels []string) (map[string]string, error) {
	labels := make(map[string]string)
	for _, file := range labelFile {
		// Use of parseEnvFile still seems safe, as it's missing the
		// extra parsing logic of parseEnv.
		// There's an argument that we SHOULD be doing that parsing for
		// all environment variables, even those sourced from files, but
		// that would require a substantial rework.
		if err := parseEnvOrLabelFile(labels, file, LabelType); err != nil {
			return nil, err
		}
	}
	for _, label := range inputLabels {
		key, value, _ := strings.Cut(label, "=")
		if key == "" {
			return nil, fmt.Errorf("invalid label format: %q", label)
		}
		labels[key] = value
	}
	return labels, nil
}

func parseEnvOrLabel(env map[string]string, line, configType string) error {
	key, val, hasVal := strings.Cut(line, "=")

	// catch invalid variables such as "=" or "=A"
	if key == "" {
		return fmt.Errorf("invalid environment variable: %q", line)
	}

	// trim the front of a variable, but nothing else
	name := strings.TrimLeft(key, whiteSpaces)
	if strings.ContainsAny(name, whiteSpaces) {
		return fmt.Errorf("name %q has white spaces, poorly formatted name", name)
	}

	if hasVal {
		env[name] = val
	} else {
		if name, hasStar := strings.CutSuffix(name, "*"); hasStar {
			for _, e := range os.Environ() {
				envKey, envVal, hasEq := strings.Cut(e, "=")
				if hasEq && strings.HasPrefix(envKey, name) {
					env[envKey] = envVal
				}
			}
		} else if configType == ENVType {
			// if only a pass-through variable is given, clean it up.
			if val, ok := os.LookupEnv(name); ok {
				env[name] = val
			}
		}
	}
	return nil
}

// parseEnvOrLabelFile reads a file with environment variables enumerated by lines
// configType should be set to either "label" or "env" based on what type is being parsed
func parseEnvOrLabelFile(envOrLabel map[string]string, filename, configType string) error {
	fh, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)
	for scanner.Scan() {
		// trim the line from all leading whitespace first
		line := strings.TrimLeft(scanner.Text(), whiteSpaces)
		// line is not empty, and not starting with '#'
		if len(line) > 0 && !strings.HasPrefix(line, "#") {
			if err := parseEnvOrLabel(envOrLabel, line, configType); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// ValidWebURL checks a string urlStr is a url or not
func ValidWebURL(urlStr string) error {
	parsedURL, err := url.ParseRequestURI(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", urlStr, err)
	}

	// to be a valid web url, scheme must be either http or https
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("invalid URL %q: unsupported scheme %q", urlStr, parsedURL.Scheme)
	}

	// ensure url contain a host
	if parsedURL.Host == "" {
		return fmt.Errorf("invalid URL %q: missing host", urlStr)
	}
	return nil
}
