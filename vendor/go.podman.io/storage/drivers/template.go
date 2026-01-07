package graphdriver

import (
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/idtools"
)

// TemplateDriver is just barely enough of a driver that we can implement a
// naive version of CreateFromTemplate on top of it.
type TemplateDriver interface {
	DiffDriver
	CreateReadWrite(id, parent string, opts *CreateOpts) error
	Create(id, parent string, opts *CreateOpts) error
	Remove(id string) error
}

// CreateFromTemplate creates a layer with the same contents and parent as
// another layer.  Internally, it may even depend on that other layer
// continuing to exist, as if it were actually a child of the child layer.
func NaiveCreateFromTemplate(d TemplateDriver, id, template string, templateIDMappings *idtools.IDMappings, parent string, parentIDMappings *idtools.IDMappings, opts *CreateOpts, readWrite bool) error {
	var err error
	if readWrite {
		err = d.CreateReadWrite(id, parent, opts)
	} else {
		err = d.Create(id, parent, opts)
	}
	if err != nil {
		return err
	}
	diff, err := d.Diff(template, templateIDMappings, parent, parentIDMappings, opts.MountLabel)
	if err != nil {
		if err2 := d.Remove(id); err2 != nil {
			logrus.Errorf("Removing layer %q: %v", id, err2)
		}
		return err
	}
	defer diff.Close()

	applyOptions := ApplyDiffOpts{
		Diff:              diff,
		Mappings:          templateIDMappings,
		MountLabel:        opts.MountLabel,
		IgnoreChownErrors: opts.ignoreChownErrors,
	}
	if _, err = d.ApplyDiff(id, parent, applyOptions); err != nil {
		if err2 := d.Remove(id); err2 != nil {
			logrus.Errorf("Removing layer %q: %v", id, err2)
		}
		return err
	}
	return nil
}
