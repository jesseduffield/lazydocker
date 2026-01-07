package graphdriver

import (
	"io"
	"os"
	"runtime"
	"time"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/chrootarchive"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/unshare"
)

// ApplyUncompressedLayer defines the unpack method used by the graph
// driver.
var ApplyUncompressedLayer = chrootarchive.ApplyUncompressedLayer

// NaiveDiffDriver takes a ProtoDriver and adds the
// capability of the Diffing methods which it may or may not
// support on its own. See the comment on the exported
// NewNaiveDiffDriver function below.
type NaiveDiffDriver struct {
	ProtoDriver
	LayerIDMapUpdater
}

// NewNaiveDiffDriver returns a fully functional driver that wraps the
// given ProtoDriver and adds the capability of the following methods which
// it may or may not support on its own:
//
//	Diff(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) (io.ReadCloser, error)
//	Changes(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) ([]archive.Change, error)
//	ApplyDiff(id, parent string, options ApplyDiffOpts) (size int64, err error)
//	DiffSize(id string, idMappings *idtools.IDMappings, parent, parentMappings *idtools.IDMappings, mountLabel string) (size int64, err error)
func NewNaiveDiffDriver(driver ProtoDriver, updater LayerIDMapUpdater) Driver {
	return &NaiveDiffDriver{ProtoDriver: driver, LayerIDMapUpdater: updater}
}

// Diff produces an archive of the changes between the specified
// layer and its parent layer which may be "".
func (gdw *NaiveDiffDriver) Diff(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) (arch io.ReadCloser, err error) {
	startTime := time.Now()
	driver := gdw.ProtoDriver

	if idMappings == nil {
		idMappings = &idtools.IDMappings{}
	}
	if parentMappings == nil {
		parentMappings = &idtools.IDMappings{}
	}

	options := MountOpts{
		MountLabel: mountLabel,
		Options:    []string{"ro"},
	}
	layerFs, err := driver.Get(id, options)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			driverPut(driver, id, &err)
		}
	}()

	if parent == "" {
		archive, err := archive.TarWithOptions(layerFs, &archive.TarOptions{
			Compression: archive.Uncompressed,
			UIDMaps:     idMappings.UIDs(),
			GIDMaps:     idMappings.GIDs(),
		})
		if err != nil {
			return nil, err
		}
		return ioutils.NewReadCloserWrapper(archive, func() error {
			err := archive.Close()
			driverPut(driver, id, &err)
			return err
		}), nil
	}

	options.Options = append(options.Options, "ro")
	parentFs, err := driver.Get(parent, options)
	if err != nil {
		return nil, err
	}
	defer driverPut(driver, parent, &err)

	changes, err := archive.ChangesDirs(layerFs, idMappings, parentFs, parentMappings)
	if err != nil {
		return nil, err
	}

	archive, err := archive.ExportChanges(layerFs, changes, idMappings.UIDs(), idMappings.GIDs())
	if err != nil {
		return nil, err
	}

	return ioutils.NewReadCloserWrapper(archive, func() error {
		err := archive.Close()
		driverPut(driver, id, &err)

		// NaiveDiffDriver compares file metadata with parent layers. Parent layers
		// are extracted from tar's with full second precision on modified time.
		// We need this hack here to make sure calls within same second receive
		// correct result.
		time.Sleep(time.Until(startTime.Truncate(time.Second).Add(time.Second)))
		return err
	}), nil
}

// Changes produces a list of changes between the specified layer
// and its parent layer. If parent is "", then all changes will be ADD changes.
func (gdw *NaiveDiffDriver) Changes(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) (_ []archive.Change, retErr error) {
	driver := gdw.ProtoDriver

	if idMappings == nil {
		idMappings = &idtools.IDMappings{}
	}
	if parentMappings == nil {
		parentMappings = &idtools.IDMappings{}
	}

	options := MountOpts{
		MountLabel: mountLabel,
		Options:    []string{"ro"},
	}
	layerFs, err := driver.Get(id, options)
	if err != nil {
		return nil, err
	}
	defer driverPut(driver, id, &retErr)

	parentFs := ""

	if parent != "" {
		parentFs, err = driver.Get(parent, options)
		if err != nil {
			return nil, err
		}
		defer driverPut(driver, parent, &retErr)
	}

	return archive.ChangesDirs(layerFs, idMappings, parentFs, parentMappings)
}

// ApplyDiff extracts the changeset from the given diff into the
// layer with the specified id and parent, returning the size of the
// new layer in bytes.
func (gdw *NaiveDiffDriver) ApplyDiff(id, parent string, options ApplyDiffOpts) (int64, error) {
	driver := gdw.ProtoDriver

	if options.Mappings == nil {
		options.Mappings = &idtools.IDMappings{}
	}

	// Mount the root filesystem so we can apply the diff/layer.
	mountOpts := MountOpts{
		MountLabel: options.MountLabel,
	}
	layerFs, err := driver.Get(id, mountOpts)
	if err != nil {
		return -1, err
	}
	defer driverPut(driver, id, &err)

	defaultForceMask := os.FileMode(0o700)
	var forceMask *os.FileMode // = nil
	if runtime.GOOS == "darwin" {
		forceMask = &defaultForceMask
	}

	tarOptions := &archive.TarOptions{
		InUserNS:          unshare.IsRootless(),
		IgnoreChownErrors: options.IgnoreChownErrors,
		ForceMask:         forceMask,
	}
	if options.Mappings != nil {
		tarOptions.UIDMaps = options.Mappings.UIDs()
		tarOptions.GIDMaps = options.Mappings.GIDs()
	}
	start := time.Now().UTC()
	logrus.Debug("Start untar layer")
	size, err := ApplyUncompressedLayer(layerFs, options.Diff, tarOptions)
	if err != nil {
		logrus.Errorf("While applying layer: %s", err)
		return -1, err
	}
	logrus.Debugf("Untar time: %vs", time.Now().UTC().Sub(start).Seconds())

	return size, nil
}

// DiffSize calculates the changes between the specified layer
// and its parent and returns the size in bytes of the changes
// relative to its base filesystem directory.
func (gdw *NaiveDiffDriver) DiffSize(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) (int64, error) {
	driver := gdw.ProtoDriver

	if idMappings == nil {
		idMappings = &idtools.IDMappings{}
	}
	if parentMappings == nil {
		parentMappings = &idtools.IDMappings{}
	}

	changes, err := gdw.Changes(id, idMappings, parent, parentMappings, mountLabel)
	if err != nil {
		return 0, err
	}

	options := MountOpts{
		MountLabel: mountLabel,
	}
	layerFs, err := driver.Get(id, options)
	if err != nil {
		return 0, err
	}
	defer driverPut(driver, id, &err)

	return archive.ChangesSize(layerFs, changes), nil
}
