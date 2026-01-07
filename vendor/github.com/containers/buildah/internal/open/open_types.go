package open

import (
	"syscall"
)

type request struct {
	Path  string
	Mode  int
	Perms uint32
}

type requests struct {
	Root string
	Wd   string
	Open []request
}

type result struct {
	Fd    uintptr       // as returned by open()
	Err   string        // if err was not `nil`, err.Error()
	Errno syscall.Errno // if err was not `nil` and included a syscall.Errno, its value
}

type results struct {
	Err  string
	Open []result
}
