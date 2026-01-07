// Copyright (c) 2018-2021, Sylabs Inc. All rights reserved.
// Copyright (c) 2017, SingularityWare, LLC. All rights reserved.
// Copyright (c) 2017, Yannick Cote <yhcote@gmail.com> All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package sif

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	errInvalidMagic        = errors.New("invalid SIF magic")
	errIncompatibleVersion = errors.New("incompatible SIF version")
)

// isValidSif looks at key fields from the global header to assess SIF validity.
func isValidSif(f *FileImage) error {
	if f.h.Magic != hdrMagic {
		return errInvalidMagic
	}

	if f.h.Version != CurrentVersion.bytes() {
		return errIncompatibleVersion
	}

	return nil
}

// populateMinIDs populates the minIDs field of f.
func (f *FileImage) populateMinIDs() {
	f.minIDs = make(map[uint32]uint32)
	f.WithDescriptors(func(d Descriptor) bool {
		if minID, ok := f.minIDs[d.raw.GroupID]; !ok || d.ID() < minID {
			f.minIDs[d.raw.GroupID] = d.ID()
		}
		return false
	})
}

// loadContainer loads a SIF image from rw.
func loadContainer(rw ReadWriter) (*FileImage, error) {
	f := FileImage{rw: rw}

	// Read global header.
	err := binary.Read(
		io.NewSectionReader(rw, 0, int64(binary.Size(f.h))),
		binary.LittleEndian,
		&f.h,
	)
	if err != nil {
		return nil, fmt.Errorf("reading global header: %w", err)
	}

	if err := isValidSif(&f); err != nil {
		return nil, err
	}

	// Read descriptors.
	f.rds = make([]rawDescriptor, f.h.DescriptorsTotal)
	err = binary.Read(
		io.NewSectionReader(rw, f.h.DescriptorsOffset, f.h.DescriptorsSize),
		binary.LittleEndian,
		&f.rds,
	)
	if err != nil {
		return nil, fmt.Errorf("reading descriptors: %w", err)
	}

	f.populateMinIDs()

	return &f, nil
}

// loadOpts accumulates container loading options.
type loadOpts struct {
	flag          int
	closeOnUnload bool
}

// LoadOpt are used to specify container loading options.
type LoadOpt func(*loadOpts) error

// OptLoadWithFlag specifies flag (os.O_RDONLY etc.) to be used when opening the container file.
func OptLoadWithFlag(flag int) LoadOpt {
	return func(lo *loadOpts) error {
		lo.flag = flag
		return nil
	}
}

// OptLoadWithCloseOnUnload specifies whether the ReadWriter should be closed by UnloadContainer.
// By default, the ReadWriter will be closed if it implements the io.Closer interface.
func OptLoadWithCloseOnUnload(b bool) LoadOpt {
	return func(lo *loadOpts) error {
		lo.closeOnUnload = b
		return nil
	}
}

// LoadContainerFromPath loads a new SIF container from path, according to opts.
//
// On success, a FileImage is returned. The caller must call UnloadContainer to ensure resources
// are released.
//
// By default, the file is opened for read and write access. To change this behavior, consider
// using OptLoadWithFlag.
func LoadContainerFromPath(path string, opts ...LoadOpt) (*FileImage, error) {
	lo := loadOpts{
		flag: os.O_RDWR,
	}

	for _, opt := range opts {
		if err := opt(&lo); err != nil {
			return nil, fmt.Errorf("%w", err)
		}
	}

	fp, err := os.OpenFile(path, lo.flag, 0)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	f, err := loadContainer(fp)
	if err != nil {
		fp.Close()

		return nil, fmt.Errorf("%w", err)
	}

	f.closeOnUnload = true
	return f, nil
}

// LoadContainer loads a new SIF container from rw, according to opts.
//
// On success, a FileImage is returned. The caller must call UnloadContainer to ensure resources
// are released. By default, UnloadContainer will close rw if it implements the io.Closer
// interface. To change this behavior, consider using OptLoadWithCloseOnUnload.
func LoadContainer(rw ReadWriter, opts ...LoadOpt) (*FileImage, error) {
	lo := loadOpts{
		closeOnUnload: true,
	}

	for _, opt := range opts {
		if err := opt(&lo); err != nil {
			return nil, fmt.Errorf("%w", err)
		}
	}

	f, err := loadContainer(rw)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	f.closeOnUnload = lo.closeOnUnload
	return f, nil
}

// UnloadContainer unloads f, releasing associated resources.
func (f *FileImage) UnloadContainer() error {
	if c, ok := f.rw.(io.Closer); ok && f.closeOnUnload {
		if err := c.Close(); err != nil {
			return fmt.Errorf("%w", err)
		}
	}
	return nil
}
