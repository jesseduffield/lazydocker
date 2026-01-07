package chrootarchive

import (
	stdtar "archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/unshare"
)

// NewArchiver returns a new Archiver which uses chrootarchive.Untar
func NewArchiver(idMappings *idtools.IDMappings) *archive.Archiver {
	archiver := archive.NewArchiver(idMappings)
	archiver.Untar = Untar
	return archiver
}

// NewArchiverWithChown returns a new Archiver which uses chrootarchive.Untar and the provided ID mapping configuration on both ends
func NewArchiverWithChown(tarIDMappings *idtools.IDMappings, chownOpts *idtools.IDPair, untarIDMappings *idtools.IDMappings) *archive.Archiver {
	archiver := archive.NewArchiverWithChown(tarIDMappings, chownOpts, untarIDMappings)
	archiver.Untar = Untar
	return archiver
}

// Untar reads a stream of bytes from `archive`, parses it as a tar archive,
// and unpacks it into the directory at `dest`.
// The archive may be compressed with one of the following algorithms:
//
//	identity (uncompressed), gzip, bzip2, xz.
func Untar(tarArchive io.Reader, dest string, options *archive.TarOptions) error {
	return untarHandler(tarArchive, dest, options, true, dest)
}

// UntarWithRoot is the same as `Untar`, but allows you to pass in a root directory
// The root directory is the directory that will be chrooted to.
// `dest` must be a path within `root`, if it is not an error will be returned.
//
// `root` should set to a directory which is not controlled by any potentially
// malicious process.
//
// This should be used to prevent a potential attacker from manipulating `dest`
// such that it would provide access to files outside of `dest` through things
// like symlinks. Normally `ResolveSymlinksInScope` would handle this, however
// sanitizing symlinks in this manner is inherently racey:
// ref: CVE-2018-15664
func UntarWithRoot(tarArchive io.Reader, dest string, options *archive.TarOptions, root string) error {
	return untarHandler(tarArchive, dest, options, true, root)
}

// UntarUncompressed reads a stream of bytes from `archive`, parses it as a tar archive,
// and unpacks it into the directory at `dest`.
// The archive must be an uncompressed stream.
func UntarUncompressed(tarArchive io.Reader, dest string, options *archive.TarOptions) error {
	return untarHandler(tarArchive, dest, options, false, dest)
}

// Handler for teasing out the automatic decompression
func untarHandler(tarArchive io.Reader, dest string, options *archive.TarOptions, decompress bool, root string) error {
	if tarArchive == nil {
		return fmt.Errorf("empty archive")
	}
	if options == nil {
		options = &archive.TarOptions{}
		options.InUserNS = unshare.IsRootless()
	}

	idMappings := idtools.NewIDMappingsFromMaps(options.UIDMaps, options.GIDMaps)
	rootIDs := idMappings.RootPair()

	dest = filepath.Clean(dest)
	if err := fileutils.Exists(dest); os.IsNotExist(err) {
		if err := idtools.MkdirAllAndChownNew(dest, 0o755, rootIDs); err != nil {
			return err
		}
	}

	destVal, err := newUnpackDestination(root, dest)
	if err != nil {
		return err
	}
	defer destVal.Close()

	r := tarArchive
	if decompress {
		decompressedArchive, err := archive.DecompressStream(tarArchive)
		if err != nil {
			return err
		}
		defer decompressedArchive.Close()
		r = decompressedArchive
	}

	return invokeUnpack(r, destVal, options)
}

// Tar tars the requested path while chrooted to the specified root.
func Tar(srcPath string, options *archive.TarOptions, root string) (io.ReadCloser, error) {
	if options == nil {
		options = &archive.TarOptions{}
	}
	return invokePack(srcPath, options, root)
}

// CopyFileWithTarAndChown returns a function which copies a single file from outside
// of any container into our working container, mapping permissions using the
// container's ID maps, possibly overridden using the passed-in chownOpts
func CopyFileWithTarAndChown(chownOpts *idtools.IDPair, hasher io.Writer, uidmap []idtools.IDMap, gidmap []idtools.IDMap) func(src, dest string) error {
	untarMappings := idtools.NewIDMappingsFromMaps(uidmap, gidmap)
	archiver := NewArchiverWithChown(nil, chownOpts, untarMappings)
	if hasher != nil {
		originalUntar := archiver.Untar
		archiver.Untar = func(tarArchive io.Reader, dest string, options *archive.TarOptions) error {
			contentReader, contentWriter, err := os.Pipe()
			if err != nil {
				return fmt.Errorf("creating pipe extract data to %q: %w", dest, err)
			}
			defer contentReader.Close()
			defer contentWriter.Close()
			var hashError error
			var hashWorker sync.WaitGroup
			hashWorker.Add(1)
			go func() {
				t := stdtar.NewReader(contentReader)
				_, err := t.Next()
				if err != nil {
					hashError = err
				}
				if _, err = io.Copy(hasher, t); err != nil && err != io.EOF {
					hashError = err
				}
				hashWorker.Done()
			}()
			if err = originalUntar(io.TeeReader(tarArchive, contentWriter), dest, options); err != nil {
				err = fmt.Errorf("extracting data to %q while copying: %w", dest, err)
			}
			hashWorker.Wait()
			if err == nil && hashError != nil {
				err = fmt.Errorf("calculating digest of data for %q while copying: %w", dest, hashError)
			}
			return err
		}
	}
	return archiver.CopyFileWithTar
}

// CopyWithTarAndChown returns a function which copies a directory tree from outside of
// any container into our working container, mapping permissions using the
// container's ID maps, possibly overridden using the passed-in chownOpts
func CopyWithTarAndChown(chownOpts *idtools.IDPair, hasher io.Writer, uidmap []idtools.IDMap, gidmap []idtools.IDMap) func(src, dest string) error {
	untarMappings := idtools.NewIDMappingsFromMaps(uidmap, gidmap)
	archiver := NewArchiverWithChown(nil, chownOpts, untarMappings)
	if hasher != nil {
		originalUntar := archiver.Untar
		archiver.Untar = func(tarArchive io.Reader, dest string, options *archive.TarOptions) error {
			return originalUntar(io.TeeReader(tarArchive, hasher), dest, options)
		}
	}
	return archiver.CopyWithTar
}

// UntarPathAndChown returns a function which extracts an archive in a specified
// location into our working container, mapping permissions using the
// container's ID maps, possibly overridden using the passed-in chownOpts
func UntarPathAndChown(chownOpts *idtools.IDPair, hasher io.Writer, uidmap []idtools.IDMap, gidmap []idtools.IDMap) func(src, dest string) error {
	untarMappings := idtools.NewIDMappingsFromMaps(uidmap, gidmap)
	archiver := NewArchiverWithChown(nil, chownOpts, untarMappings)
	if hasher != nil {
		originalUntar := archiver.Untar
		archiver.Untar = func(tarArchive io.Reader, dest string, options *archive.TarOptions) error {
			return originalUntar(io.TeeReader(tarArchive, hasher), dest, options)
		}
	}
	return archiver.UntarPath
}
