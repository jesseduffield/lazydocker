// Copyright (c) 2021-2024, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package sif

import (
	"errors"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// ErrNoObjects is the error returned when an image contains no data objects.
var ErrNoObjects = errors.New("no objects in image")

// ErrObjectNotFound is the error returned when a data object is not found.
var ErrObjectNotFound = errors.New("object not found")

// ErrMultipleObjectsFound is the error returned when multiple data objects are found.
var ErrMultipleObjectsFound = errors.New("multiple objects found")

// ErrInvalidObjectID is the error returned when an invalid object ID is supplied.
var ErrInvalidObjectID = errors.New("invalid object ID")

// ErrInvalidGroupID is the error returned when an invalid group ID is supplied.
var ErrInvalidGroupID = errors.New("invalid group ID")

// DescriptorSelectorFunc returns true if d matches, and false otherwise.
type DescriptorSelectorFunc func(d Descriptor) (bool, error)

// WithDataType selects descriptors that have data type dt.
func WithDataType(dt DataType) DescriptorSelectorFunc {
	return func(d Descriptor) (bool, error) {
		return d.DataType() == dt, nil
	}
}

// WithID selects descriptors with a matching ID.
func WithID(id uint32) DescriptorSelectorFunc {
	return func(d Descriptor) (bool, error) {
		if id == 0 {
			return false, ErrInvalidObjectID
		}
		return d.ID() == id, nil
	}
}

// WithNoGroup selects descriptors that are not contained within an object group.
func WithNoGroup() DescriptorSelectorFunc {
	return func(d Descriptor) (bool, error) {
		return d.GroupID() == 0, nil
	}
}

// WithGroupID returns a selector func that selects descriptors with a matching groupID.
func WithGroupID(groupID uint32) DescriptorSelectorFunc {
	return func(d Descriptor) (bool, error) {
		if groupID == 0 {
			return false, ErrInvalidGroupID
		}
		return d.GroupID() == groupID, nil
	}
}

// WithLinkedID selects descriptors that are linked to the data object with specified ID.
func WithLinkedID(id uint32) DescriptorSelectorFunc {
	return func(d Descriptor) (bool, error) {
		if id == 0 {
			return false, ErrInvalidObjectID
		}
		linkedID, isGroup := d.LinkedID()
		return !isGroup && linkedID == id, nil
	}
}

// WithLinkedGroupID selects descriptors that are linked to the data object group with specified
// ID.
func WithLinkedGroupID(groupID uint32) DescriptorSelectorFunc {
	return func(d Descriptor) (bool, error) {
		if groupID == 0 {
			return false, ErrInvalidGroupID
		}
		linkedID, isGroup := d.LinkedID()
		return isGroup && linkedID == groupID, nil
	}
}

// WithPartitionType selects descriptors containing a partition of type pt.
func WithPartitionType(pt PartType) DescriptorSelectorFunc {
	return func(d Descriptor) (bool, error) {
		return d.raw.isPartitionOfType(pt), nil
	}
}

// WithOCIBlobDigest selects descriptors that contain a OCI blob with the specified digest.
func WithOCIBlobDigest(digest v1.Hash) DescriptorSelectorFunc {
	return func(d Descriptor) (bool, error) {
		if h, err := d.OCIBlobDigest(); err == nil {
			return h.String() == digest.String(), nil
		}
		return false, nil
	}
}

// descriptorFromRaw populates a Descriptor from rd.
func (f *FileImage) descriptorFromRaw(rd *rawDescriptor) Descriptor {
	return Descriptor{
		raw:        *rd,
		r:          f.rw,
		relativeID: rd.ID - f.minIDs[rd.GroupID],
	}
}

// GetDescriptors returns a slice of in-use descriptors for which all selector funcs return true.
// If the image contains no data objects, an error wrapping ErrNoObjects is returned.
func (f *FileImage) GetDescriptors(fns ...DescriptorSelectorFunc) ([]Descriptor, error) {
	if f.DescriptorsFree() == f.DescriptorsTotal() {
		return nil, fmt.Errorf("%w", ErrNoObjects)
	}

	var ds []Descriptor

	err := f.withDescriptors(multiSelectorFunc(fns...), func(d *rawDescriptor) error {
		ds = append(ds, f.descriptorFromRaw(d))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	return ds, nil
}

// getDescriptor returns a pointer to the in-use descriptor selected by fns. If no descriptor is
// selected by fns, ErrObjectNotFound is returned. If multiple descriptors are selected by fns,
// ErrMultipleObjectsFound is returned.
func (f *FileImage) getDescriptor(fns ...DescriptorSelectorFunc) (*rawDescriptor, error) {
	var d *rawDescriptor

	err := f.withDescriptors(multiSelectorFunc(fns...), func(found *rawDescriptor) error {
		if d != nil {
			return ErrMultipleObjectsFound
		}
		d = found
		return nil
	})

	if err == nil && d == nil {
		err = ErrObjectNotFound
	}

	return d, err
}

// GetDescriptor returns the in-use descriptor selected by fns. If the image contains no data
// objects, an error wrapping ErrNoObjects is returned. If no descriptor is selected by fns, an
// error wrapping ErrObjectNotFound is returned. If multiple descriptors are selected by fns, an
// error wrapping ErrMultipleObjectsFound is returned.
func (f *FileImage) GetDescriptor(fns ...DescriptorSelectorFunc) (Descriptor, error) {
	if f.DescriptorsFree() == f.DescriptorsTotal() {
		return Descriptor{}, fmt.Errorf("%w", ErrNoObjects)
	}

	d, err := f.getDescriptor(fns...)
	if err != nil {
		return Descriptor{}, fmt.Errorf("%w", err)
	}

	return f.descriptorFromRaw(d), nil
}

// multiSelectorFunc returns a DescriptorSelectorFunc that selects a descriptor iff all of fns
// select the descriptor.
func multiSelectorFunc(fns ...DescriptorSelectorFunc) DescriptorSelectorFunc {
	return func(d Descriptor) (bool, error) {
		for _, fn := range fns {
			if ok, err := fn(d); !ok || err != nil {
				return ok, err
			}
		}
		return true, nil
	}
}

var errNilSelectFunc = errors.New("descriptor selector func must not be nil")

// withDescriptors calls onMatchFn with each in-use descriptor in f for which selectFn returns
// true. If selectFn or onMatchFn return a non-nil error, the iteration halts, and the error is
// returned to the caller.
func (f *FileImage) withDescriptors(selectFn DescriptorSelectorFunc, onMatchFn func(*rawDescriptor) error) error {
	if selectFn == nil {
		return errNilSelectFunc
	}

	for i, d := range f.rds {
		if !d.Used {
			continue
		}

		if ok, err := selectFn(f.descriptorFromRaw(&f.rds[i])); err != nil {
			return err
		} else if !ok {
			continue
		}

		if err := onMatchFn(&f.rds[i]); err != nil {
			return err
		}
	}

	return nil
}

var errAbort = errors.New("abort")

// abortOnMatch is a semantic convenience function that always returns a non-nil error, which can
// be used as a no-op matchFn.
func abortOnMatch(*rawDescriptor) error { return errAbort }

// WithDescriptors calls fn with each in-use descriptor in f, until fn returns true.
func (f *FileImage) WithDescriptors(fn func(d Descriptor) bool) {
	selectFn := func(d Descriptor) (bool, error) {
		return fn(d), nil
	}
	_ = f.withDescriptors(selectFn, abortOnMatch)
}
