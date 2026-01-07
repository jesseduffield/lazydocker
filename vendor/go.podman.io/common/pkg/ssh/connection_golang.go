package ssh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	// We are using skeema/knownhosts rather than
	// golang.org/x/crypto/ssh/knownhosts because the
	// latter has an issue when the first key returned
	// by the server doesn't match the one in known_hosts:
	// https://github.com/golang/go/issues/29286
	// https://github.com/containers/podman/issues/23575
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
	"github.com/skeema/knownhosts"
	"go.podman.io/common/pkg/config"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func golangConnectionCreate(options ConnectionCreateOptions) error {
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

	if uri.Path == "" || uri.Path == "/" {
		if uri.Path, err = getUDS(uri, options.Identity); err != nil {
			return err
		}
		dst.URI += uri.Path
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

func golangConnectionDial(options ConnectionDialOptions) (*ConnectionDialReport, error) {
	_, uri, err := Validate(options.User, options.Host, options.Port, options.Identity)
	if err != nil {
		return nil, err
	}
	cfg, err := ValidateAndConfigure(uri, options.Identity, options.InsecureIsMachineConnection)
	if err != nil {
		return nil, err
	}

	dial, err := ssh.Dial("tcp", uri.Host, cfg) // dial the client
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return &ConnectionDialReport{dial}, nil
}

func golangConnectionExec(options ConnectionExecOptions, input io.Reader) (*ConnectionExecReport, error) {
	if !strings.HasPrefix(options.Host, "ssh://") {
		options.Host = "ssh://" + options.Host
	}
	_, uri, err := Validate(options.User, options.Host, options.Port, options.Identity)
	if err != nil {
		return nil, err
	}

	cfg, err := ValidateAndConfigure(uri, options.Identity, false)
	if err != nil {
		return nil, err
	}
	dialAdd, err := ssh.Dial("tcp", uri.Host, cfg) // dial the client
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	out, err := ExecRemoteCommandWithInput(dialAdd, strings.Join(options.Args, " "), input)
	if err != nil {
		return nil, err
	}
	return &ConnectionExecReport{Response: string(out)}, nil
}

func golangConnectionScp(options ConnectionScpOptions) (*ConnectionScpReport, error) {
	host, remoteFile, localFile, swap, err := ParseScpArgs(options)
	if err != nil {
		return nil, err
	}

	// removed for parsing
	if !strings.HasPrefix(host, "ssh://") {
		host = "ssh://" + host
	}
	_, uri, err := Validate(options.User, host, options.Port, options.Identity)
	if err != nil {
		return nil, err
	}
	cfg, err := ValidateAndConfigure(uri, options.Identity, false)
	if err != nil {
		return nil, err
	}

	dial, err := ssh.Dial("tcp", uri.Host, cfg) // dial the client
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	sc, err := sftp.NewClient(dial)
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(localFile, (os.O_RDWR | os.O_CREATE), 0o644)
	if err != nil {
		return nil, err
	}

	parent := filepath.Dir(remoteFile)
	path := string(filepath.Separator)
	for dir := range strings.SplitSeq(parent, path) {
		path = filepath.Join(path, dir)
		// ignore errors due to most of the dirs already existing
		_ = sc.Mkdir(path)
	}

	remote, err := sc.OpenFile(remoteFile, (os.O_RDWR | os.O_CREATE))
	if err != nil {
		return nil, err
	}
	defer remote.Close()

	if !swap {
		_, err = io.Copy(remote, f)
		if err != nil {
			return nil, err
		}
	} else {
		_, err = io.Copy(f, remote)
		if err != nil {
			return nil, err
		}
	}
	return &ConnectionScpReport{Response: remote.Name()}, nil
}

// ExecRemoteCommand takes a ssh client connection and a command to run and executes the
// command on the specified client. The function returns the Stdout from the client or the Stderr.
func ExecRemoteCommand(dial *ssh.Client, run string) ([]byte, error) {
	return ExecRemoteCommandWithInput(dial, run, nil)
}

func ExecRemoteCommandWithInput(dial *ssh.Client, run string, input io.Reader) ([]byte, error) {
	sess, err := dial.NewSession() // new ssh client session
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	var buffer bytes.Buffer
	var bufferErr bytes.Buffer
	sess.Stdout = &buffer    // output from client funneled into buffer
	sess.Stderr = &bufferErr // err from client funneled into buffer
	if input != nil {
		sess.Stdin = input
	}
	if err := sess.Run(run); err != nil { // run the command on the ssh client
		return nil, fmt.Errorf("%v: %w", bufferErr.String(), err)
	}
	return buffer.Bytes(), nil
}

func GetUserInfo(uri *url.URL) (*url.Userinfo, error) {
	var (
		usr *user.User
		err error
	)
	if u, found := os.LookupEnv("_CONTAINERS_ROOTLESS_UID"); found {
		usr, err = user.LookupId(u)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup rootless user: %w", err)
		}
	} else {
		usr, err = user.Current()
		if err != nil {
			return nil, fmt.Errorf("failed to obtain current user: %w", err)
		}
	}

	pw, set := uri.User.Password()
	if set {
		return url.UserPassword(usr.Username, pw), nil
	}
	return url.User(usr.Username), nil
}

// ValidateAndConfigure will take a ssh url and an identity key (rsa and the like) and ensure the information given is valid
// iden can be blank to mean no identity key
// once the function validates the information it creates and returns an ssh.ClientConfig.
func ValidateAndConfigure(uri *url.URL, iden string, insecureIsMachineConnection bool) (*ssh.ClientConfig, error) {
	var signers []ssh.Signer
	passwd, passwdSet := uri.User.Password()
	if iden != "" { // iden might be blank if coming from image scp or if no validation is needed
		value := iden
		s, err := PublicKey(value, []byte(passwd))
		if err != nil {
			return nil, fmt.Errorf("failed to read identity %q: %w", value, err)
		}
		signers = append(signers, s)
		logrus.Debugf("SSH Ident Key %q %s %s", value, ssh.FingerprintSHA256(s.PublicKey()), s.PublicKey().Type())
	} else if sock, found := os.LookupEnv("SSH_AUTH_SOCK"); found { // validate ssh information, specifically the unix file socket used by the ssh agent.
		logrus.Debugf("Found SSH_AUTH_SOCK %q, ssh-agent signer enabled", sock)

		c, err := net.Dial("unix", sock)
		if err != nil {
			return nil, err
		}
		agentSigners, err := agent.NewClient(c).Signers()
		if err != nil {
			return nil, err
		}

		signers = append(signers, agentSigners...)

		if logrus.IsLevelEnabled(logrus.DebugLevel) {
			for _, s := range agentSigners {
				logrus.Debugf("SSH Agent Key %s %s", ssh.FingerprintSHA256(s.PublicKey()), s.PublicKey().Type())
			}
		}
	}
	var authMethods []ssh.AuthMethod // now we validate and check for the authorization methods, most notably public key authorization
	if len(signers) > 0 {
		dedup := make(map[string]ssh.Signer)
		for _, s := range signers {
			fp := ssh.FingerprintSHA256(s.PublicKey())
			if _, found := dedup[fp]; found {
				logrus.Debugf("Dedup SSH Key %s %s", ssh.FingerprintSHA256(s.PublicKey()), s.PublicKey().Type())
			}
			dedup[fp] = s
		}

		uniq := slices.Collect(maps.Values(dedup))
		authMethods = append(authMethods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			return uniq, nil
		}))
	}
	if passwdSet { // if password authentication is given and valid, add to the list
		authMethods = append(authMethods, ssh.Password(passwd))
	}
	if len(authMethods) == 0 {
		authMethods = append(authMethods, ssh.PasswordCallback(func() (string, error) {
			pass, err := ReadPassword(uri.User.Username() + "'s login password:")
			return string(pass), err
		}))
	}
	tick, err := time.ParseDuration("40s")
	if err != nil {
		return nil, err
	}

	keyFilePath := filepath.Join(homedir.Get(), ".ssh", "known_hosts")
	known, err := knownhosts.NewDB(keyFilePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		keyDir := filepath.Dir(keyFilePath)
		if err := fileutils.Exists(keyDir); errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(keyDir, 0o700); err != nil {
				return nil, err
			}
		}
		k, err := os.OpenFile(keyFilePath, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, err
		}
		k.Close()
		known, err = knownhosts.NewDB(keyFilePath)
		if err != nil {
			return nil, err
		}
	}

	var callback ssh.HostKeyCallback
	if insecureIsMachineConnection {
		callback = ssh.InsecureIgnoreHostKey()
	} else {
		callback = ssh.HostKeyCallback(func(host string, remote net.Addr, pubKey ssh.PublicKey) error {
			// we need to check if there is an error from reading known hosts for this public key and if there is an error, what is it, and why is it happening?
			// if it is a key mismatch we want to error since we know the host using another key
			// however, if it is a general error not because of a known key, we want to add our key to the known_hosts file
			hErr := known.HostKeyCallback()(host, remote, pubKey)
			switch {
			case knownhosts.IsHostKeyChanged(hErr):
				logrus.Warnf("ssh host key mismatch for host %s, got key %s of type %s", host, ssh.FingerprintSHA256(pubKey), pubKey.Type())
				return hErr
			// if keyErr.Want is empty that just means we do not know this host yet, add it.
			case knownhosts.IsHostUnknown(hErr):
				// write to known_hosts
				err := addKnownHostsEntry(host, pubKey)
				if err != nil {
					if os.IsNotExist(err) {
						logrus.Warn("podman will soon require a known_hosts file to function properly.")
						return nil
					}
					return err
				}
			case hErr != nil:
				return hErr
			}
			return nil
		})
	}

	cfg := &ssh.ClientConfig{
		User:              uri.User.Username(),
		Auth:              authMethods,
		HostKeyCallback:   callback,
		Timeout:           tick,
		HostKeyAlgorithms: known.HostKeyAlgorithms(uri.Host),
	}
	return cfg, nil
}

