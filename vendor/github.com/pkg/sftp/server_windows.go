package sftp

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

func (s *Server) toLocalPath(p string) string {
	if s.workDir != "" && !path.IsAbs(p) {
		p = path.Join(s.workDir, p)
	}

	lp := filepath.FromSlash(p)

	if path.IsAbs(p) { // starts with '/'
		if len(p) == 1 && s.winRoot {
			return `\\.\` // for openfile
		}

		tmp := lp
		for len(tmp) > 0 && tmp[0] == '\\' {
			tmp = tmp[1:]
		}

		if filepath.IsAbs(tmp) {
			// If the FromSlash without any starting slashes is absolute,
			// then we have a filepath encoded with a prefix '/'.
			// e.g. "/C:/Windows" to "C:\\Windows"
			return tmp
		}

		tmp += "\\"

		if filepath.IsAbs(tmp) {
			// If the FromSlash without any starting slashes but with extra end slash is absolute,
			// then we have a filepath encoded with a prefix '/' and a dropped '/' at the end.
			// e.g. "/C:" to "C:\\"
			return tmp
		}

		if s.winRoot {
			// Make it so that "/Windows" is not found, and "/c:/Windows" has to be used
			return `\\.\` + tmp
		}
	}

	return lp
}

func bitsToDrives(bitmap uint32) []string {
	var drive rune = 'a'
	var drives []string

	for bitmap != 0 && drive <= 'z' {
		if bitmap&1 == 1 {
			drives = append(drives, string(drive)+":")
		}
		drive++
		bitmap >>= 1
	}

	return drives
}

func getDrives() ([]string, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, fmt.Errorf("GetLogicalDrives: %w", err)
	}
	return bitsToDrives(mask), nil
}

type driveInfo struct {
	fs.FileInfo
	name string
}

func (i *driveInfo) Name() string {
	return i.name // since the Name() returned from a os.Stat("C:\\") is "\\"
}

type winRoot struct {
	drives []string
}

func newWinRoot() (*winRoot, error) {
	drives, err := getDrives()
	if err != nil {
		return nil, err
	}
	return &winRoot{
		drives: drives,
	}, nil
}

func (f *winRoot) Readdir(n int) ([]os.FileInfo, error) {
	drives := f.drives
	if n > 0 && len(drives) > n {
		drives = drives[:n]
	}
	f.drives = f.drives[len(drives):]
	if len(drives) == 0 {
		return nil, io.EOF
	}

	var infos []os.FileInfo
	for _, drive := range drives {
		fi, err := os.Stat(drive + `\`)
		if err != nil {
			return nil, err
		}

		di := &driveInfo{
			FileInfo: fi,
			name:     drive,
		}
		infos = append(infos, di)
	}

	return infos, nil
}

func (f *winRoot) Stat() (os.FileInfo, error) {
	return rootFileInfo, nil
}
func (f *winRoot) ReadAt(b []byte, off int64) (int, error) {
	return 0, os.ErrPermission
}
func (f *winRoot) WriteAt(b []byte, off int64) (int, error) {
	return 0, os.ErrPermission
}
func (f *winRoot) Name() string {
	return "/"
}
func (f *winRoot) Truncate(int64) error {
	return os.ErrPermission
}
func (f *winRoot) Chmod(mode fs.FileMode) error {
	return os.ErrPermission
}
func (f *winRoot) Chown(uid, gid int) error {
	return os.ErrPermission
}
func (f *winRoot) Close() error {
	f.drives = nil
	return nil
}

func (s *Server) openfile(path string, flag int, mode fs.FileMode) (file, error) {
	if path == `\\.\` && s.winRoot {
		return newWinRoot()
	}
	return os.OpenFile(path, flag, mode)
}

type winRootFileInfo struct {
	name    string
	modTime time.Time
}

func (w *winRootFileInfo) Name() string       { return w.name }
func (w *winRootFileInfo) Size() int64        { return 0 }
func (w *winRootFileInfo) Mode() fs.FileMode  { return fs.ModeDir | 0555 } // read+execute for all
func (w *winRootFileInfo) ModTime() time.Time { return w.modTime }
func (w *winRootFileInfo) IsDir() bool        { return true }
func (w *winRootFileInfo) Sys() interface{}   { return nil }

// Create a new root FileInfo
var rootFileInfo = &winRootFileInfo{
	name:    "/",
	modTime: time.Now(),
}

func (s *Server) lstat(name string) (os.FileInfo, error) {
	if name == `\\.\` && s.winRoot {
		return rootFileInfo, nil
	}
	return os.Lstat(name)
}

func (s *Server) stat(name string) (os.FileInfo, error) {
	if name == `\\.\` && s.winRoot {
		return rootFileInfo, nil
	}
	return os.Stat(name)
}
