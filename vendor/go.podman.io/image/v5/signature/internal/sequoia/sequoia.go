//go:build containers_image_sequoia

package sequoia

// #cgo CFLAGS: -I. -DGO_SEQUOIA_ENABLE_DLOPEN=1
// #include "gosequoia.h"
// #include <dlfcn.h>
// #include <limits.h>
// typedef void (*sequoia_logger_consumer_t) (enum SequoiaLogLevel level, char *message);
// extern void sequoia_logrus_logger (enum SequoiaLogLevel level, char *message);
import "C"

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/sirupsen/logrus"
)

// sequoiaLibraryDir is the path to the directory where libpodman_sequoia is installed,
// if it is not in the platform’s default library path.
// You can override this at build time with
// -ldflags '-X go.podman.io/image/v5/signature/sequoia.sequoiaLibraryDir=$your_path'
var sequoiaLibraryDir = ""

type SigningMechanism struct {
	mechanism *C.SequoiaMechanism
}

// NewMechanismFromDirectory initializes a mechanism using (user-managed) Sequoia state
// in dir, which can be "" to indicate the default (using $SEQUOIA_HOME or the default home directory location).
func NewMechanismFromDirectory(
	dir string,
) (*SigningMechanism, error) {
	var cerr *C.SequoiaError
	var cDir *C.char
	if dir != "" {
		cDir = C.CString(dir)
		defer C.free(unsafe.Pointer(cDir))
	}
	cMechanism := C.go_sequoia_mechanism_new_from_directory(cDir, &cerr)
	if cMechanism == nil {
		defer C.go_sequoia_error_free(cerr)
		return nil, errors.New(C.GoString(cerr.message))
	}
	return &SigningMechanism{
		mechanism: cMechanism,
	}, nil
}

func NewEphemeralMechanism() (*SigningMechanism, error) {
	var cerr *C.SequoiaError
	cMechanism := C.go_sequoia_mechanism_new_ephemeral(&cerr)
	if cMechanism == nil {
		defer C.go_sequoia_error_free(cerr)
		return nil, errors.New(C.GoString(cerr.message))
	}
	return &SigningMechanism{
		mechanism: cMechanism,
	}, nil
}

func (m *SigningMechanism) SignWithPassphrase(
	input []byte,
	keyIdentity string,
	passphrase string,
) ([]byte, error) {
	var cerr *C.SequoiaError
	var cPassphrase *C.char
	if passphrase == "" {
		cPassphrase = nil
	} else {
		cPassphrase = C.CString(passphrase)
		defer C.free(unsafe.Pointer(cPassphrase))
	}
	cKeyIdentity := C.CString(keyIdentity)
	defer C.free(unsafe.Pointer(cKeyIdentity))
	sig := C.go_sequoia_sign(
		m.mechanism,
		cKeyIdentity,
		cPassphrase,
		(*C.uchar)(unsafe.Pointer(unsafe.SliceData(input))),
		C.size_t(len(input)),
		&cerr,
	)
	if sig == nil {
		defer C.go_sequoia_error_free(cerr)
		return nil, errors.New(C.GoString(cerr.message))
	}
	defer C.go_sequoia_signature_free(sig)
	var size C.size_t
	cData := C.go_sequoia_signature_get_data(sig, &size)
	if size > C.size_t(C.INT_MAX) {
		return nil, errors.New("overflow") // Coverage: This should not reasonably happen, and we don’t want to generate gigabytes of input to test this.
	}
	return C.GoBytes(unsafe.Pointer(cData), C.int(size)), nil
}

func (m *SigningMechanism) Sign(
	input []byte,
	keyIdentity string,
) ([]byte, error) {
	return m.SignWithPassphrase(input, keyIdentity, "")
}

func (m *SigningMechanism) Verify(
	unverifiedSignature []byte,
) (contents []byte, keyIdentity string, err error) {
	var cerr *C.SequoiaError
	result := C.go_sequoia_verify(
		m.mechanism,
		(*C.uchar)(unsafe.Pointer(unsafe.SliceData(unverifiedSignature))),
		C.size_t(len(unverifiedSignature)),
		&cerr,
	)
	if result == nil {
		defer C.go_sequoia_error_free(cerr)
		return nil, "", errors.New(C.GoString(cerr.message))
	}
	defer C.go_sequoia_verification_result_free(result)
	var size C.size_t
	cContent := C.go_sequoia_verification_result_get_content(result, &size)
	if size > C.size_t(C.INT_MAX) {
		return nil, "", errors.New("overflow") // Coverage: This should not reasonably happen, and we don’t want to generate gigabytes of input to test this.
	}
	contents = C.GoBytes(unsafe.Pointer(cContent), C.int(size))
	cSigner := C.go_sequoia_verification_result_get_signer(result)
	keyIdentity = C.GoString(cSigner)
	return contents, keyIdentity, nil
}

