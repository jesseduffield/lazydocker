package reexec

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	registeredInitializers = make(map[string]func())
	initWasCalled          = false
)

// Register adds an initialization func under the specified name
func Register(name string, initializer func()) {
	if _, exists := registeredInitializers[name]; exists {
		panic(fmt.Sprintf("reexec func already registered under name %q", name))
	}

	registeredInitializers[name] = initializer
}

// Init is called as the first part of the exec process and returns true if an
// initialization function was called.
func Init() bool {
	initializer, exists := registeredInitializers[os.Args[0]]
	initWasCalled = true
	if exists {
		initializer()

		return true
	}
	return false
}

func panicIfNotInitialized() {
	if !initWasCalled {
		// The reexec package is used to run subroutines in
		// subprocesses which would otherwise have unacceptable side
		// effects on the main thread.  If you found this error, then
		// your program uses a package which needs to do this.  In
		// order for that to work, main() should start with this
		// boilerplate, or an equivalent:
		//     if reexec.Init() {
		//         return
		//     }
		panic("a library subroutine needed to run a subprocess, but reexec.Init() was not called in main()")
	}
}

func naiveSelf() string {
	name := os.Args[0]
	if filepath.Base(name) == name {
		if lp, err := exec.LookPath(name); err == nil {
			return lp
		}
	}
	// handle conversion of relative paths to absolute
	if absName, err := filepath.Abs(name); err == nil {
		return absName
	}
	// if we couldn't get absolute name, return original
	// (NOTE: Go only errors on Abs() if os.Getwd fails)
	return name
}
