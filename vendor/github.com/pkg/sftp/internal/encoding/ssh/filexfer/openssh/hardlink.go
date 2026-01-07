package openssh

import (
	sshfx "github.com/pkg/sftp/internal/encoding/ssh/filexfer"
)

const extensionHardlink = "hardlink@openssh.com"

// RegisterExtensionHardlink registers the "hardlink@openssh.com" extended packet with the encoding/ssh/filexfer package.
func RegisterExtensionHardlink() {
	sshfx.RegisterExtendedPacketType(extensionHardlink, func() sshfx.ExtendedData {
		return new(HardlinkExtendedPacket)
	})
}

// ExtensionHardlink returns an ExtensionPair suitable to append into an sshfx.InitPacket or sshfx.VersionPacket.
func ExtensionHardlink() *sshfx.ExtensionPair {
	return &sshfx.ExtensionPair{
		Name: extensionHardlink,
		Data: "1",
	}
}

// HardlinkExtendedPacket defines the hardlink@openssh.com extend packet.
type HardlinkExtendedPacket struct {
	OldPath string
	NewPath string
}

// Type returns the SSH_FXP_EXTENDED packet type.
func (ep *HardlinkExtendedPacket) Type() sshfx.PacketType {
	return sshfx.PacketTypeExtended
}

// MarshalPacket returns ep as a two-part binary encoding of the full extended packet.
func (ep *HardlinkExtendedPacket) MarshalPacket(reqid uint32, b []byte) (header, payload []byte, err error) {
	p := &sshfx.ExtendedPacket{
		ExtendedRequest: extensionHardlink,

		Data: ep,
	}
	return p.MarshalPacket(reqid, b)
}

// MarshalInto encodes ep into the binary encoding of the hardlink@openssh.com extended packet-specific data.
func (ep *HardlinkExtendedPacket) MarshalInto(buf *sshfx.Buffer) {
	buf.AppendString(ep.OldPath)
	buf.AppendString(ep.NewPath)
}

// MarshalBinary encodes ep into the binary encoding of the hardlink@openssh.com extended packet-specific data.
//
// NOTE: This _only_ encodes the packet-specific data, it does not encode the full extended packet.
func (ep *HardlinkExtendedPacket) MarshalBinary() ([]byte, error) {
	// string(oldpath) + string(newpath)
	size := 4 + len(ep.OldPath) + 4 + len(ep.NewPath)

	buf := sshfx.NewBuffer(make([]byte, 0, size))
	ep.MarshalInto(buf)
	return buf.Bytes(), nil
}

// UnmarshalFrom decodes the hardlink@openssh.com extended packet-specific data from buf.
func (ep *HardlinkExtendedPacket) UnmarshalFrom(buf *sshfx.Buffer) (err error) {
	*ep = HardlinkExtendedPacket{
		OldPath: buf.ConsumeString(),
		NewPath: buf.ConsumeString(),
	}

	return buf.Err
}

// UnmarshalBinary decodes the hardlink@openssh.com extended packet-specific data into ep.
func (ep *HardlinkExtendedPacket) UnmarshalBinary(data []byte) (err error) {
	return ep.UnmarshalFrom(sshfx.NewBuffer(data))
}