func getUDS(uri *url.URL, iden string) (string, error) {
	cfg, err := ValidateAndConfigure(uri, iden, false)
	if err != nil {
		return "", fmt.Errorf("failed to validate: %w", err)
	}
	dial, err := ssh.Dial("tcp", uri.Host, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to connect: %w", err)
	}
	defer dial.Close()

	session, err := dial.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create new ssh session on %q: %w", uri.Host, err)
	}
	defer session.Close()

	// Override podman binary for testing etc
	podman := "podman"
	if v, found := os.LookupEnv("PODMAN_BINARY"); found {
		podman = v
	}
	infoJSON, err := ExecRemoteCommand(dial, podman+" info --format=json")
	if err != nil {
		return "", err
	}

	var info Info
	if err := json.Unmarshal(infoJSON, &info); err != nil {
		return "", fmt.Errorf("failed to parse 'podman info' results: %w", err)
	}

	if info.Host.RemoteSocket == nil || len(info.Host.RemoteSocket.Path) == 0 {
		return "", fmt.Errorf("remote podman %q failed to report its UDS socket", uri.Host)
	}
	return info.Host.RemoteSocket.Path, nil
}

// addKnownHostsEntry adds (host, pubKey) to user’s known_hosts.
func addKnownHostsEntry(host string, pubKey ssh.PublicKey) error {
	hd := homedir.Get()
	known := filepath.Join(hd, ".ssh", "known_hosts")
	f, err := os.OpenFile(known, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	l := knownhosts.Line([]string{host}, pubKey)
	if _, err = f.WriteString("\n" + l + "\n"); err != nil {
		return err
	}
	logrus.Infof("key %s added to %s", ssh.FingerprintSHA256(pubKey), known)
	return nil
}
