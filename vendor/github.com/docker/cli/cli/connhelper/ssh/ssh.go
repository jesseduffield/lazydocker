// Package ssh provides the connection helper for ssh:// URL.
package ssh

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/docker/cli/cli/connhelper/internal/syntax"
)

// ParseURL creates a [Spec] from the given ssh URL. It returns an error if
// the URL is using the wrong scheme, contains fragments, query-parameters,
// or contains a password.
func ParseURL(daemonURL string) (*Spec, error) {
	u, err := url.Parse(daemonURL)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Unwrap()
		}
		return nil, fmt.Errorf("invalid SSH URL: %w", err)
	}
	return NewSpec(u)
}

// NewSpec creates a [Spec] from the given ssh URL's properties. It returns
// an error if the URL is using the wrong scheme, contains fragments,
// query-parameters, or contains a password.
func NewSpec(sshURL *url.URL) (*Spec, error) {
	s, err := newSpec(sshURL)
	if err != nil {
		return nil, fmt.Errorf("invalid SSH URL: %w", err)
	}
	return s, nil
}

func newSpec(u *url.URL) (*Spec, error) {
	if u == nil {
		return nil, errors.New("URL is nil")
	}
	if u.Scheme == "" {
		return nil, errors.New("no scheme provided")
	}
	if u.Scheme != "ssh" {
		return nil, errors.New("incorrect scheme: " + u.Scheme)
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
		return nil, errors.New("hostname is empty")
	}
	sp.Port = u.Port()
	sp.Path = u.Path
	if u.RawQuery != "" {
		return nil, fmt.Errorf("query parameters are not allowed: %q", u.RawQuery)
	}
	if u.Fragment != "" {
		return nil, fmt.Errorf("fragments are not allowed: %q", u.Fragment)
	}

	return &sp, nil
}

// Spec of SSH URL
type Spec struct {
	User string
	Host string
	Port string
	Path string
}

// Args returns args except "ssh" itself combined with optional additional
// command and args to be executed on the remote host. It attempts to quote
// the given arguments to account for ssh executing the remote command in a
// shell. It returns nil when unable to quote the remote command.
func (sp *Spec) Args(remoteCommandAndArgs ...string) []string {
	// Format the remote command to run using the ssh connection, quoting
	// values where needed because ssh executes these in a POSIX shell.
	remoteCommand, err := quoteCommand(remoteCommandAndArgs...)
	if err != nil {
		return nil
	}

	sshArgs, err := sp.args()
	if err != nil {
		return nil
	}
	if remoteCommand != "" {
		sshArgs = append(sshArgs, remoteCommand)
	}
	return sshArgs
}

func (sp *Spec) args(sshFlags ...string) ([]string, error) {
	var args []string
	if sp.Host == "" {
		return nil, errors.New("no host specified")
	}
	if sp.User != "" {
		// Quote user, as it's obtained from the URL.
		usr, err := syntax.Quote(sp.User, syntax.LangPOSIX)
		if err != nil {
			return nil, fmt.Errorf("invalid user: %w", err)
		}
		args = append(args, "-l", usr)
	}
	if sp.Port != "" {
		// Quote port, as it's obtained from the URL.
		port, err := syntax.Quote(sp.Port, syntax.LangPOSIX)
		if err != nil {
			return nil, fmt.Errorf("invalid port: %w", err)
		}
		args = append(args, "-p", port)
	}

	// We consider "sshFlags" to be "trusted", and set from code only,
	// as they are not parsed from the DOCKER_HOST URL.
	args = append(args, sshFlags...)

	host, err := syntax.Quote(sp.Host, syntax.LangPOSIX)
	if err != nil {
		return nil, fmt.Errorf("invalid host: %w", err)
	}

	return append(args, "--", host), nil
}

// Command returns the ssh flags and arguments to execute a command
// (remoteCommandAndArgs) on the remote host. Where needed, it quotes
// values passed in remoteCommandAndArgs to account for ssh executing
// the remote command in a shell. It returns an error if no remote command
// is passed, or when unable to quote the remote command.
//
// Important: to preserve backward-compatibility, Command does not currently
// perform sanitization or quoting on the sshFlags and callers are expected
// to sanitize this argument.
func (sp *Spec) Command(sshFlags []string, remoteCommandAndArgs ...string) ([]string, error) {
	if len(remoteCommandAndArgs) == 0 {
		return nil, errors.New("no remote command specified")
	}
	sshArgs, err := sp.args(sshFlags...)
	if err != nil {
		return nil, err
	}
	remoteCommand, err := quoteCommand(remoteCommandAndArgs...)
	if err != nil {
		return nil, err
	}
	if remoteCommand != "" {
		sshArgs = append(sshArgs, remoteCommand)
	}
	return sshArgs, nil
}

// quoteCommand returns the remote command to run using the ssh connection
// as a single string, quoting values where needed because ssh executes
// these in a POSIX shell.
func quoteCommand(commandAndArgs ...string) (string, error) {
	var quotedCmd string
	for i, arg := range commandAndArgs {
		a, err := syntax.Quote(arg, syntax.LangPOSIX)
		if err != nil {
			return "", fmt.Errorf("invalid argument: %w", err)
		}
		if i == 0 {
			quotedCmd = a
			continue
		}
		quotedCmd += " " + a //nolint:perfsprint // ignore "concat-loop"; no need to use a string-builder for this.
	}
	// each part is quoted appropriately, so now we'll have a full
	// shell command to pass off to "ssh"
	return quotedCmd, nil
}
