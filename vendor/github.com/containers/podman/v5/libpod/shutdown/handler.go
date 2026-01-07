package shutdown

import (
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	logrusImport "github.com/sirupsen/logrus"
)

var (
	ErrHandlerExists = errors.New("handler with given name already exists")
)

var (
	stopped    bool
	sigChan    chan os.Signal
	cancelChan chan bool
	// Synchronize accesses to the map
	handlerLock sync.Mutex
	// Definitions of all on-shutdown handlers
	handlers map[string]func(os.Signal) error
	// Ordering that on-shutdown handlers will be invoked.
	handlerOrder    []string
	shutdownInhibit sync.RWMutex
	logrus          = logrusImport.WithField("PID", os.Getpid())
	ErrNotStarted   = errors.New("shutdown signal handler has not yet been started")
	// exitCode used to exit once we are done with all signal handlers, by default 1
	exitCode = 1
)

// SetExitCode when we exit after we ran all shutdown handlers, it should be positive.
func SetExitCode(i int) {
	exitCode = i
}

// Start begins handling SIGTERM and SIGINT and will run the given on-signal
// handlers when one is called and then exit with the exit code of 1 if not
// overwritten with SetExitCode(). This can be cancelled by calling Stop().
func Start() error {
	if sigChan != nil {
		// Already running, do nothing.
		return nil
	}

	sigChan = make(chan os.Signal, 2)
	cancelChan = make(chan bool, 1)
	stopped = false

	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-cancelChan:
			logrus.Infof("Received shutdown.Stop(), terminating!")
			signal.Stop(sigChan)
			close(sigChan)
			close(cancelChan)
			stopped = true
			return
		case sig := <-sigChan:
			logrus.Infof("Received shutdown signal %q, terminating!", sig.String())
			shutdownInhibit.Lock()
			handlerLock.Lock()

			for _, name := range handlerOrder {
				handler, ok := handlers[name]
				if !ok {
					logrus.Errorf("Shutdown handler %q definition not found!", name)
					continue
				}

				logrus.Infof("Invoking shutdown handler %q", name)
				start := time.Now()
				if err := handler(sig); err != nil {
					logrus.Errorf("Running shutdown handler %q: %v", name, err)
				}
				logrus.Debugf("Completed shutdown handler %q, duration %v", name,
					time.Since(start).Round(time.Second))
			}
			handlerLock.Unlock()
			shutdownInhibit.Unlock()
			os.Exit(exitCode)
			return
		}
	}()

	return nil
}

// Stop the shutdown signal handler.
func Stop() error {
	if cancelChan == nil {
		return ErrNotStarted
	}
	if stopped {
		return nil
	}

	// if the signal handler is running, wait that it terminates
	handlerLock.Lock()
	defer handlerLock.Unlock()
	// it doesn't need to be in the critical section, but staticcheck complains if
	// the critical section is empty.
	cancelChan <- true

	return nil
}

// Inhibit temporarily inhibit signals from shutting down Libpod.
func Inhibit() {
	shutdownInhibit.RLock()
}

// Uninhibit stop inhibiting signals from shutting down Libpod.
func Uninhibit() {
	shutdownInhibit.RUnlock()
}

// Register registers a function that will be executed when Podman is terminated
// by a signal. Handlers are invoked LIFO - the last handler registered is the
// first run.
func Register(name string, handler func(os.Signal) error) error {
	handlerLock.Lock()
	defer handlerLock.Unlock()

	if handlers == nil {
		handlers = make(map[string]func(os.Signal) error)
	}

	if _, ok := handlers[name]; ok {
		return ErrHandlerExists
	}

	handlers[name] = handler
	handlerOrder = append([]string{name}, handlerOrder...)

	return nil
}

// Unregister un-registers a given shutdown handler.
func Unregister(name string) error {
	handlerLock.Lock()
	defer handlerLock.Unlock()

	if handlers == nil {
		return nil
	}

	if _, ok := handlers[name]; !ok {
		return nil
	}

	delete(handlers, name)

	newOrder := []string{}
	for _, checkName := range handlerOrder {
		if checkName != name {
			newOrder = append(newOrder, checkName)
		}
	}
	handlerOrder = newOrder

	return nil
}
