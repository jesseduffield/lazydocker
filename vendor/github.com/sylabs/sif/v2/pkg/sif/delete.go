// Copyright (c) 2018-2024, Sylabs Inc. All rights reserved.
// Copyright (c) 2017, SingularityWare, LLC. All rights reserved.
// Copyright (c) 2017, Yannick Cote <yhcote@gmail.com> All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package sif

import (
	"fmt"
	"io"
	"time"
)

// zeroReader is an io.Reader that returns a stream of zero-bytes.
type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) {
	clear(b)
	return len(b), nil
}

// zero overwrites the data object described by d with a stream of zero bytes.
func (f *FileImage) zero(d *rawDescriptor) error {
	if _, err := f.rw.Seek(d.Offset, io.SeekStart); err != nil {
		return err
	}

	_, err := io.CopyN(f.rw, zeroReader{}, d.Size)
	return err
}

// deleteOpts accumulates object deletion options.
type deleteOpts struct {
	zero    bool
	compact bool
	t       time.Time
}

// DeleteOpt are used to specify object deletion options.
type DeleteOpt func(*deleteOpts) error

// OptDeleteZero specifies whether the deleted object should be zeroed.
func OptDeleteZero(b bool) DeleteOpt {
	return func(do *deleteOpts) error {
		do.zero = b
		return nil
	}
}

// OptDeleteCompact specifies whether the image should be compacted following object deletion.
func OptDeleteCompact(b bool) DeleteOpt {
	return func(do *deleteOpts) error {
		do.compact = b
		return nil
	}
}

// OptDeleteDeterministic sets header/descriptor fields to values that support deterministic
// modification of images.
func OptDeleteDeterministic() DeleteOpt {
	return func(do *deleteOpts) error {
		do.t = time.Time{}
		return nil
	}
}

// OptDeleteWithTime specifies t as the image modification time.
func OptDeleteWithTime(t time.Time) DeleteOpt {
	return func(do *deleteOpts) error {
		do.t = t
		return nil
	}
}

// DeleteObject deletes the data object with id, according to opts. If no matching descriptor is
// found, an error wrapping ErrObjectNotFound is returned.
//
// To zero the data region of the deleted object, use OptDeleteZero. To remove unused space at the
// end of the FileImage following object deletion, use OptDeleteCompact.
//
// By default, the image modification time is set to the current time for non-deterministic images,
// and unset otherwise. To override this, consider using OptDeleteDeterministic or
// OptDeleteWithTime.
func (f *FileImage) DeleteObject(id uint32, opts ...DeleteOpt) error {
	return f.DeleteObjects(WithID(id), opts...)
}

// DeleteObjects deletes the data objects selected by fn, according to opts. If no descriptors are
// selected by fns, an error wrapping ErrObjectNotFound is returned.
//
// To zero the data region of the deleted object, use OptDeleteZero. To remove unused space at the
// end of the FileImage following object deletion, use OptDeleteCompact.
//
// By default, the image modification time is set to the current time for non-deterministic images,
// and unset otherwise. To override this, consider using OptDeleteDeterministic or
// OptDeleteWithTime.
func (f *FileImage) DeleteObjects(fn DescriptorSelectorFunc, opts ...DeleteOpt) error {
	do := deleteOpts{}

	if !f.isDeterministic() {
		do.t = time.Now()
	}

	for _, opt := range opts {
		if err := opt(&do); err != nil {
			return fmt.Errorf("%w", err)
		}
	}

	var selected bool

	if err := f.withDescriptors(fn, func(d *rawDescriptor) error {
		selected = true

		if do.zero {
			if err := f.zero(d); err != nil {
				return fmt.Errorf("%w", err)
			}
		}

		f.h.DescriptorsFree++

		// If we remove the primary partition, set the global header Arch field to HdrArchUnknown
		// to indicate that the SIF file doesn't include a primary partition and no dependency
		// on any architecture exists.
		if d.isPartitionOfType(PartPrimSys) {
			f.h.Arch = hdrArchUnknown
		}

		// Reset rawDescripter with empty struct
		*d = rawDescriptor{}

		return nil
	}); err != nil {
		return fmt.Errorf("%w", err)
	}

	if !selected {
		return fmt.Errorf("%w", ErrObjectNotFound)
	}

	f.h.ModifiedAt = do.t.Unix()

	if do.compact {
		f.h.DataSize = f.calculatedDataSize()

		if err := f.rw.Truncate(f.h.DataOffset + f.h.DataSize); err != nil {
			return fmt.Errorf("%w", err)
		}
	}

	if err := f.writeDescriptors(); err != nil {
		return fmt.Errorf("%w", err)
	}

	if err := f.writeHeader(); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}
