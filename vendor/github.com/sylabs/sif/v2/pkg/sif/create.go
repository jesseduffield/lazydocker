// Copyright (c) 2018-2024, Sylabs Inc. All rights reserved.
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
	"math"
	"os"
	"time"

	"github.com/google/uuid"
)

var errAlignmentOverflow = errors.New("integer overflow when calculating alignment")

// nextAligned finds the next offset that satisfies alignment.
func nextAligned(offset int64, alignment int) (int64, error) {
	align64 := int64(alignment)

	if align64 <= 0 || offset%align64 == 0 {
		return offset, nil
	}

	align64 -= offset % align64

	if (math.MaxInt64 - offset) < align64 {
		return 0, errAlignmentOverflow
	}

	return offset + align64, nil
}

// writeDataObjectAt writes the data object described by di to ws, using time t, recording details
// in d. The object is written at the first position that satisfies the alignment requirements
// described by di following offsetUnaligned.
func writeDataObjectAt(ws io.WriteSeeker, offsetUnaligned int64, di DescriptorInput, t time.Time, d *rawDescriptor) error { //nolint:lll
	offset, err := nextAligned(offsetUnaligned, di.opts.alignment)
	if err != nil {
		return err
	}

	if _, err := ws.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	n, err := io.Copy(ws, di.r)
	if err != nil {
		return err
	}

	if err := di.fillDescriptor(t, d); err != nil {
		return err
	}
	d.Used = true
	d.Offset = offset
	d.Size = n
	d.SizeWithPadding = offset - offsetUnaligned + n

	return nil
}

// calculatedDataSize calculates the size of the data section based on the in-use descriptors.
func (f *FileImage) calculatedDataSize() int64 {
	dataEnd := f.DataOffset()

	f.WithDescriptors(func(d Descriptor) bool {
		if objectEnd := d.Offset() + d.Size(); dataEnd < objectEnd {
			dataEnd = objectEnd
		}
		return false
	})

	return dataEnd - f.DataOffset()
}

var (
	errInsufficientCapacity = errors.New("insufficient descriptor capacity to add data object(s) to image")
	errPrimaryPartition     = errors.New("image already contains a primary partition")
	errObjectIDOverflow     = errors.New("object ID would overflow")
)

// writeDataObject writes the data object described by di to f, using time t, recording details in
// the descriptor at index i.
func (f *FileImage) writeDataObject(i int, di DescriptorInput, t time.Time) error {
	if i >= len(f.rds) {
		return errInsufficientCapacity
	}

	// We derive the ID from i, so make sure the ID will not overflow.
	if int64(i) >= math.MaxUint32 {
		return errObjectIDOverflow
	}

	// If this is a primary partition, verify there isn't another primary partition, and update the
	// architecture in the global header.
	if p, ok := di.opts.md.(partition); ok && p.Parttype == PartPrimSys {
		if ds, err := f.GetDescriptors(WithPartitionType(PartPrimSys)); err == nil && len(ds) > 0 {
			return errPrimaryPartition
		}

		f.h.Arch = p.Arch
	}

	d := &f.rds[i]
	d.ID = uint32(i) + 1 //nolint:gosec // Overflow handled above.

	f.h.DataSize = f.calculatedDataSize()

	if err := writeDataObjectAt(f.rw, f.h.DataOffset+f.h.DataSize, di, t, d); err != nil {
		return err
	}

	// Update minimum object ID map.
	if minID, ok := f.minIDs[d.GroupID]; !ok || d.ID < minID {
		f.minIDs[d.GroupID] = d.ID
	}

	f.h.DescriptorsFree--
	f.h.DataSize += d.SizeWithPadding

	return nil
}

// writeDescriptors writes the descriptors in f to backing storage.
func (f *FileImage) writeDescriptors() error {
	if _, err := f.rw.Seek(f.h.DescriptorsOffset, io.SeekStart); err != nil {
		return err
	}

	return binary.Write(f.rw, binary.LittleEndian, f.rds)
}

// writeHeader writes the global header in f to backing storage.
func (f *FileImage) writeHeader() error {
	if _, err := f.rw.Seek(0, io.SeekStart); err != nil {
		return err
	}

	return binary.Write(f.rw, binary.LittleEndian, f.h)
}

// createOpts accumulates container creation options.
type createOpts struct {
	launchScript       [hdrLaunchLen]byte
	id                 uuid.UUID
	descriptorsOffset  int64
	descriptorCapacity int64
	dis                []DescriptorInput
	t                  time.Time
	closeOnUnload      bool
}

// CreateOpt are used to specify container creation options.
type CreateOpt func(*createOpts) error

var errLaunchScriptLen = errors.New("launch script too large")

// OptCreateWithLaunchScript specifies s as the launch script.
func OptCreateWithLaunchScript(s string) CreateOpt {
	return func(co *createOpts) error {
		b := []byte(s)

		if len(b) >= len(co.launchScript) {
			return errLaunchScriptLen
		}

		copy(co.launchScript[:], b)

		return nil
	}
}

// OptCreateDeterministic sets header/descriptor fields to values that support deterministic
// creation of images.
func OptCreateDeterministic() CreateOpt {
	return func(co *createOpts) error {
		co.id = uuid.Nil
		co.t = time.Time{}
		return nil
	}
}

