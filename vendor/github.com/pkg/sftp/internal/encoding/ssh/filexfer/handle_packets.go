package sshfx

// ClosePacket defines the SSH_FXP_CLOSE packet.
type ClosePacket struct {
	Handle string
}

// Type returns the SSH_FXP_xy value associated with this packet type.
func (p *ClosePacket) Type() PacketType {
	return PacketTypeClose
}

// MarshalPacket returns p as a two-part binary encoding of p.
func (p *ClosePacket) MarshalPacket(reqid uint32, b []byte) (header, payload []byte, err error) {
	buf := NewBuffer(b)
	if buf.Cap() < 9 {
		size := 4 + len(p.Handle) // string(handle)
		buf = NewMarshalBuffer(size)
	}

	buf.StartPacket(PacketTypeClose, reqid)
	buf.AppendString(p.Handle)

	return buf.Packet(payload)
}

// UnmarshalPacketBody unmarshals the packet body from the given Buffer.
// It is assumed that the uint32(request-id) has already been consumed.
func (p *ClosePacket) UnmarshalPacketBody(buf *Buffer) (err error) {
	*p = ClosePacket{
		Handle: buf.ConsumeString(),
	}

	return buf.Err
}

// ReadPacket defines the SSH_FXP_READ packet.
type ReadPacket struct {
	Handle string
	Offset uint64
	Length uint32
}

// Type returns the SSH_FXP_xy value associated with this packet type.
func (p *ReadPacket) Type() PacketType {
	return PacketTypeRead
}

// MarshalPacket returns p as a two-part binary encoding of p.
func (p *ReadPacket) MarshalPacket(reqid uint32, b []byte) (header, payload []byte, err error) {
	buf := NewBuffer(b)
	if buf.Cap() < 9 {
		// string(handle) + uint64(offset) + uint32(len)
		size := 4 + len(p.Handle) + 8 + 4
		buf = NewMarshalBuffer(size)
	}

	buf.StartPacket(PacketTypeRead, reqid)
	buf.AppendString(p.Handle)
	buf.AppendUint64(p.Offset)
	buf.AppendUint32(p.Length)

	return buf.Packet(payload)
}

// UnmarshalPacketBody unmarshals the packet body from the given Buffer.
// It is assumed that the uint32(request-id) has already been consumed.
func (p *ReadPacket) UnmarshalPacketBody(buf *Buffer) (err error) {
	*p = ReadPacket{
		Handle: buf.ConsumeString(),
		Offset: buf.ConsumeUint64(),
		Length: buf.ConsumeUint32(),
	}

	return buf.Err
}

// WritePacket defines the SSH_FXP_WRITE packet.
type WritePacket struct {
	Handle string
	Offset uint64
	Data   []byte
}

// Type returns the SSH_FXP_xy value associated with this packet type.
func (p *WritePacket) Type() PacketType {
	return PacketTypeWrite
}

// MarshalPacket returns p as a two-part binary encoding of p.
func (p *WritePacket) MarshalPacket(reqid uint32, b []byte) (header, payload []byte, err error) {
	buf := NewBuffer(b)
	if buf.Cap() < 9 {
		// string(handle) + uint64(offset) + uint32(len(data)); data content in payload
		size := 4 + len(p.Handle) + 8 + 4
		buf = NewMarshalBuffer(size)
	}

	buf.StartPacket(PacketTypeWrite, reqid)
	buf.AppendString(p.Handle)
	buf.AppendUint64(p.Offset)
	buf.AppendUint32(uint32(len(p.Data)))

	return buf.Packet(p.Data)
}

