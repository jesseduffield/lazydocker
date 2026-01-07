package sshagent

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/containers/buildah/internal/tmpdir"
	"github.com/opencontainers/selinux/go-selinux"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// AgentServer is an ssh agent that can be served and shutdown at a later time
type AgentServer struct {
	agent     agent.Agent
	wg        sync.WaitGroup
	conn      *net.Conn
	listener  net.Listener
	shutdown  chan bool
	servePath string
	serveDir  string
}

// NewAgentServer creates a new agent on the host
func NewAgentServer(source *Source) (*AgentServer, error) {
	if source.Keys != nil {
		return newAgentServerKeyring(source.Keys)
	}
	return newAgentServerSocket(source.Socket)
}

// newAgentServerKeyring creates a new agent from scratch and adds keys
func newAgentServerKeyring(keys []any) (*AgentServer, error) {
	a := agent.NewKeyring()
	for _, k := range keys {
		if err := a.Add(agent.AddedKey{PrivateKey: k}); err != nil {
			return nil, fmt.Errorf("failed to create ssh agent: %w", err)
		}
	}
	return &AgentServer{
		agent:    a,
		shutdown: make(chan bool, 1),
	}, nil
}

// newAgentServerSocket creates a new agent from an existing agent on the host
func newAgentServerSocket(socketPath string) (*AgentServer, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	a := &readOnlyAgent{agent.NewClient(conn)}

	return &AgentServer{
		agent:    a,
		conn:     &conn,
		shutdown: make(chan bool, 1),
	}, nil
}

// Serve starts the SSH agent on the host and returns the path of the socket where the agent is serving
func (a *AgentServer) Serve(processLabel string) (string, error) {
	// Calls to `selinux.SetSocketLabel` should be wrapped in
	// runtime.LockOSThread()/runtime.UnlockOSThread() until
	// the the socket is created to guarantee another goroutine
	// does not migrate to the current thread before execution
	// is complete.
	// Ref: https://github.com/opencontainers/selinux/blob/main/go-selinux/selinux.go#L158
	runtime.LockOSThread()
	err := selinux.SetSocketLabel(processLabel)
	if err != nil {
		return "", err
	}
	serveDir, err := os.MkdirTemp(tmpdir.GetTempDir(), ".buildah-ssh-sock")
	if err != nil {
		return "", err
	}
	servePath := filepath.Join(serveDir, "ssh_auth_sock")
	a.serveDir = serveDir
	a.servePath = servePath
	listener, err := net.Listen("unix", servePath)
	if err != nil {
		return "", err
	}
	// Reset socket label.
	err = selinux.SetSocketLabel("")
	// Unlock the thread only if the process label could be restored
	// successfully.  Otherwise leave the thread locked and the Go runtime
	// will terminate it once it returns to the threads pool.
	runtime.UnlockOSThread()
	if err != nil {
		return "", err
	}
	a.listener = listener

	go func() {
		for {
			// listener.Accept blocks
			c, err := listener.Accept()
			if err != nil {
				select {
				case <-a.shutdown:
					return
				default:
					logrus.Errorf("error accepting SSH connection: %v", err)
					continue
				}
			}
			a.wg.Add(1)
			go func() {
				// agent.ServeAgent will only ever return with error,
				err := agent.ServeAgent(a.agent, c)
				if err != io.EOF {
					logrus.Errorf("error serving agent: %v", err)
				}
				a.wg.Done()
			}()
			// the only way to get agent.ServeAgent is to close the connection it's serving on
			// TODO: ideally we should use some sort of forwarding mechanism for output instead of manually closing connection.
			go func() {
				time.Sleep(2000 * time.Millisecond)
				c.Close()
			}()
		}
	}()
	return a.servePath, nil
}

// Shutdown shuts down the agent and closes the socket
func (a *AgentServer) Shutdown() error {
	if a.listener != nil {
		a.shutdown <- true
		a.listener.Close()
	}
	if a.conn != nil {
		conn := *a.conn
		conn.Close()
	}
	a.wg.Wait()
	err := os.RemoveAll(a.serveDir)
	if err != nil {
		return err
	}
	a.serveDir = ""
	a.servePath = ""
	return nil
}

// ServePath returns the path where the agent is serving
func (a *AgentServer) ServePath() string {
	return a.servePath
}

// readOnlyAgent and its functions originally from github.com/mopby/buildkit/session/sshforward/sshprovider/agentprovider.go

// readOnlyAgent implemetnts the agent.Agent interface
// readOnlyAgent allows reads only to prevent keys from being added from the build to the forwarded ssh agent on the host
type readOnlyAgent struct {
	agent.ExtendedAgent
}

func (a *readOnlyAgent) Add(_ agent.AddedKey) error {
	return errors.New("adding new keys not allowed by buildah")
}

func (a *readOnlyAgent) Remove(_ ssh.PublicKey) error {
	return errors.New("removing keys not allowed by buildah")
}

func (a *readOnlyAgent) RemoveAll() error {
	return errors.New("removing keys not allowed by buildah")
}

func (a *readOnlyAgent) Lock(_ []byte) error {
	return errors.New("locking agent not allowed by buildah")
}

func (a *readOnlyAgent) Extension(_ string, _ []byte) ([]byte, error) {
	return nil, errors.New("extensions not allowed by buildah")
}

// Source is what the forwarded agent's source is
// The source of the forwarded agent can be from a socket on the host, or from individual key files
type Source struct {
	Socket string
	Keys   []any
}

// NewSource takes paths and checks of they are keys or sockets, and creates a source
func NewSource(paths []string) (*Source, error) {
	var keys []any
	var socket string
	if len(paths) == 0 {
		socket = os.Getenv("SSH_AUTH_SOCK")
		if socket == "" {
			return nil, errors.New("SSH_AUTH_SOCK not set in environment")
		}
		absSocket, err := filepath.Abs(socket)
		if err != nil {
			return nil, fmt.Errorf("evaluating SSH_AUTH_SOCK in environment: %w", err)
		}
		socket = absSocket
	}
	for _, p := range paths {
		if socket != "" {
			return nil, errors.New("only one socket is allowed")
		}

		fi, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if fi.Mode()&os.ModeSocket > 0 {
			if len(keys) == 0 {
				socket = p
			} else {
				return nil, errors.New("cannot mix keys and socket file")
			}
			continue
		}

		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		dt, err := io.ReadAll(&io.LimitedReader{R: f, N: 100 * 1024})
		if err != nil {
			return nil, err
		}

		k, err := ssh.ParseRawPrivateKey(dt)
		if err != nil {
			return nil, fmt.Errorf("cannot parse ssh key: %w", err)
		}
		keys = append(keys, k)
	}
	if socket != "" {
		return &Source{
			Socket: socket,
		}, nil
	}
	return &Source{
		Keys: keys,
	}, nil
}
