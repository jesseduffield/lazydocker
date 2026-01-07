package openssh

import (
	sshfx "github.com/pkg/sftp/internal/encoding/ssh/filexfer"
)

const extensionStatVFS = "statvfs@openssh.com"

// RegisterExtensionStatVFS registers the "statvfs@openssh.com" extended packet with the encoding/ssh/filexfer package.
func RegisterExtensionStatVFS() {
	sshfx.RegisterExtendedPacketType(extensionStatVFS, func() sshfx.ExtendedData {
		return new(StatVFSExtendedPacket)
	})
}

// ExtensionStatVFS returns an ExtensionPair suitable to append into an sshfx.InitPacket or sshfx.VersionPacket.
func ExtensionStatVFS() *sshfx.ExtensionPair {
	return &sshfx.ExtensionPair{
		Name: extensionStatVFS,
		Data: "2",
	}
}

// StatVFSExtendedPacket defines the statvfs@openssh.com extend packet.
type StatVFSExtendedPacket struct {
	Path string
}

// Type returns the SSH_FXP_EXTENDED packet type.
func (ep *StatVFSExtendedPacket) Type() sshfx.PacketType {
	return sshfx.PacketTypeExtended
}

// MarshalPacket returns ep as a two-part binary encoding of the full extended packet.
func (ep *StatVFSExtendedPacket) MarshalPacket(reqid uint32, b []byte) (header, payload []byte, err error) {
	p := &sshfx.ExtendedPacket{
		ExtendedRequest: extensionStatVFS,

		Data: ep,
	}
	return p.MarshalPacket(reqid, b)
}

// MarshalInto encodes ep into the binary encoding of the statvfs@openssh.com extended packet-specific data.
func (ep *StatVFSExtendedPacket) MarshalInto(buf *sshfx.Buffer) {
	buf.AppendString(ep.Path)
}

// MarshalBinary encodes ep into the binary encoding of the statvfs@openssh.com extended packet-specific data.
//
// NOTE: This _only_ encodes the packet-specific data, it does not encode the full extended packet.
func (ep *StatVFSExtendedPacket) MarshalBinary() ([]byte, error) {
	size := 4 + len(ep.Path) // string(path)

	buf := sshfx.NewBuffer(make([]byte, 0, size))

	ep.MarshalInto(buf)

	return buf.Bytes(), nil
}

// UnmarshalFrom decodes the statvfs@openssh.com extended packet-specific data into ep.
func (ep *StatVFSExtendedPacket) UnmarshalFrom(buf *sshfx.Buffer) (err error) {
	*ep = StatVFSExtendedPacket{
		Path: buf.ConsumeString(),
	}

	return buf.Err
}

// UnmarshalBinary decodes the statvfs@openssh.com extended packet-specific data into ep.
func (ep *StatVFSExtendedPacket) UnmarshalBinary(data []byte) (err error) {
	return ep.UnmarshalFrom(sshfx.NewBuffer(data))
}

const extensionFStatVFS = "fstatvfs@openssh.com"

// RegisterExtensionFStatVFS registers the "fstatvfs@openssh.com" extended packet with the encoding/ssh/filexfer package.
func RegisterExtensionFStatVFS() {
	sshfx.RegisterExtendedPacketType(extensionFStatVFS, func() sshfx.ExtendedData {
		return new(FStatVFSExtendedPacket)
	})
}

// ExtensionFStatVFS returns an ExtensionPair suitable to append into an sshfx.InitPacket or sshfx.VersionPacket.
func ExtensionFStatVFS() *sshfx.ExtensionPair {
	return &sshfx.ExtensionPair{
		Name: extensionFStatVFS,
		Data: "2",
	}
}

// FStatVFSExtendedPacket defines the fstatvfs@openssh.com extend packet.
type FStatVFSExtendedPacket struct {
	Path string
}

// Type returns the SSH_FXP_EXTENDED packet type.
func (ep *FStatVFSExtendedPacket) Type() sshfx.PacketType {
	return sshfx.PacketTypeExtended
}

// MarshalPacket returns ep as a two-part binary encoding of the full extended packet.
func (ep *FStatVFSExtendedPacket) MarshalPacket(reqid uint32, b []byte) (header, payload []byte, err error) {
	p := &sshfx.ExtendedPacket{
		ExtendedRequest: extensionFStatVFS,

		Data: ep,
	}
	return p.MarshalPacket(reqid, b)
}

// MarshalInto encodes ep into the binary encoding of the statvfs@openssh.com extended packet-specific data.
func (ep *FStatVFSExtendedPacket) MarshalInto(buf *sshfx.Buffer) {
	buf.AppendString(ep.Path)
}

// MarshalBinary encodes ep into the binary encoding of the statvfs@openssh.com extended packet-specific data.
//
// NOTE: This _only_ encodes the packet-specific data, it does not encode the full extended packet.
func (ep *FStatVFSExtendedPacket) MarshalBinary() ([]byte, error) {
	size := 4 + len(ep.Path) // string(path)

	buf := sshfx.NewBuffer(make([]byte, 0, size))

	ep.MarshalInto(buf)

	return buf.Bytes(), nil
}

