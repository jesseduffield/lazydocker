// Copyright (c) 2018-2024, Sylabs Inc. All rights reserved.
// Copyright (c) 2017, SingularityWare, LLC. All rights reserved.
// Copyright (c) 2017, Yannick Cote <yhcote@gmail.com> All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package sif

import (
	"encoding"
	"errors"
	"fmt"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// setOpts accumulates object set options.
type setOpts struct {
	t time.Time
}

// SetOpt are used to specify object set options.
type SetOpt func(*setOpts) error

// OptSetDeterministic sets header/descriptor fields to values that support deterministic
// modification of images.
func OptSetDeterministic() SetOpt {
	return func(so *setOpts) error {
		so.t = time.Time{}
		return nil
	}
}

// OptSetWithTime specifies t as the image/object modification time.
func OptSetWithTime(t time.Time) SetOpt {
	return func(so *setOpts) error {
		so.t = t
		return nil
	}
}

var (
	errNotPartition = errors.New("data object not a partition")
	errNotSystem    = errors.New("data object not a system partition")
)

// SetPrimPart sets the specified system partition to be the primary one.
//
// By default, the image/object modification times are set to the current time for
// non-deterministic images, and unset otherwise. To override this, consider using
// OptSetDeterministic or OptSetWithTime.
func (f *FileImage) SetPrimPart(id uint32, opts ...SetOpt) error {
	so := setOpts{}

	if !f.isDeterministic() {
		so.t = time.Now()
	}

	for _, opt := range opts {
		if err := opt(&so); err != nil {
			return fmt.Errorf("%w", err)
		}
	}

	descr, err := f.getDescriptor(WithID(id))
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	if descr.DataType != DataPartition {
		return fmt.Errorf("%w", errNotPartition)
	}

	var p partition
	if err := descr.getExtra(binaryUnmarshaler{&p}); err != nil {
		return fmt.Errorf("%w", err)
	}

	// if already primary system partition, nothing to do
	if p.Parttype == PartPrimSys {
		return nil
	}

	if p.Parttype != PartSystem {
		return fmt.Errorf("%w", errNotSystem)
	}

	// If there is currently a primary system partition, update it.
	if d, err := f.getDescriptor(WithPartitionType(PartPrimSys)); err == nil {
		var p partition
		if err := d.getExtra(binaryUnmarshaler{&p}); err != nil {
			return fmt.Errorf("%w", err)
		}

		p.Parttype = PartSystem

		if err := d.setExtra(p); err != nil {
			return fmt.Errorf("%w", err)
		}

		d.ModifiedAt = so.t.Unix()
	} else if !errors.Is(err, ErrObjectNotFound) {
		return fmt.Errorf("%w", err)
	}

	// Update the descriptor of the new primary system partition.
	p.Parttype = PartPrimSys

	if err := descr.setExtra(p); err != nil {
		return fmt.Errorf("%w", err)
	}

	descr.ModifiedAt = so.t.Unix()

	if err := f.writeDescriptors(); err != nil {
		return fmt.Errorf("%w", err)
	}

	f.h.Arch = p.Arch
	f.h.ModifiedAt = so.t.Unix()

	if err := f.writeHeader(); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

// SetMetadata sets the metadata of the data object with id to md, according to opts.
//
// By default, the image/object modification times are set to the current time for
// non-deterministic images, and unset otherwise. To override this, consider using
// OptSetDeterministic or OptSetWithTime.
func (f *FileImage) SetMetadata(id uint32, md encoding.BinaryMarshaler, opts ...SetOpt) error {
	so := setOpts{}

	if !f.isDeterministic() {
		so.t = time.Now()
	}

	for _, opt := range opts {
		if err := opt(&so); err != nil {
			return fmt.Errorf("%w", err)
		}
	}

	rd, err := f.getDescriptor(WithID(id))
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	if err := rd.setExtra(md); err != nil {
		return fmt.Errorf("%w", err)
	}

	rd.ModifiedAt = so.t.Unix()

	if err := f.writeDescriptors(); err != nil {
		return fmt.Errorf("%w", err)
	}

	f.h.ModifiedAt = so.t.Unix()

	if err := f.writeHeader(); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

// SetOCIBlobDigest updates the digest of the OCI blob object with id to h, according to opts.
//
// By default, the image/object modification times are set to the current time for
// non-deterministic images, and unset otherwise. To override this, consider using
// OptSetDeterministic or OptSetWithTime.
func (f *FileImage) SetOCIBlobDigest(id uint32, h v1.Hash, opts ...SetOpt) error {
	rd, err := f.getDescriptor(WithID(id))
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	if got := rd.DataType; got != DataOCIRootIndex && got != DataOCIBlob {
		return &unexpectedDataTypeError{got, []DataType{DataOCIRootIndex, DataOCIBlob}}
	}

	so := setOpts{}

	if !f.isDeterministic() {
		so.t = time.Now()
	}

	for _, opt := range opts {
		if err := opt(&so); err != nil {
			return fmt.Errorf("%w", err)
		}
	}

	md := &ociBlob{
		digest: h,
	}
	if err := rd.setExtra(md); err != nil {
		return fmt.Errorf("%w", err)
	}

	rd.ModifiedAt = so.t.Unix()

	if err := f.writeDescriptors(); err != nil {
		return fmt.Errorf("%w", err)
	}

	f.h.ModifiedAt = so.t.Unix()

	if err := f.writeHeader(); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}
