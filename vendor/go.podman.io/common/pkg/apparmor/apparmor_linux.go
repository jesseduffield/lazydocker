//go:build linux && apparmor

package apparmor

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"text/template"

	runcaa "github.com/opencontainers/runc/libcontainer/apparmor"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/apparmor/internal/supported"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/unshare"
)

// profileDirectory is the file store for apparmor profiles and macros.
var profileDirectory = "/etc/apparmor.d"

// IsEnabled returns true if AppArmor is enabled on the host. It also checks
// for the existence of the `apparmor_parser` binary, which will be required to
// apply profiles.
func IsEnabled() bool {
	return supported.NewAppArmorVerifier().IsSupported() == nil
}

// profileData holds information about the given profile for generation.
type profileData struct {
	// Name is profile name.
	Name string
	// Imports defines the apparmor functions to import, before defining the profile.
	Imports []string
	// InnerImports defines the apparmor functions to import in the profile.
	InnerImports []string
	// Version is the {major, minor, patch} version of apparmor_parser as a single number.
	Version int
}

// generateDefault creates an apparmor profile from ProfileData.
func (p *profileData) generateDefault(apparmorParserPath string, out io.Writer) error {
	compiled, err := template.New("apparmor_profile").Parse(defaultProfileTemplate)
	if err != nil {
		return fmt.Errorf("create AppArmor profile from template: %w", err)
	}

	if macroExists("tunables/global") {
		p.Imports = append(p.Imports, "#include <tunables/global>")
	} else {
		p.Imports = append(p.Imports, "@{PROC}=/proc/")
	}

	if macroExists("abstractions/base") {
		p.InnerImports = append(p.InnerImports, "#include <abstractions/base>")
	}

	ver, err := getAAParserVersion(apparmorParserPath)
	if err != nil {
		return fmt.Errorf("get AppArmor version: %w", err)
	}
	p.Version = ver

	if err := compiled.Execute(out, p); err != nil {
		return fmt.Errorf("execute compiled profile: %w", err)
	}

	return nil
}

// macrosExists checks if the passed macro exists.
func macroExists(m string) bool {
	err := fileutils.Exists(path.Join(profileDirectory, m))
	return err == nil
}

// InstallDefault generates a default profile and loads it into the kernel
// using 'apparmor_parser'.
func InstallDefault(name string) error {
	if unshare.IsRootless() {
		return ErrApparmorRootless
	}

	p := profileData{
		Name: name,
	}

	apparmorParserPath, err := supported.NewAppArmorVerifier().FindAppArmorParserBinary()
	if err != nil {
		return fmt.Errorf("find `apparmor_parser` binary: %w", err)
	}

	cmd := exec.Command(apparmorParserPath, "-Kr")
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("execute %s: %w", apparmorParserPath, err)
	}
	if err := cmd.Start(); err != nil {
		if pipeErr := pipe.Close(); pipeErr != nil {
			logrus.Errorf("Unable to close AppArmor pipe: %q", pipeErr)
		}
		return fmt.Errorf("start %s command: %w", apparmorParserPath, err)
	}
	if err := p.generateDefault(apparmorParserPath, pipe); err != nil {
		if pipeErr := pipe.Close(); pipeErr != nil {
			logrus.Errorf("Unable to close AppArmor pipe: %q", pipeErr)
		}
		if cmdErr := cmd.Wait(); cmdErr != nil {
			logrus.Errorf("Unable to wait for AppArmor command: %q", cmdErr)
		}
		return fmt.Errorf("generate default profile into pipe: %w", err)
	}

	if pipeErr := pipe.Close(); pipeErr != nil {
		logrus.Errorf("Unable to close AppArmor pipe: %q", pipeErr)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("wait for AppArmor command: %w", err)
	}

	return nil
}

// DefaultContent returns the default profile content as byte slice. The
// profile is named as the provided `name`. The function errors if the profile
// generation fails.
func DefaultContent(name string) ([]byte, error) {
	p := profileData{Name: name}
	buffer := &bytes.Buffer{}

	apparmorParserPath, err := supported.NewAppArmorVerifier().FindAppArmorParserBinary()
	if err != nil {
		return nil, fmt.Errorf("find `apparmor_parser` binary: %w", err)
	}

	if err := p.generateDefault(apparmorParserPath, buffer); err != nil {
		return nil, fmt.Errorf("generate default AppAmor profile: %w", err)
	}
	return buffer.Bytes(), nil
}

// IsLoaded checks if a profile with the given name has been loaded into the
// kernel.
func IsLoaded(name string) (bool, error) {
	if name != "" && unshare.IsRootless() {
		return false, fmt.Errorf("cannot load AppArmor profile %q: %w", name, ErrApparmorRootless)
	}

	file, err := os.Open("/sys/kernel/security/apparmor/profiles")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("open AppArmor profile path: %w", err)
	}
	defer file.Close()

	r := bufio.NewReader(file)
	for {
		p, err := r.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, fmt.Errorf("reading AppArmor profile: %w", err)
		}
		if strings.HasPrefix(p, name+" ") {
			return true, nil
		}
	}

	return false, nil
}