// UnmarshalPacketBody unmarshals the packet body from the given Buffer.
// It is assumed that the uint32(request-id) has already been consumed.
//
// If p.Data is already populated, and of sufficient length to hold the data,
// then this will copy the data into that byte slice.
//
// If p.Data has a length insufficient to hold the data,
// then this will make a new slice of sufficient length, and copy the data into that.
//
// This means this _does not_ alias any of the data buffer that is passed in.
func (p *WritePacket) UnmarshalPacketBody(buf *Buffer) (err error) {
	*p = WritePacket{
		Handle: buf.ConsumeString(),
		Offset: buf.ConsumeUint64(),
		Data:   buf.ConsumeByteSliceCopy(p.Data),
	}

	return buf.Err
}

// FStatPacket defines the SSH_FXP_FSTAT packet.
type FStatPacket struct {
	Handle string
}

// Type returns the SSH_FXP_xy value associated with this packet type.
func (p *FStatPacket) Type() PacketType {
	return PacketTypeFStat
}

// MarshalPacket returns p as a two-part binary encoding of p.
func (p *FStatPacket) MarshalPacket(reqid uint32, b []byte) (header, payload []byte, err error) {
	buf := NewBuffer(b)
	if buf.Cap() < 9 {
		size := 4 + len(p.Handle) // string(handle)
		buf = NewMarshalBuffer(size)
	}

	buf.StartPacket(PacketTypeFStat, reqid)
	buf.AppendString(p.Handle)

	return buf.Packet(payload)
}

// UnmarshalPacketBody unmarshals the packet body from the given Buffer.
// It is assumed that the uint32(request-id) has already been consumed.
func (p *FStatPacket) UnmarshalPacketBody(buf *Buffer) (err error) {
	*p = FStatPacket{
		Handle: buf.ConsumeString(),
	}

	return buf.Err
}

// FSetstatPacket defines the SSH_FXP_FSETSTAT packet.
type FSetstatPacket struct {
	Handle string
	Attrs  Attributes
}

// Type returns the SSH_FXP_xy value associated with this packet type.
func (p *FSetstatPacket) Type() PacketType {
	return PacketTypeFSetstat
}

// MarshalPacket returns p as a two-part binary encoding of p.
func (p *FSetstatPacket) MarshalPacket(reqid uint32, b []byte) (header, payload []byte, err error) {
	buf := NewBuffer(b)
	if buf.Cap() < 9 {
		size := 4 + len(p.Handle) + p.Attrs.Len() // string(handle) + ATTRS(attrs)
		buf = NewMarshalBuffer(size)
	}

	buf.StartPacket(PacketTypeFSetstat, reqid)
	buf.AppendString(p.Handle)

	p.Attrs.MarshalInto(buf)

	return buf.Packet(payload)
}

// UnmarshalPacketBody unmarshals the packet body from the given Buffer.
// It is assumed that the uint32(request-id) has already been consumed.
func (p *FSetstatPacket) UnmarshalPacketBody(buf *Buffer) (err error) {
	*p = FSetstatPacket{
		Handle: buf.ConsumeString(),
	}

	return p.Attrs.UnmarshalFrom(buf)
}

// ReadDirPacket defines the SSH_FXP_READDIR packet.
type ReadDirPacket struct {
	Handle string
}

// Type returns the SSH_FXP_xy value associated with this packet type.
func (p *ReadDirPacket) Type() PacketType {
	return PacketTypeReadDir
}

// MarshalPacket returns p as a two-part binary encoding of p.
func (p *ReadDirPacket) MarshalPacket(reqid uint32, b []byte) (header, payload []byte, err error) {
	buf := NewBuffer(b)
	if buf.Cap() < 9 {
		size := 4 + len(p.Handle) // string(handle)
		buf = NewMarshalBuffer(size)
	}

	buf.StartPacket(PacketTypeReadDir, reqid)
	buf.AppendString(p.Handle)

	return buf.Packet(payload)
}

// UnmarshalPacketBody unmarshals the packet body from the given Buffer.
// It is assumed that the uint32(request-id) has already been consumed.
func (p *ReadDirPacket) UnmarshalPacketBody(buf *Buffer) (err error) {
	*p = ReadDirPacket{
		Handle: buf.ConsumeString(),
	}

	return buf.Err
}
