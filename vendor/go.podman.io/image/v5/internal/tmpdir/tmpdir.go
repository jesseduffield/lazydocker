package tmpdir

import (
	"os"
	"runtime"

	"go.podman.io/image/v5/types"
)

// unixTempDirForBigFiles is the directory path to store big files on non Windows systems.
// You can override this at build time with
// -ldflags '-X go.podman.io/image/v5/internal/tmpdir.unixTempDirForBigFiles=$your_path'
var unixTempDirForBigFiles = builtinUnixTempDirForBigFiles

// builtinUnixTempDirForBigFiles is the directory path to store big files.
// Do not use the system default of os.TempDir(), usually /tmp, because with systemd it could be a tmpfs.
// DO NOT change this, instead see unixTempDirForBigFiles above.
const builtinUnixTempDirForBigFiles = "/var/tmp"

const prefix = "container_images_"

// TemporaryDirectoryForBigFiles returns a directory for temporary (big) files.
// On non Windows systems it avoids the use of os.TempDir(), because the default temporary directory usually falls under /tmp
// which on systemd based systems could be the unsuitable tmpfs filesystem.
func temporaryDirectoryForBigFiles(sys *types.SystemContext) string {
	if sys != nil && sys.BigFilesTemporaryDir != "" {
		return sys.BigFilesTemporaryDir
	}
	var temporaryDirectoryForBigFiles string
	if runtime.GOOS == "windows" {
		temporaryDirectoryForBigFiles = os.TempDir()
	} else {
		temporaryDirectoryForBigFiles = unixTempDirForBigFiles
	}
	return temporaryDirectoryForBigFiles
}

func CreateBigFileTemp(sys *types.SystemContext, name string) (*os.File, error) {
	return os.CreateTemp(temporaryDirectoryForBigFiles(sys), prefix+name)
}

func MkDirBigFileTemp(sys *types.SystemContext, name string) (string, error) {
	return os.MkdirTemp(temporaryDirectoryForBigFiles(sys), prefix+name)
}
