// Copyright (c) 2018-2023, Sylabs Inc. All rights reserved.
// Copyright (c) 2017, SingularityWare, LLC. All rights reserved.
// Copyright (c) 2017, Yannick Cote <yhcote@gmail.com> All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package sif

import (
	"fmt"
	"time"
)

// addOpts accumulates object add options.
type addOpts struct {
	t time.Time
}

// AddOpt are used to specify object add options.
type AddOpt func(*addOpts) error

// OptAddDeterministic sets header/descriptor fields to values that support deterministic
// modification of images.
func OptAddDeterministic() AddOpt {
	return func(ao *addOpts) error {
		ao.t = time.Time{}
		return nil
	}
}

// OptAddWithTime specifies t as the image modification time.
func OptAddWithTime(t time.Time) AddOpt {
	return func(ao *addOpts) error {
		ao.t = t
		return nil
	}
}

// AddObject adds a new data object and its descriptor into the specified SIF file.
//
// By default, the image modification time is set to the current time for non-deterministic images,
// and unset otherwise. To override this, consider using OptAddDeterministic or OptAddWithTime.
func (f *FileImage) AddObject(di DescriptorInput, opts ...AddOpt) error {
	ao := addOpts{}

	if !f.isDeterministic() {
		ao.t = time.Now()
	}

	for _, opt := range opts {
		if err := opt(&ao); err != nil {
			return fmt.Errorf("%w", err)
		}
	}

	// Find an unused descriptor.
	i := 0
	for _, rd := range f.rds {
		if !rd.Used {
			break
		}
		i++
	}

	if err := f.writeDataObject(i, di, ao.t); err != nil {
		return fmt.Errorf("%w", err)
	}

	if err := f.writeDescriptors(); err != nil {
		return fmt.Errorf("%w", err)
	}

	f.h.ModifiedAt = ao.t.Unix()

	if err := f.writeHeader(); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}
