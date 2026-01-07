package ssh

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"go.podman.io/common/pkg/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

const sshdPort = 22

func Validate(user *url.Userinfo, path string, port int, identity string) (*config.Destination, *url.URL, error) {
	// url.Parse NEEDS ssh://, if this ever fails or returns some nonsense, that is why.
	uri, err := url.Parse(path)
	if err != nil {
		return nil, nil, err
	}

	// sometimes we are not going to have a path, this breaks uri.Hostname()
	if uri.Host == "" && strings.Contains(uri.String(), "@") {
		uri.Host = strings.Split(uri.String(), "@")[1]
	}

	if uri.Port() == "" {
		if port == 0 {
			port = sshdPort
		}
		uri.Host = net.JoinHostPort(uri.Host, strconv.Itoa(port))
	}

	if user != nil {
		uri.User = user
	}

	dst := config.Destination{
		URI: uri.String(),
	}

	if len(identity) > 0 {
		dst.Identity = identity
	}
	return &dst, uri, err
}

var (
	passPhrase   []byte
	phraseSync   sync.Once
	password     []byte
	passwordSync sync.Once
)

// ReadPassword prompts for a secret and returns value input by user from stdin
// Unlike terminal.ReadPassword(), $(echo $SECRET | podman...) is supported.
// Additionally, all input after `<secret>/n` is queued to podman command.
func ReadPassword(prompt string) (pw []byte, err error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, prompt)
		pw, err = term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return pw, err
	}

	var b [1]byte
	for {
		n, err := os.Stdin.Read(b[:])
		// terminal.ReadPassword discards any '\r', so we do the same
		if n > 0 && b[0] != '\r' {
			if b[0] == '\n' {
				return pw, nil
			}
			pw = append(pw, b[0])
			// limit size, so that a wrong input won't fill up the memory
			if len(pw) > 1024 {
				err = errors.New("password too long, 1024 byte limit")
			}
		}
		if err != nil {
			// terminal.ReadPassword accepts EOF-terminated passwords
			// if non-empty, so we do the same
			if err == io.EOF && len(pw) > 0 {
				err = nil
			}
			return pw, err
		}
	}
}

func PublicKey(path string, passphrase []byte) (ssh.Signer, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		if _, ok := err.(*ssh.PassphraseMissingError); !ok {
			return nil, err
		}
		if len(passphrase) == 0 {
			passphrase = ReadPassphrase()
		}
		return ssh.ParsePrivateKeyWithPassphrase(key, passphrase)
	}
	return signer, nil
}

func ReadPassphrase() []byte {
	phraseSync.Do(func() {
		secret, err := ReadPassword("Key Passphrase: ")
		if err != nil {
			secret = []byte{}
		}
		passPhrase = secret
	})
	return passPhrase
}

func ReadLogin() []byte {
	passwordSync.Do(func() {
		secret, err := ReadPassword("Login password: ")
		if err != nil {
			secret = []byte{}
		}
		password = secret
	})
	return password
}

func ParseScpArgs(options ConnectionScpOptions) (string, string, string, bool, error) {
	// assume load to remote
	host := options.Destination
	if strings.Contains(host, "ssh://") {
		host = strings.Split(host, "ssh://")[1]
	}
	localPath := options.Source
	if strings.Contains(localPath, "ssh://") {
		localPath = strings.Split(localPath, "ssh://")[1]
	}
	var remotePath string
	swap := false
	if split := strings.Split(localPath, ":"); len(split) == 2 {
		// save to remote, load to local
		host = split[0]
		remotePath = split[1]
		localPath = options.Destination
		swap = true
	} else {
		split = strings.Split(host, ":")
		if len(split) != 2 {
			return "", "", "", false, errors.New("no remote destination provided")
		}
		host = split[0]
		remotePath = split[1]
	}
	remotePath = strings.TrimSuffix(remotePath, "\n")
	return host, remotePath, localPath, swap, nil
}

func DialNet(sshClient *ssh.Client, mode string, url *url.URL) (net.Conn, error) {
	port := sshdPort
	if url.Port() != "" {
		p, err := strconv.Atoi(url.Port())
		if err != nil {
			return nil, err
		}
		port = p
	}
	if _, _, err := Validate(url.User, url.Hostname(), port, ""); err != nil {
		return nil, err
	}
	return sshClient.Dial(mode, url.Path)
}

func DefineMode(flag string) EngineMode {
	switch flag {
	case "native":
		return NativeMode
	case "golang":
		return GolangMode
	default:
		return InvalidMode
	}
}