// OptCreateWithID specifies id as the unique ID.
func OptCreateWithID(id string) CreateOpt {
	return func(co *createOpts) error {
		id, err := uuid.Parse(id)
		co.id = id
		return err
	}
}

// OptCreateWithDescriptorCapacity specifies that the created image should have the capacity for a
// maximum of n descriptors.
func OptCreateWithDescriptorCapacity(n int64) CreateOpt {
	return func(co *createOpts) error {
		co.descriptorCapacity = n
		return nil
	}
}

// OptCreateWithDescriptors appends dis to the list of descriptors.
func OptCreateWithDescriptors(dis ...DescriptorInput) CreateOpt {
	return func(co *createOpts) error {
		co.dis = append(co.dis, dis...)
		return nil
	}
}

// OptCreateWithTime specifies t as the image creation time.
func OptCreateWithTime(t time.Time) CreateOpt {
	return func(co *createOpts) error {
		co.t = t
		return nil
	}
}

// OptCreateWithCloseOnUnload specifies whether the ReadWriter should be closed by UnloadContainer.
// By default, the ReadWriter will be closed if it implements the io.Closer interface.
func OptCreateWithCloseOnUnload(b bool) CreateOpt {
	return func(co *createOpts) error {
		co.closeOnUnload = b
		return nil
	}
}

var errDescriptorCapacityNotSupported = errors.New("descriptor capacity not supported")

// createContainer creates a new SIF container file in rw, according to opts.
func createContainer(rw ReadWriter, co createOpts) (*FileImage, error) {
	// The supported number of descriptors is limited by the unsigned 32-bit ID field in each
	// rawDescriptor.
	if co.descriptorCapacity >= math.MaxUint32 {
		return nil, errDescriptorCapacityNotSupported
	}

	rds := make([]rawDescriptor, co.descriptorCapacity)
	rdsSize := int64(binary.Size(rds))

	h := header{
		LaunchScript:      co.launchScript,
		Magic:             hdrMagic,
		Version:           CurrentVersion.bytes(),
		Arch:              hdrArchUnknown,
		ID:                co.id,
		CreatedAt:         co.t.Unix(),
		ModifiedAt:        co.t.Unix(),
		DescriptorsFree:   co.descriptorCapacity,
		DescriptorsTotal:  co.descriptorCapacity,
		DescriptorsOffset: co.descriptorsOffset,
		DescriptorsSize:   rdsSize,
		DataOffset:        co.descriptorsOffset + rdsSize,
	}

	f := &FileImage{
		rw:     rw,
		h:      h,
		rds:    rds,
		minIDs: make(map[uint32]uint32),
	}

	for i, di := range co.dis {
		if err := f.writeDataObject(i, di, co.t); err != nil {
			return nil, err
		}
	}

	if err := f.writeDescriptors(); err != nil {
		return nil, err
	}

	if err := f.writeHeader(); err != nil {
		return nil, err
	}

	return f, nil
}

// CreateContainer creates a new SIF container in rw, according to opts. One or more data objects
// can optionally be specified using OptCreateWithDescriptors.
//
// On success, a FileImage is returned. The caller must call UnloadContainer to ensure resources
// are released. By default, UnloadContainer will close rw if it implements the io.Closer
// interface. To change this behavior, consider using OptCreateWithCloseOnUnload.
//
// By default, the image ID is set to a randomly generated value. To override this, consider using
// OptCreateDeterministic or OptCreateWithID.
//
// By default, the image creation time is set to the current time. To override this, consider using
// OptCreateDeterministic or OptCreateWithTime.
//
// By default, the image will support a maximum of 48 descriptors. To change this, consider using
// OptCreateWithDescriptorCapacity.
//
// A launch script can optionally be set using OptCreateWithLaunchScript.
func CreateContainer(rw ReadWriter, opts ...CreateOpt) (*FileImage, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}

	co := createOpts{
		id:                 id,
		descriptorsOffset:  4096,
		descriptorCapacity: 48,
		t:                  time.Now(),
		closeOnUnload:      true,
	}

	for _, opt := range opts {
		if err := opt(&co); err != nil {
			return nil, fmt.Errorf("%w", err)
		}
	}

	f, err := createContainer(rw, co)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	f.closeOnUnload = co.closeOnUnload
	return f, nil
}

// CreateContainerAtPath creates a new SIF container file at path, according to opts. One or more
// data objects can optionally be specified using OptCreateWithDescriptors.
//
// On success, a FileImage is returned. The caller must call UnloadContainer to ensure resources
// are released.
//
// By default, the image ID is set to a randomly generated value. To override this, consider using
// OptCreateDeterministic or OptCreateWithID.
//
// By default, the image creation time is set to the current time. To override this, consider using
// OptCreateDeterministic or OptCreateWithTime.
//
// By default, the image will support a maximum of 48 descriptors. To change this, consider using
// OptCreateWithDescriptorCapacity.
//
// A launch script can optionally be set using OptCreateWithLaunchScript.
func CreateContainerAtPath(path string, opts ...CreateOpt) (*FileImage, error) {
	fp, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	f, err := CreateContainer(fp, opts...)
	if err != nil {
		fp.Close()
		os.Remove(fp.Name())

		return nil, err
	}

	f.closeOnUnload = true
	return f, nil
}
