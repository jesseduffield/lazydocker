package ssh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"

	"go.podman.io/common/pkg/config"
)

func nativeConnectionCreate(options ConnectionCreateOptions) error {
	var match bool
	var err error
	if match, err = regexp.MatchString("^[A-Za-z][A-Za-z0-9+.-]*://", options.Path); err != nil {
		return fmt.Errorf("invalid destination: %w", err)
	}

	if !match {
		options.Path = "ssh://" + options.Path
	}

	if len(options.Socket) > 0 {
		options.Path += options.Socket
	}

	dst, uri, err := Validate(options.User, options.Path, options.Port, options.Identity)
	if err != nil {
		return err
	}

	// test connection
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return err
	}

	if host, _, ok := strings.Cut(uri.Host, "/run"); ok {
		uri.Host = host
	}
	conf, err := config.Default()
	if err != nil {
		return err
	}

	args := []string{uri.User.String() + "@" + uri.Hostname()}

	if len(dst.Identity) > 0 {
		args = append(args, "-i", dst.Identity)
	}
	if len(conf.Engine.SSHConfig) > 0 {
		args = append(args, "-F", conf.Engine.SSHConfig)
	}

	output := &bytes.Buffer{}
	args = append(args, "podman", "info", "--format", "json")
	info := exec.Command(ssh, args...)
	info.Stdout = output
	err = info.Run()
	if err != nil {
		return err
	}

	remoteInfo := &Info{}
	if err := json.Unmarshal(output.Bytes(), &remoteInfo); err != nil {
		return fmt.Errorf("failed to parse 'podman info' results: %w", err)
	}

	if remoteInfo.Host.RemoteSocket == nil || len(remoteInfo.Host.RemoteSocket.Path) == 0 {
		return fmt.Errorf("remote podman %q failed to report its UDS socket", uri.Host)
	}

	// TODO this really should not live here, it must be in podman where we write the other connections as well.
	// This duplicates the code for no reason and I have a really hard time to make any sense of why this code
	// was added in the first place.
	return config.EditConnectionConfig(func(cfg *config.ConnectionsFile) error {
		if cfg.Connection.Connections == nil {
			cfg.Connection.Connections = map[string]config.Destination{
				options.Name: *dst,
			}
			cfg.Connection.Default = options.Name
		} else {
			cfg.Connection.Connections[options.Name] = *dst
		}

		// Create or update an existing farm with the connection being added
		if options.Farm != "" {
			if len(cfg.Farm.List) == 0 {
				cfg.Farm.Default = options.Farm
			}
			if val, ok := cfg.Farm.List[options.Farm]; ok {
				cfg.Farm.List[options.Farm] = append(val, options.Name)
			} else {
				cfg.Farm.List[options.Farm] = []string{options.Name}
			}
		}
		return nil
	})
}

func nativeConnectionExec(options ConnectionExecOptions, input io.Reader) (*ConnectionExecReport, error) {
	dst, uri, err := Validate(options.User, options.Host, options.Port, options.Identity)
	if err != nil {
		return nil, err
	}

	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return nil, err
	}

	output := &bytes.Buffer{}
	errors := &bytes.Buffer{}
	if host, _, ok := strings.Cut(uri.Host, "/run"); ok {
		uri.Host = host
	}

	options.Args = append([]string{uri.User.String() + "@" + uri.Hostname()}, options.Args...)
	conf, err := config.Default()
	if err != nil {
		return nil, err
	}

	args := []string{}
	if len(dst.Identity) > 0 {
		args = append(args, "-i", dst.Identity)
	}
	if len(conf.Engine.SSHConfig) > 0 {
		args = append(args, "-F", conf.Engine.SSHConfig)
	}
	args = append(args, options.Args...)
	info := exec.Command(ssh, args...)
	info.Stdout = output
	info.Stderr = errors
	if input != nil {
		info.Stdin = input
	}
	err = info.Run()
	if err != nil {
		return nil, err
	}
	return &ConnectionExecReport{Response: output.String()}, nil
}

func nativeConnectionScp(options ConnectionScpOptions) (*ConnectionScpReport, error) {
	host, remotePath, localPath, swap, err := ParseScpArgs(options)
	if err != nil {
		return nil, err
	}
	dst, uri, err := Validate(options.User, host, options.Port, options.Identity)
	if err != nil {
		return nil, err
	}

	scp, err := exec.LookPath("scp")
	if err != nil {
		return nil, err
	}

	conf, err := config.Default()
	if err != nil {
		return nil, err
	}

	args := []string{}
	if len(dst.Identity) > 0 {
		args = append(args, "-i", dst.Identity)
	}
	if len(conf.Engine.SSHConfig) > 0 {
		args = append(args, "-F", conf.Engine.SSHConfig)
	}

	userString := ""
	if !strings.Contains(host, "@") {
		userString = uri.User.String() + "@"
	}
	// meaning, we are copying from a remote host
	if swap {
		args = append(args, userString+host+":"+remotePath, localPath)
	} else {
		args = append(args, localPath, userString+host+":"+remotePath)
	}

	info := exec.Command(scp, args...)
	err = info.Run()
	if err != nil {
		return nil, err
	}

	return &ConnectionScpReport{Response: remotePath}, nil
}
