package buildah

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/containers/buildah/define"
	"github.com/containers/buildah/internal/sbom"
	"github.com/mattn/go-shellwords"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

func stringSliceReplaceAll(slice []string, replacements map[string]string, important []string) (built []string, replacedAnImportantValue bool) {
	built = make([]string, 0, len(slice))
	for i := range slice {
		element := slice[i]
		for from, to := range replacements {
			previous := element
			if element = strings.ReplaceAll(previous, from, to); element != previous {
				if len(important) == 0 || slices.Contains(important, from) {
					replacedAnImportantValue = true
				}
			}
		}
		built = append(built, element)
	}
	return built, replacedAnImportantValue
}

// sbomScan iterates through the scanning configuration settings, generating
// SBOM files and storing them either in the rootfs or in a local file path.
func (b *Builder) sbomScan(ctx context.Context, options CommitOptions) (imageFiles, localFiles map[string]string, scansDir string, err error) {
	// We'll use a temporary per-container directory for this one.
	cdir, err := b.store.ContainerDirectory(b.ContainerID)
	if err != nil {
		return nil, nil, "", err
	}
	scansDir, err = os.MkdirTemp(cdir, "buildah-scan")
	if err != nil {
		return nil, nil, "", err
	}
	defer func() {
		if err != nil {
			if err := os.RemoveAll(scansDir); err != nil {
				logrus.Warnf("removing temporary directory %q: %v", scansDir, err)
			}
		}
	}()
	scansSubdir := filepath.Join(scansDir, "scans")
	if err = os.Mkdir(scansSubdir, 0o700); err != nil {
		return nil, nil, "", err
	}
	if err = os.Chmod(scansSubdir, 0o777); err != nil {
		return nil, nil, "", err
	}

	// We may be producing sets of outputs using temporary containers, and
	// there's no need to create more than one container for any one
	// specific scanner image.
	scanners := make(map[string]*Builder)
	defer func() {
		for _, scanner := range scanners {
			scannerID := scanner.ContainerID
			if err := scanner.Delete(); err != nil {
				logrus.Warnf("removing temporary scanner container %q: %v", scannerID, err)
			}
		}
	}()

	// Just assume that every scanning method will be looking at the rootfs.
	rootfs, err := b.Mount(b.MountLabel)
	if err != nil {
		return nil, nil, "", err
	}
	defer func(b *Builder) {
		if err := b.Unmount(); err != nil {
			logrus.Warnf("unmounting temporary scanner container %q: %v", b.ContainerID, err)
		}
	}(b)

	// Iterate through all of the scanning strategies.
	for _, scanSpec := range options.SBOMScanOptions {
		// Pull the image and create a container we can run the scanner
		// in, unless we've done that already for this scanner image.
		scanBuilder, ok := scanners[scanSpec.Image]
		if !ok {
			builderOptions := BuilderOptions{
				FromImage:        scanSpec.Image,
				ContainerSuffix:  "scanner",
				PullPolicy:       scanSpec.PullPolicy,
				BlobDirectory:    options.BlobDirectory,
				Logger:           b.Logger,
				SystemContext:    options.SystemContext,
				MountLabel:       b.MountLabel,
				ProcessLabel:     b.ProcessLabel,
				IDMappingOptions: &b.IDMappingOptions,
			}
			if scanBuilder, err = NewBuilder(ctx, b.store, builderOptions); err != nil {
				return nil, nil, "", fmt.Errorf("creating temporary working container to run scanner: %w", err)
			}
			scanners[scanSpec.Image] = scanBuilder
		}
		// Now figure out which commands we need to run.  First, try to
		// parse a command ourselves, because syft's image (at least)
		// doesn't include a shell.  Build a slice of command slices.
		var commands [][]string
		for _, commandSpec := range scanSpec.Commands {
			// Start by assuming it's shell -c $whatever.
			parsedCommand := []string{"/bin/sh", "-c", commandSpec}
			if shell := scanBuilder.Shell(); len(shell) != 0 {
				parsedCommand = append(slices.Clone(shell), commandSpec)
			}
			if !strings.ContainsAny(commandSpec, "<>|") { // An imperfect check for shell redirection being used.
				// If we can parse it ourselves, though, prefer to use that result,
				// in case the scanner image doesn't include a shell.
				if parsed, err := shellwords.Parse(commandSpec); err == nil {
					parsedCommand = parsed
				}
			}
			commands = append(commands, parsedCommand)
		}
		// Set up a list of mounts for the rootfs and whichever context
		// directories we're told were used.
		const rootfsTargetDir = "/.rootfs"
		const scansTargetDir = "/.scans"
		const contextsTargetDirPrefix = "/.context"
		runMounts := []rspec.Mount{
			// Our temporary directory, read-write.
			{
				Type:        define.TypeBind,
				Source:      scansSubdir,
				Destination: scansTargetDir,
				Options:     []string{"rw", "z"},
			},
			// The rootfs, read-only.
			{
				Type:        define.TypeBind,
				Source:      rootfs,
				Destination: rootfsTargetDir,
				Options:     []string{"ro"},
			},
		}
		// Each context directory, also read-only.
		for i := range scanSpec.ContextDir {
			contextMount := rspec.Mount{
				Type:        define.TypeBind,
				Source:      scanSpec.ContextDir[i],
				Destination: fmt.Sprintf("%s%d", contextsTargetDirPrefix, i),
				Options:     []string{"ro"},
			}
			runMounts = append(runMounts, contextMount)
		}
		// Set up run options and mounts one time, and reuse it.
		runOptions := RunOptions{
			Logger:        b.Logger,
			Isolation:     b.Isolation,
			SystemContext: options.SystemContext,
			Mounts:        runMounts,
		}
		// We'll have to do some text substitutions so that we run the
		// right commands, in the right order, pointing at the right
		// mount points.
		var resolvedCommands [][]string
		var resultFiles []string
		for _, command := range commands {
			// Each command gets to produce its own file that we'll
			// combine later if there's more than one of them.
			contextDirScans := 0
			for i := range scanSpec.ContextDir {
				resultFile := filepath.Join(scansTargetDir, fmt.Sprintf("scan%d.json", len(resultFiles)))
				// If the command mentions {CONTEXT}...
				resolvedCommand, scansContext := stringSliceReplaceAll(command,
					map[string]string{
						"{CONTEXT}": fmt.Sprintf("%s%d", contextsTargetDirPrefix, i),
						"{OUTPUT}":  resultFile,
					},
					[]string{"{CONTEXT}"},
				)
				if !scansContext {
					break
				}
				// ... resolve the path references and add it to the list of commands.
				resolvedCommands = append(resolvedCommands, resolvedCommand)
				resultFiles = append(resultFiles, resultFile)
				contextDirScans++
			}
			if contextDirScans == 0 {
				resultFile := filepath.Join(scansTargetDir, fmt.Sprintf("scan%d.json", len(resultFiles)))
				// If the command didn't mention {CONTEXT}, but does mention {ROOTFS}...
				resolvedCommand, scansRootfs := stringSliceReplaceAll(command,
					map[string]string{
						"{ROOTFS}": rootfsTargetDir,
						"{OUTPUT}": resultFile,
					},
					[]string{"{ROOTFS}"},
				)
				// ... resolve the path references and add that to the list of commands.
				if scansRootfs {
					resolvedCommands = append(resolvedCommands, resolvedCommand)
					resultFiles = append(resultFiles, resultFile)
				}
			}
		}
		// Run all of the commands, one after the other, producing one
		// or more files named "scan%d.json" in our temporary directory.
		for _, resolvedCommand := range resolvedCommands {
			logrus.Debugf("Running scan command %q", resolvedCommand)
			if err = scanBuilder.Run(resolvedCommand, runOptions); err != nil {
				return nil, nil, "", fmt.Errorf("running scanning command %v: %w", resolvedCommand, err)
			}
		}
		// Produce the combined output files that we need to create, if there are any.
		var sbomResult, purlResult string
		switch {
		case scanSpec.ImageSBOMOutput != "":
			sbomResult = filepath.Join(scansSubdir, filepath.Base(scanSpec.ImageSBOMOutput))
		case scanSpec.SBOMOutput != "":
			sbomResult = filepath.Join(scansSubdir, filepath.Base(scanSpec.SBOMOutput))
		default:
			sbomResult = filepath.Join(scansSubdir, "sbom-result")
		}
		switch {
		case scanSpec.ImagePURLOutput != "":
			purlResult = filepath.Join(scansSubdir, filepath.Base(scanSpec.ImagePURLOutput))
		case scanSpec.PURLOutput != "":
			purlResult = filepath.Join(scansSubdir, filepath.Base(scanSpec.PURLOutput))
		default:
			purlResult = filepath.Join(scansSubdir, "purl-result")
		}
		copyFile := func(destination, source string) error {
			dst, err := os.Create(destination)
			if err != nil {
				return err
			}
			defer dst.Close()
			src, err := os.Open(source)
			if err != nil {
				return err
			}
			defer src.Close()
			if _, err = io.Copy(dst, src); err != nil {
				return fmt.Errorf("copying %q to %q: %w", source, destination, err)
			}
			return nil
		}
		err = func() error {
			for i := range resultFiles {
				thisResultFile := filepath.Join(scansSubdir, filepath.Base(resultFiles[i]))
				switch i {
				case 0:
					// Straight-up copy to create the first version of the final output.
					if err = copyFile(sbomResult, thisResultFile); err != nil {
						return err
					}
					// This shouldn't change any contents, but lets us generate the purl file.
					err = sbom.Merge(scanSpec.MergeStrategy, thisResultFile, sbomResult, purlResult)
				default:
					// Hopefully we know how to merge information from the new one into the final output.
					err = sbom.Merge(scanSpec.MergeStrategy, sbomResult, thisResultFile, purlResult)
				}
			}
			return err
		}()
		if err != nil {
			return nil, nil, "", err
		}
		// If these files are supposed to be written to the local filesystem, add
		// their contents to the map of files we expect our caller to write.
		if scanSpec.SBOMOutput != "" || scanSpec.PURLOutput != "" {
			if localFiles == nil {
				localFiles = make(map[string]string)
			}
			if scanSpec.SBOMOutput != "" {
				localFiles[scanSpec.SBOMOutput] = sbomResult
			}
			if scanSpec.PURLOutput != "" {
				localFiles[scanSpec.PURLOutput] = purlResult
			}
		}
		// If these files are supposed to be written to the image, create a map of
		// their contents so that we can either create a layer diff for them (or
		// slipstream them into a squashed layer diff) later.
		if scanSpec.ImageSBOMOutput != "" || scanSpec.ImagePURLOutput != "" {
			if imageFiles == nil {
				imageFiles = make(map[string]string)
			}
			if scanSpec.ImageSBOMOutput != "" {
				imageFiles[scanSpec.ImageSBOMOutput] = sbomResult
			}
			if scanSpec.ImagePURLOutput != "" {
				imageFiles[scanSpec.ImagePURLOutput] = purlResult
			}
		}
	}
	return imageFiles, localFiles, scansDir, nil
}