// execAAParser runs `apparmor_parser` with the passed arguments.
func execAAParser(apparmorParserPath, dir string, args ...string) (string, error) {
	c := exec.Command(apparmorParserPath, args...)
	c.Dir = dir

	output, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("running `%s %s` failed with output: %s\nerror: %v", c.Path, strings.Join(c.Args, " "), output, err)
	}

	return string(output), nil
}

// getAAParserVersion returns the major and minor version of apparmor_parser.
func getAAParserVersion(apparmorParserPath string) (int, error) {
	output, err := execAAParser(apparmorParserPath, "", "--version")
	if err != nil {
		return -1, fmt.Errorf("execute apparmor_parser: %w", err)
	}
	return parseAAParserVersion(output)
}

// parseAAParserVersion parses the given `apparmor_parser --version` output and
// returns the major and minor version number as an integer.
func parseAAParserVersion(output string) (int, error) {
	// output is in the form of the following:
	// AppArmor parser version 2.9.1
	// Copyright (C) 1999-2008 Novell Inc.
	// Copyright 2009-2012 Canonical Ltd.
	firstLine, _, _ := strings.Cut(output, "\n")
	words := strings.Split(firstLine, " ")
	version := words[len(words)-1]

	// trim "-beta1" suffix from version="3.0.0-beta1" if exists
	version, _, _ = strings.Cut(version, "-")
	// also trim "~..." suffix used historically (https://gitlab.com/apparmor/apparmor/-/commit/bca67d3d27d219d11ce8c9cc70612bd637f88c10)
	version, _, _ = strings.Cut(version, "~")

	// split by major minor version
	v := strings.Split(version, ".")
	if len(v) == 0 || len(v) > 3 {
		return -1, fmt.Errorf("parsing version failed for output: `%s`", output)
	}

	// Default the versions to 0.
	var majorVersion, minorVersion, patchLevel int

	majorVersion, err := strconv.Atoi(v[0])
	if err != nil {
		return -1, fmt.Errorf("convert AppArmor major version: %w", err)
	}

	if len(v) > 1 {
		minorVersion, err = strconv.Atoi(v[1])
		if err != nil {
			return -1, fmt.Errorf("convert AppArmor minor version: %w", err)
		}
	}
	if len(v) > 2 {
		patchLevel, err = strconv.Atoi(v[2])
		if err != nil {
			return -1, fmt.Errorf("convert AppArmor patch version: %w", err)
		}
	}

	// major*10^5 + minor*10^3 + patch*10^0
	numericVersion := majorVersion*1e5 + minorVersion*1e3 + patchLevel
	return numericVersion, nil
}

// CheckProfileAndLoadDefault checks if the specified profile is loaded and
// loads the DefaultLibpodProfile if the specified on is prefixed by
// DefaultLipodProfilePrefix.  This allows to always load and apply the latest
// default AppArmor profile.  Note that AppArmor requires root.  If it's a
// default profile, return DefaultLipodProfilePrefix, otherwise the specified
// one.
func CheckProfileAndLoadDefault(name string) (string, error) {
	if name == "unconfined" {
		return name, nil
	}

	// AppArmor is not supported in rootless mode as it requires root
	// privileges.  Return an error in case a specific profile is specified.
	if unshare.IsRootless() {
		if name != "" {
			return "", fmt.Errorf("cannot load AppArmor profile %q: %w", name, ErrApparmorRootless)
		}
		logrus.Debug("Skipping loading default AppArmor profile (rootless mode)")
		return "", nil
	}

	// Check if AppArmor is disabled and error out if a profile is to be set.
	if !runcaa.IsEnabled() {
		if name == "" {
			return "", nil
		}
		return "", fmt.Errorf("profile %q specified but AppArmor is disabled on the host", name)
	}

	if name == "" {
		name = Profile
	} else if !strings.HasPrefix(name, ProfilePrefix) {
		// If the specified name is not a default one, ignore it and return the
		// name.
		isLoaded, err := IsLoaded(name)
		if err != nil {
			return "", fmt.Errorf("verify if profile %s is loaded: %w", name, err)
		}
		if !isLoaded {
			return "", fmt.Errorf("AppArmor profile %q specified but not loaded", name)
		}
		return name, nil
	}

	// To avoid expensive redundant loads on each invocation, check
	// if it's loaded before installing it.
	isLoaded, err := IsLoaded(name)
	if err != nil {
		return "", fmt.Errorf("verify if profile %s is loaded: %w", name, err)
	}
	if !isLoaded {
		err = InstallDefault(name)
		if err != nil {
			return "", fmt.Errorf("install profile %s: %w", name, err)
		}
		logrus.Infof("Successfully loaded AppAmor profile %q", name)
	} else {
		logrus.Infof("AppAmor profile %q is already loaded", name)
	}

	return name, nil
}
