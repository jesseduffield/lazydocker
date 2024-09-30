// Package ssh provides the connection helper for ssh:// URL.
package ssh

import (
	"net/url"

	"github.com/pkg/errors"
)

// ParseURL parses URL
func ParseURL(daemonURL string) (*Spec, error) {
	u, err := url.Parse(daemonURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ssh" {
		return nil, errors.Errorf("expected scheme ssh, got %q", u.Scheme)
	}

	var sp Spec

	if u.User != nil {
		sp.User = u.User.Username()
		if _, ok := u.User.Password(); ok {
			return nil, errors.New("plain-text password is not supported")
		}
	}
	sp.Host = u.Hostname()
	if sp.Host == "" {
		return nil, errors.Errorf("no host specified")
	}
	sp.Port = u.Port()
	sp.Path = u.Path
	if u.RawQuery != "" {
		return nil, errors.Errorf("extra query after the host: %q", u.RawQuery)
	}
	if u.Fragment != "" {
		return nil, errors.Errorf("extra fragment after the host: %q", u.Fragment)
	}
	return &sp, err
}

// Spec of SSH URL
type Spec struct {
	User string
	Host string
	Port string
	Path string
}

// Args returns args except "ssh" itself combined with optional additional command args
func (sp *Spec) Args(add ...string) []string {
	var args []string
	if sp.User != "" {
		args = append(args, "-l", sp.User)
	}
	if sp.Port != "" {
		args = append(args, "-p", sp.Port)
	}
	args = append(args, "--", sp.Host)
	args = append(args, add...)
	return args
}