func (m *SigningMechanism) ImportKeys(blob []byte) ([]string, error) {
	var cerr *C.SequoiaError
	result := C.go_sequoia_import_keys(
		m.mechanism,
		(*C.uchar)(unsafe.Pointer(unsafe.SliceData(blob))),
		C.size_t(len(blob)),
		&cerr,
	)
	if result == nil {
		defer C.go_sequoia_error_free(cerr)
		return nil, errors.New(C.GoString(cerr.message))
	}
	defer C.go_sequoia_import_result_free(result)

	keyIdentities := []string{}
	count := C.go_sequoia_import_result_get_count(result)
	for i := C.size_t(0); i < count; i++ {
		var cerr *C.SequoiaError
		cKeyIdentity := C.go_sequoia_import_result_get_content(result, i, &cerr)
		if cerr != nil {
			defer C.go_sequoia_error_free(cerr) // Coverage: this can fail only if i is out of range.
			return nil, errors.New(C.GoString(cerr.message))
		}
		keyIdentities = append(keyIdentities, C.GoString(cKeyIdentity))
	}

	return keyIdentities, nil
}

func (m *SigningMechanism) Close() error {
	C.go_sequoia_mechanism_free(m.mechanism)
	return nil
}

//export sequoia_logrus_logger
func sequoia_logrus_logger(level C.enum_SequoiaLogLevel, message *C.char) {
	var logrusLevel logrus.Level
	switch level { // Coverage: We are not in control of whether / how the Rust code chooses to log things.
	case C.SEQUOIA_LOG_LEVEL_ERROR:
		logrusLevel = logrus.ErrorLevel
	case C.SEQUOIA_LOG_LEVEL_WARN:
		logrusLevel = logrus.WarnLevel
	case C.SEQUOIA_LOG_LEVEL_INFO:
		logrusLevel = logrus.InfoLevel
	case C.SEQUOIA_LOG_LEVEL_DEBUG:
		logrusLevel = logrus.DebugLevel
	case C.SEQUOIA_LOG_LEVEL_TRACE:
		logrusLevel = logrus.TraceLevel
	case C.SEQUOIA_LOG_LEVEL_UNKNOWN:
		fallthrough
	default:
		logrusLevel = logrus.ErrorLevel // Should never happen
	}
	logrus.StandardLogger().Log(logrusLevel, C.GoString(message))
}

// initOnce should only be called by Init.
func initOnce() error {
	var soName string
	switch runtime.GOOS {
	case "linux":
		soName = "libpodman_sequoia.so.0"
	case "darwin":
		soName = "libpodman_sequoia.dylib"
	default:
		return fmt.Errorf("Unhandled OS %q in sequoia initialization", runtime.GOOS) // Coverage: This is ~by definition not reached in tests.
	}
	if sequoiaLibraryDir != "" {
		soName = filepath.Join(sequoiaLibraryDir, soName)
	}
	cSOName := C.CString(soName)
	defer C.free(unsafe.Pointer(cSOName))
	if C.go_sequoia_ensure_library(cSOName,
		C.RTLD_NOW|C.RTLD_GLOBAL) < 0 {
		return fmt.Errorf("unable to load %q", soName) // Coverage: This is impractical to test in-process, with the static go_sequoia_dlhandle.
	}

	var cerr *C.SequoiaError
	if C.go_sequoia_set_logger_consumer(C.sequoia_logger_consumer_t(C.sequoia_logrus_logger), &cerr) != 0 {
		defer C.go_sequoia_error_free(cerr) // Coverage: This is impractical to test in-process, with the static go_sequoia_dlhandle.
		return fmt.Errorf("initializing logging: %s", C.GoString(cerr.message))
	}
	return nil
}

// Init ensures the libpodman_sequoia library is available.
// It is safe to call from arbitrary goroutines.
var Init = sync.OnceValue(initOnce)
