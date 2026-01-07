package version

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"go.podman.io/storage/pkg/fileutils"
)

const (
	UnknownPackage = "Unknown"
)

// Note: This function is copied from containers/podman libpod/util.go
// Please see https://github.com/containers/common/pull/1460
func queryPackageVersion(cmdArg ...string) string {
	err := fileutils.Exists(cmdArg[0])
	if err != nil {
		return ""
	}
	output := UnknownPackage
	if 1 < len(cmdArg) {
		cmd := exec.Command(cmdArg[0], cmdArg[1:]...)
		if outp, err := cmd.Output(); err == nil {
			output = string(outp)
			switch cmdArg[0] {
			case "/usr/bin/dlocate":
				// can return multiple matches
				output, _, _ := strings.Cut(output, "\n")
				r, _, _ := strings.Cut(output, ": ")
				regexpFormat := `^..\s` + r + `\s`
				cmd = exec.Command(cmdArg[0], "-P", regexpFormat, "-l")
				cmd.Env = []string{"COLUMNS=160"} // show entire value
				// dlocate always returns exit code 1 for list command
				if outp, _ = cmd.Output(); len(outp) > 0 {
					lines := strings.Split(string(outp), "\n")
					if len(lines) > 1 {
						line := lines[len(lines)-2] // trailing newline
						f := strings.Fields(line)
						if len(f) >= 2 {
							return f[1] + "_" + f[2]
						}
					}
				}
			case "/usr/bin/dpkg":
				r, _, _ := strings.Cut(output, ": ")
				queryFormat := `${Package}_${Version}_${Architecture}`
				cmd = exec.Command("/usr/bin/dpkg-query", "-f", queryFormat, "-W", r)
				if outp, err := cmd.Output(); err == nil {
					output = string(outp)
				}
			case "/usr/bin/pacman":
				pkg := strings.Trim(output, "\n")
				cmd = exec.Command(cmdArg[0], "-Q", "--", pkg)
				if outp, err := cmd.Output(); err == nil {
					output = strings.ReplaceAll(string(outp), " ", "-")
				}
			case "/sbin/apk":
				prefix := cmdArg[len(cmdArg)-1] + " is owned by "
				output = strings.Replace(output, prefix, "", 1)
			}
		}
	}
	return strings.Trim(output, "\n")
}

// Package tries to query the package information of the given program path.
// Note it must be an absolute path.
func Package(program string) string {
	// Note: This function is copied from containers/podman libpod/util.go
	// Please see https://github.com/containers/common/pull/1460
	err := fileutils.Exists(program)
	if err != nil {
		return UnknownPackage
	}

	type Packager struct {
		Format  string
		Command []string
	}
	packagers := []Packager{
		{"rpm", []string{"/usr/bin/rpm", "-q", "-f"}},
		{"deb", []string{"/usr/bin/dlocate", "-F"}},             // Debian, Ubuntu (quick)
		{"deb", []string{"/usr/bin/dpkg", "-S"}},                // Debian, Ubuntu (slow)
		{"pacman", []string{"/usr/bin/pacman", "-Qoq"}},         // Arch
		{"gentoo", []string{"/usr/bin/qfile", "-qv"}},           // Gentoo (quick)
		{"gentoo", []string{"/usr/bin/equery", "b"}},            // Gentoo (slow)
		{"apk", []string{"/sbin/apk", "info", "-W"}},            // Alpine
		{"pkg", []string{"/usr/local/sbin/pkg", "which", "-q"}}, // FreeBSD
	}

	lastformat := ""
	for _, packager := range packagers {
		if packager.Format == lastformat {
			continue
		}
		cmd := packager.Command
		cmd = append(cmd, program)
		if out := queryPackageVersion(cmd...); out != UnknownPackage {
			if out == "" {
				continue
			}
			return out
		}
		lastformat = packager.Format
	}
	return UnknownPackage
}

// Program returns the --version output as string of the given command.
func Program(name string) (string, error) {
	// Note: This function is copied from containers/podman libpod/util.go
	// Please see https://github.com/containers/common/pull/1460
	return program(name, false)
}

func ProgramDnsname(name string) (string, error) {
	return program(name, true)
}

func program(program string, dnsname bool) (string, error) {
	cmd := exec.Command(program, "--version")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("`%v --version` failed: %v %v (%v)", program, stderr.String(), stdout.String(), err)
	}

	output := strings.TrimSuffix(stdout.String(), "\n")
	// dnsname --version returns the information to stderr
	if dnsname {
		output = strings.TrimSuffix(stderr.String(), "\n")
	}

	return output, nil
}
