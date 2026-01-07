package sif

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/sylabs/sif/v2/pkg/sif"
)

// injectedScriptTargetPath is the path injectedScript should be written to in the created image.
const injectedScriptTargetPath = "/podman/runscript"

// parseDefFile parses a SIF definition file from reader,
// and returns non-trivial contents of the %environment and %runscript sections.
func parseDefFile(reader io.Reader) ([]string, []string, error) {
	type parserState int
	const (
		parsingOther parserState = iota
		parsingEnvironment
		parsingRunscript
	)

	environment := []string{}
	runscript := []string{}

	state := parsingOther
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		s := strings.TrimSpace(scanner.Text())
		switch {
		case s == `%environment`:
			state = parsingEnvironment
		case s == `%runscript`:
			state = parsingRunscript
		case strings.HasPrefix(s, "%"):
			state = parsingOther
		case state == parsingEnvironment:
			if s != "" && !strings.HasPrefix(s, "#") {
				environment = append(environment, s)
			}
		case state == parsingRunscript:
			runscript = append(runscript, s)
		default: // parsingOther: ignore the line
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("reading lines from SIF definition file object: %w", err)
	}
	return environment, runscript, nil
}

// generateInjectedScript generates a shell script based on
// SIF definition file %environment and %runscript data, and returns it.
func generateInjectedScript(environment []string, runscript []string) []byte {
	script := fmt.Sprintf("#!/bin/bash\n"+
		"%s\n"+
		"%s\n", strings.Join(environment, "\n"), strings.Join(runscript, "\n"))
	return []byte(script)
}

// processDefFile finds sif.DataDeffile in sifImage, if any,
// and returns:
// - the command to run
// - contents of a script to inject as injectedScriptTargetPath, or nil
func processDefFile(sifImage *sif.FileImage) (string, []byte, error) {
	var environment, runscript []string

	desc, err := sifImage.GetDescriptor(sif.WithDataType(sif.DataDeffile))
	if err == nil {
		environment, runscript, err = parseDefFile(desc.GetReader())
		if err != nil {
			return "", nil, err
		}
	}

	var command string
	var injectedScript []byte
	if len(environment) == 0 && len(runscript) == 0 {
		command = "bash"
		injectedScript = nil
	} else {
		injectedScript = generateInjectedScript(environment, runscript)
		command = injectedScriptTargetPath
	}

	return command, injectedScript, nil
}

func writeInjectedScript(extractedRootPath string, injectedScript []byte) error {
	if injectedScript == nil {
		return nil
	}
	filePath := filepath.Join(extractedRootPath, injectedScriptTargetPath)
	parentDirPath := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDirPath, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", parentDirPath, err)
	}
	if err := os.WriteFile(filePath, injectedScript, 0755); err != nil {
		return fmt.Errorf("writing %s to %s: %w", injectedScriptTargetPath, filePath, err)
	}
	return nil
}

// createTarFromSIFInputs creates a tar file at tarPath, using a squashfs image at squashFSPath.
// It can also use extractedRootPath and scriptPath, which are allocated for its exclusive use,
// if necessary.
func createTarFromSIFInputs(ctx context.Context, tarPath, squashFSPath string, injectedScript []byte, extractedRootPath, scriptPath string) error {
	// It's safe for the Remove calls to happen even before we create the files, because tempDir is exclusive
	// for our use.
	defer os.RemoveAll(extractedRootPath)

	// Almost everything in extractedRootPath comes from squashFSPath.
	conversionCommand := fmt.Sprintf("unsquashfs -d %s -f %s && tar --acls --xattrs -C %s -cpf %s ./",
		extractedRootPath, squashFSPath, extractedRootPath, tarPath)
	script := "#!/bin/sh\n" + conversionCommand + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	// On top of squashFSPath, we only add injectedScript, if necessary.
	if err := writeInjectedScript(extractedRootPath, injectedScript); err != nil {
		return err
	}

	logrus.Debugf("Converting squashfs to tar, command: %s ...", conversionCommand)
	cmd := exec.CommandContext(ctx, "fakeroot", "--", scriptPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("converting image: %w, output: %s", err, string(output))
	}
	logrus.Debugf("... finished converting squashfs to tar")
	return nil
}

// convertSIFToElements processes sifImage and creates/returns
// the relevant elements for constructing an OCI-like image:
// - A path to a tar file containing a root filesystem,
// - A command to run.
// The returned tar file path is inside tempDir, which can be assumed to be empty
// at start, and is exclusively used by the current process (i.e. it is safe
// to use hard-coded relative paths within it).
func convertSIFToElements(ctx context.Context, sifImage *sif.FileImage, tempDir string) (string, []string, error) {
	// We could allocate unique names for all of these using os.{CreateTemp,MkdirTemp}, but tempDir is exclusive,
	// so we can just hard-code a set of unique values here.
	// We create and/or manage cleanup of these two paths.
	squashFSPath := filepath.Join(tempDir, "rootfs.squashfs")
	tarPath := filepath.Join(tempDir, "rootfs.tar")
	// We only allocate these paths, the user is responsible for cleaning them up.
	extractedRootPath := filepath.Join(tempDir, "rootfs")
	scriptPath := filepath.Join(tempDir, "script")

	succeeded := false
	// It's safe for the Remove calls to happen even before we create the files, because tempDir is exclusive
	// for our use.
	// Ideally we would remove squashFSPath immediately after creating extractedRootPath, but we need
	// to run both creation and consumption of extractedRootPath in the same fakeroot context.
	// So, overall, this process requires at least 2 compressed copies (SIF and squashFSPath) and 2
	// uncompressed copies (extractedRootPath and tarPath) of the data, all using up space at the same time.
	// That's rather unsatisfactory, ideally we would be streaming the data directly from a squashfs parser
	// reading from the SIF file to a tarball, for 1 compressed and 1 uncompressed copy.
	defer os.Remove(squashFSPath)
	defer func() {
		if !succeeded {
			os.Remove(tarPath)
		}
	}()

	command, injectedScript, err := processDefFile(sifImage)
	if err != nil {
		return "", nil, err
	}

	rootFS, err := sifImage.GetDescriptor(sif.WithPartitionType(sif.PartPrimSys))
	if err != nil {
		return "", nil, fmt.Errorf("looking up rootfs from SIF file: %w", err)
	}
	// TODO: We'd prefer not to make a full copy of the file here; unsquashfs ≥ 4.4
	// has an -o option that allows extracting a squashfs from the SIF file directly,
	// but that version is not currently available in RHEL 8.
	logrus.Debugf("Creating a temporary squashfs image %s ...", squashFSPath)
	if err := func() (retErr error) { // A scope for defer
		f, err := os.Create(squashFSPath)
		if err != nil {
			return err
		}
		// since we are writing to this file, make sure we handle err on Close()
		defer func() {
			closeErr := f.Close()
			if retErr == nil {
				retErr = closeErr
			}
		}()
		// TODO: This can take quite some time, and should ideally be cancellable using ctx.Done().
		if _, err := io.CopyN(f, rootFS.GetReader(), rootFS.Size()); err != nil {
			return err
		}
		return nil
	}(); err != nil {
		return "", nil, err
	}
	logrus.Debugf("... finished creating a temporary squashfs image")

	if err := createTarFromSIFInputs(ctx, tarPath, squashFSPath, injectedScript, extractedRootPath, scriptPath); err != nil {
		return "", nil, err
	}
	succeeded = true
	return tarPath, []string{command}, nil
}