// UnmarshalFrom decodes the statvfs@openssh.com extended packet-specific data into ep.
func (ep *FStatVFSExtendedPacket) UnmarshalFrom(buf *sshfx.Buffer) (err error) {
	*ep = FStatVFSExtendedPacket{
		Path: buf.ConsumeString(),
	}

	return buf.Err
}

// UnmarshalBinary decodes the statvfs@openssh.com extended packet-specific data into ep.
func (ep *FStatVFSExtendedPacket) UnmarshalBinary(data []byte) (err error) {
	return ep.UnmarshalFrom(sshfx.NewBuffer(data))
}

// The values for the MountFlags field.
// https://github.com/openssh/openssh-portable/blob/master/PROTOCOL
const (
	MountFlagsReadOnly = 0x1 // SSH_FXE_STATVFS_ST_RDONLY
	MountFlagsNoSUID   = 0x2 // SSH_FXE_STATVFS_ST_NOSUID
)

// StatVFSExtendedReplyPacket defines the extended reply packet for statvfs@openssh.com and fstatvfs@openssh.com requests.
type StatVFSExtendedReplyPacket struct {
	BlockSize     uint64 /* f_bsize:   file system block size */
	FragmentSize  uint64 /* f_frsize:  fundamental fs block size / fagment size */
	Blocks        uint64 /* f_blocks:  number of blocks (unit f_frsize) */
	BlocksFree    uint64 /* f_bfree:   free blocks in filesystem */
	BlocksAvail   uint64 /* f_bavail:  free blocks for non-root */
	Files         uint64 /* f_files:   total file inodes */
	FilesFree     uint64 /* f_ffree:   free file inodes */
	FilesAvail    uint64 /* f_favail:  free file inodes for to non-root */
	FilesystemID  uint64 /* f_fsid:    file system id */
	MountFlags    uint64 /* f_flag:    bit mask of mount flag values */
	MaxNameLength uint64 /* f_namemax: maximum filename length */
}

// Type returns the SSH_FXP_EXTENDED_REPLY packet type.
func (ep *StatVFSExtendedReplyPacket) Type() sshfx.PacketType {
	return sshfx.PacketTypeExtendedReply
}

// MarshalPacket returns ep as a two-part binary encoding of the full extended reply packet.
func (ep *StatVFSExtendedReplyPacket) MarshalPacket(reqid uint32, b []byte) (header, payload []byte, err error) {
	p := &sshfx.ExtendedReplyPacket{
		Data: ep,
	}
	return p.MarshalPacket(reqid, b)
}

// UnmarshalPacketBody returns ep as a two-part binary encoding of the full extended reply packet.
func (ep *StatVFSExtendedReplyPacket) UnmarshalPacketBody(buf *sshfx.Buffer) (err error) {
	p := &sshfx.ExtendedReplyPacket{
		Data: ep,
	}
	return p.UnmarshalPacketBody(buf)
}

// MarshalInto encodes ep into the binary encoding of the (f)statvfs@openssh.com extended reply packet-specific data.
func (ep *StatVFSExtendedReplyPacket) MarshalInto(buf *sshfx.Buffer) {
	buf.AppendUint64(ep.BlockSize)
	buf.AppendUint64(ep.FragmentSize)
	buf.AppendUint64(ep.Blocks)
	buf.AppendUint64(ep.BlocksFree)
	buf.AppendUint64(ep.BlocksAvail)
	buf.AppendUint64(ep.Files)
	buf.AppendUint64(ep.FilesFree)
	buf.AppendUint64(ep.FilesAvail)
	buf.AppendUint64(ep.FilesystemID)
	buf.AppendUint64(ep.MountFlags)
	buf.AppendUint64(ep.MaxNameLength)
}

// MarshalBinary encodes ep into the binary encoding of the (f)statvfs@openssh.com extended reply packet-specific data.
//
// NOTE: This _only_ encodes the packet-specific data, it does not encode the full extended reply packet.
func (ep *StatVFSExtendedReplyPacket) MarshalBinary() ([]byte, error) {
	size := 11 * 8 // 11 × uint64(various)

	b := sshfx.NewBuffer(make([]byte, 0, size))
	ep.MarshalInto(b)
	return b.Bytes(), nil
}

// UnmarshalFrom decodes the fstatvfs@openssh.com extended reply packet-specific data into ep.
func (ep *StatVFSExtendedReplyPacket) UnmarshalFrom(buf *sshfx.Buffer) (err error) {
	*ep = StatVFSExtendedReplyPacket{
		BlockSize:     buf.ConsumeUint64(),
		FragmentSize:  buf.ConsumeUint64(),
		Blocks:        buf.ConsumeUint64(),
		BlocksFree:    buf.ConsumeUint64(),
		BlocksAvail:   buf.ConsumeUint64(),
		Files:         buf.ConsumeUint64(),
		FilesFree:     buf.ConsumeUint64(),
		FilesAvail:    buf.ConsumeUint64(),
		FilesystemID:  buf.ConsumeUint64(),
		MountFlags:    buf.ConsumeUint64(),
		MaxNameLength: buf.ConsumeUint64(),
	}

	return buf.Err
}

// UnmarshalBinary decodes the fstatvfs@openssh.com extended reply packet-specific data into ep.
func (ep *StatVFSExtendedReplyPacket) UnmarshalBinary(data []byte) (err error) {
	return ep.UnmarshalFrom(sshfx.NewBuffer(data))
}
