//go:build !windows

package containers

import (
	"context"
	"os"
	"os/signal"

	sig "github.com/containers/podman/v5/pkg/signal"
	"golang.org/x/term"
)

func makeRawTerm(stdin *os.File) (*term.State, error) {
	return term.MakeRaw(int(stdin.Fd()))
}

func notifyWinChange(_ context.Context, winChange chan os.Signal, _ *os.File, _ *os.File) {
	signal.Notify(winChange, sig.SIGWINCH)
}

func getTermSize(stdin *os.File, _ *os.File) (width, height int, err error) {
	return term.GetSize(int(stdin.Fd()))
}
