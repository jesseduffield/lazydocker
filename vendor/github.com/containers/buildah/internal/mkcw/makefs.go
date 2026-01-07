package mkcw

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

// MakeFS formats the imageFile as a filesystem of the specified type,
// populating it with the contents of the directory at sourcePath.
// Recognized filesystem types are "ext2", "ext3", "ext4", and "btrfs".
// Note that krun's init is currently hard-wired to assume "ext4".
// Returns the stdout, stderr, and any error returned by the mkfs command.
func MakeFS(sourcePath, imageFile, filesystem string) (string, string, error) {
	var stdout, stderr strings.Builder
	// N.B. mkfs.xfs can accept a protofile via its -p option, but the
	// protofile format doesn't allow us to supply timestamp information or
	// specify that files are hard linked
	switch filesystem {
	case "ext2", "ext3", "ext4":
		logrus.Debugf("mkfs -t %s --rootdir %q %q", filesystem, sourcePath, imageFile)
		cmd := exec.Command("mkfs", "-t", filesystem, "-d", sourcePath, imageFile)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	case "btrfs":
		logrus.Debugf("mkfs -t %s --rootdir %q %q", filesystem, sourcePath, imageFile)
		cmd := exec.Command("mkfs", "-t", filesystem, "--rootdir", sourcePath, imageFile)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}
	return "", "", fmt.Errorf("don't know how to make a %q filesystem with contents", filesystem)
}
