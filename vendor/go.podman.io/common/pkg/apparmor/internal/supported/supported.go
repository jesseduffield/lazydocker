package supported

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	runcaa "github.com/opencontainers/runc/libcontainer/apparmor"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/unshare"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate

// ApparmorVerifier is the global struct for verifying if AppAmor is available
// on the system.
type ApparmorVerifier struct {
	impl             verifierImpl
	parserBinaryPath string
}

var (
	singleton *ApparmorVerifier
	once      sync.Once
)

// NewAppArmorVerifier can be used to retrieve a new ApparmorVerifier instance.
func NewAppArmorVerifier() *ApparmorVerifier {
	once.Do(func() {
		singleton = &ApparmorVerifier{impl: &defaultVerifier{}}
	})
	return singleton
}

// IsSupported returns nil if AppAmor is supported by the host system.
// The method will error if:
// - the process runs in rootless mode
// - AppArmor is disabled by the host system
// - the `apparmor_parser` binary is not discoverable.
func (a *ApparmorVerifier) IsSupported() error {
	if a.impl.UnshareIsRootless() {
		return errors.New("AppAmor is not supported on rootless containers")
	}
	if !a.impl.RuncIsEnabled() {
		return errors.New("AppArmor not supported by the host system")
	}

	_, err := a.FindAppArmorParserBinary()
	return err
}

// FindAppArmorParserBinary returns the `apparmor_parser` binary either from
// `/sbin` or from `$PATH`. It returns an error if the binary could not be
// found.
func (a *ApparmorVerifier) FindAppArmorParserBinary() (string, error) {
	// Use the memoized path if available
	if a.parserBinaryPath != "" {
		logrus.Debugf("Using %s binary", a.parserBinaryPath)
		return a.parserBinaryPath, nil
	}

	const (
		binary = "apparmor_parser"
		sbin   = "/sbin"
	)

	// `/sbin` is not always in `$PATH`, so we check it explicitly
	sbinBinaryPath := filepath.Join(sbin, binary)
	if _, err := a.impl.OsStat(sbinBinaryPath); err == nil {
		logrus.Debugf("Found %s binary in %s", binary, sbinBinaryPath)
		a.parserBinaryPath = sbinBinaryPath
		return sbinBinaryPath, nil
	}

	// Fallback to checking $PATH
	if path, err := a.impl.ExecLookPath(binary); err == nil {
		logrus.Debugf("Found %s binary in %s", binary, path)
		a.parserBinaryPath = path
		return path, nil
	}

	return "", fmt.Errorf(
		"%s binary neither found in %s nor $PATH", binary, sbin,
	)
}

//counterfeiter:generate . verifierImpl
type verifierImpl interface {
	UnshareIsRootless() bool
	RuncIsEnabled() bool
	OsStat(name string) (os.FileInfo, error)
	ExecLookPath(file string) (string, error)
}

type defaultVerifier struct{}

func (d *defaultVerifier) UnshareIsRootless() bool {
	return unshare.IsRootless()
}

func (d *defaultVerifier) RuncIsEnabled() bool {
	return runcaa.IsEnabled()
}

func (d *defaultVerifier) OsStat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (d *defaultVerifier) ExecLookPath(file string) (string, error) {
	return exec.LookPath(file)
}
