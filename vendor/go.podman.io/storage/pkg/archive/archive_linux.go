package archive

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/system"
	"golang.org/x/sys/unix"
)

func getOverlayOpaqueXattrName() string {
	return GetOverlayXattrName("opaque")
}

func GetWhiteoutConverter(format WhiteoutFormat, data any) TarWhiteoutConverter {
	if format == OverlayWhiteoutFormat {
		if rolayers, ok := data.([]string); ok && len(rolayers) > 0 {
			return overlayWhiteoutConverter{rolayers: rolayers}
		}
		return overlayWhiteoutConverter{rolayers: nil}
	}
	return nil
}

type overlayWhiteoutConverter struct {
	rolayers []string
}

func (o overlayWhiteoutConverter) ConvertWrite(hdr *tar.Header, path string, fi os.FileInfo) (*tar.Header, error) {
	// convert whiteouts to AUFS format
	if fi.Mode()&os.ModeCharDevice != 0 && hdr.Devmajor == 0 && hdr.Devminor == 0 {
		// we just rename the file and make it normal
		dir, filename := filepath.Split(hdr.Name)
		hdr.Name = filepath.Join(dir, WhiteoutPrefix+filename)
		hdr.Mode = 0
		hdr.Typeflag = tar.TypeReg
		hdr.Size = 0
	}

	if fi.Mode()&os.ModeDir != 0 {
		// convert opaque dirs to AUFS format by writing an empty file with the whiteout prefix
		opaque, err := system.Lgetxattr(path, getOverlayOpaqueXattrName())
		if err != nil {
			return nil, err
		}
		if len(opaque) == 1 && opaque[0] == 'y' {
			if hdr.PAXRecords != nil {
				delete(hdr.PAXRecords, PaxSchilyXattr+getOverlayOpaqueXattrName())
			}
			// If there are no lower layers, then it can't have been deleted in this layer.
			if len(o.rolayers) == 0 {
				return nil, nil //nolint: nilnil
			}
			// At this point, we have a directory that's opaque.  If it appears in one of the lower
			// layers, then it was newly-created here, so it wasn't also deleted here.
			for _, rolayer := range o.rolayers {
				stat, statErr := os.Stat(filepath.Join(rolayer, hdr.Name))
				if statErr != nil && !os.IsNotExist(statErr) && !isENOTDIR(statErr) {
					// Not sure what happened here.
					return nil, statErr
				}
				if statErr == nil {
					if stat.Mode()&os.ModeCharDevice != 0 {
						if isWhiteOut(stat) {
							return nil, nil //nolint: nilnil
						}
					}
					// It's not whiteout, so it was there in the older layer, so we need to
					// add a whiteout for this item in this layer.
					// create a header for the whiteout file
					// it should inherit some properties from the parent, but be a regular file
					wo := &tar.Header{
						Typeflag:   tar.TypeReg,
						Mode:       hdr.Mode & int64(os.ModePerm),
						Name:       filepath.Join(hdr.Name, WhiteoutOpaqueDir),
						Size:       0,
						Uid:        hdr.Uid,
						Uname:      hdr.Uname,
						Gid:        hdr.Gid,
						Gname:      hdr.Gname,
						AccessTime: hdr.AccessTime,
						ChangeTime: hdr.ChangeTime,
					}
					return wo, nil
				}
				for dir := filepath.Dir(hdr.Name); dir != "" && dir != "." && dir != string(os.PathSeparator); dir = filepath.Dir(dir) {
					// Check for whiteout for a parent directory in a parent layer.
					stat, statErr := os.Stat(filepath.Join(rolayer, dir))
					if statErr != nil && !os.IsNotExist(statErr) && !isENOTDIR(statErr) {
						// Not sure what happened here.
						return nil, statErr
					}
					if statErr == nil {
						if stat.Mode()&os.ModeCharDevice != 0 {
							// If it's whiteout for a parent directory, then the
							// original directory wasn't inherited into this layer,
							// so we don't need to emit whiteout for it.
							if isWhiteOut(stat) {
								return nil, nil //nolint: nilnil
							}
						}
					}
				}
			}
		}
	}

	return nil, nil
}

func (overlayWhiteoutConverter) ConvertReadWithHandler(hdr *tar.Header, path string, handler TarWhiteoutHandler) (bool, error) {
	base := filepath.Base(path)
	dir := filepath.Dir(path)

	// if a directory is marked as opaque by the AUFS special file, we need to translate that to overlay
	if base == WhiteoutOpaqueDir {
		err := handler.Setxattr(dir, getOverlayOpaqueXattrName(), []byte{'y'})
		// don't write the file itself
		return false, err
	}

	// if a file was deleted and we are using overlay, we need to create a character device
	if originalBase, ok := strings.CutPrefix(base, WhiteoutPrefix); ok {
		originalPath := filepath.Join(dir, originalBase)

		if err := handler.Mknod(originalPath, unix.S_IFCHR, 0); err != nil {
			// If someone does:
			//     rm -rf /foo/bar
			// in an image, some tools will generate a layer with:
			//     /.wh.foo
			//     /foo/.wh.bar
			// and when doing the second mknod(), we will fail with
			// ENOTDIR, since the previous /foo was mknod()'d as a
			// character device node and not a directory.
			if isENOTDIR(err) {
				return false, nil
			}
			return false, err
		}
		if err := handler.Chown(originalPath, hdr.Uid, hdr.Gid); err != nil {
			return false, err
		}

		// don't write the file itself
		return false, nil
	}

	return true, nil
}

type directHandler struct{}

func (d directHandler) Setxattr(path, name string, value []byte) error {
	return unix.Setxattr(path, name, value, 0)
}

func (d directHandler) Mknod(path string, mode uint32, dev int) error {
	return unix.Mknod(path, mode, dev)
}

func (d directHandler) Chown(path string, uid, gid int) error {
	return idtools.SafeChown(path, uid, gid)
}

func (o overlayWhiteoutConverter) ConvertRead(hdr *tar.Header, path string) (bool, error) {
	var handler directHandler
	return o.ConvertReadWithHandler(hdr, path, handler)
}

func isWhiteOut(stat os.FileInfo) bool {
	s := stat.Sys().(*syscall.Stat_t)
	return major(uint64(s.Rdev)) == 0 && minor(uint64(s.Rdev)) == 0 //nolint:unconvert
}

func GetFileOwner(path string) (uint32, uint32, uint32, error) {
	f, err := os.Stat(path)
	if err != nil {
		return 0, 0, 0, err
	}
	s, ok := f.Sys().(*syscall.Stat_t)
	if ok {
		return s.Uid, s.Gid, s.Mode & 0o7777, nil
	}
	return 0, 0, uint32(f.Mode()), nil
}

func handleLChmod(hdr *tar.Header, path string, hdrInfo os.FileInfo, forceMask *os.FileMode) error {
	permissionsMask := hdrInfo.Mode()
	if forceMask != nil {
		permissionsMask = *forceMask
	}
	if hdr.Typeflag == tar.TypeLink {
		if fi, err := os.Lstat(hdr.Linkname); err == nil && (fi.Mode()&os.ModeSymlink == 0) {
			if err := os.Chmod(path, permissionsMask); err != nil {
				return err
			}
		}
	} else if hdr.Typeflag != tar.TypeSymlink {
		if err := os.Chmod(path, permissionsMask); err != nil {
			return err
		}
	}
	return nil
}
