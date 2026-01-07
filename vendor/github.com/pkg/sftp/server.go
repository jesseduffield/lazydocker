package sftp

// sftp server counterpart

import (
	"encoding"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const (
	// SftpServerWorkerCount defines the number of workers for the SFTP server
	SftpServerWorkerCount = 8
)

type file interface {
	Stat() (os.FileInfo, error)
	ReadAt(b []byte, off int64) (int, error)
	WriteAt(b []byte, off int64) (int, error)
	Readdir(int) ([]os.FileInfo, error)
	Name() string
	Truncate(int64) error
	Chmod(mode fs.FileMode) error
	Chown(uid, gid int) error
	Close() error
}

// Server is an SSH File Transfer Protocol (sftp) server.
// This is intended to provide the sftp subsystem to an ssh server daemon.
// This implementation currently supports most of sftp server protocol version 3,
// as specified at https://filezilla-project.org/specs/draft-ietf-secsh-filexfer-02.txt
type Server struct {
	*serverConn
	debugStream   io.Writer
	readOnly      bool
	pktMgr        *packetManager
	openFiles     map[string]file
	openFilesLock sync.RWMutex
	handleCount   int
	workDir       string
	winRoot       bool
	maxTxPacket   uint32
}

func (svr *Server) nextHandle(f file) string {
	svr.openFilesLock.Lock()
	defer svr.openFilesLock.Unlock()
	svr.handleCount++
	handle := strconv.Itoa(svr.handleCount)
	svr.openFiles[handle] = f
	return handle
}

func (svr *Server) closeHandle(handle string) error {
	svr.openFilesLock.Lock()
	defer svr.openFilesLock.Unlock()
	if f, ok := svr.openFiles[handle]; ok {
		delete(svr.openFiles, handle)
		return f.Close()
	}

	return EBADF
}

func (svr *Server) getHandle(handle string) (file, bool) {
	svr.openFilesLock.RLock()
	defer svr.openFilesLock.RUnlock()
	f, ok := svr.openFiles[handle]
	return f, ok
}

type serverRespondablePacket interface {
	encoding.BinaryUnmarshaler
	id() uint32
	respond(svr *Server) responsePacket
}

// NewServer creates a new Server instance around the provided streams, serving
// content from the root of the filesystem.  Optionally, ServerOption
// functions may be specified to further configure the Server.
//
// A subsequent call to Serve() is required to begin serving files over SFTP.
func NewServer(rwc io.ReadWriteCloser, options ...ServerOption) (*Server, error) {
	svrConn := &serverConn{
		conn: conn{
			Reader:      rwc,
			WriteCloser: rwc,
		},
	}
	s := &Server{
		serverConn:  svrConn,
		debugStream: ioutil.Discard,
		pktMgr:      newPktMgr(svrConn),
		openFiles:   make(map[string]file),
		maxTxPacket: defaultMaxTxPacket,
	}

	for _, o := range options {
		if err := o(s); err != nil {
			return nil, err
		}
	}

	return s, nil
}

// A ServerOption is a function which applies configuration to a Server.
type ServerOption func(*Server) error

// WithDebug enables Server debugging output to the supplied io.Writer.
func WithDebug(w io.Writer) ServerOption {
	return func(s *Server) error {
		s.debugStream = w
		return nil
	}
}

// ReadOnly configures a Server to serve files in read-only mode.
func ReadOnly() ServerOption {
	return func(s *Server) error {
		s.readOnly = true
		return nil
	}
}

// WindowsRootEnumeratesDrives configures a Server to serve a virtual '/' for windows that lists all drives
func WindowsRootEnumeratesDrives() ServerOption {
	return func(s *Server) error {
		s.winRoot = true
		return nil
	}
}

// WithAllocator enable the allocator.
// After processing a packet we keep in memory the allocated slices
// and we reuse them for new packets.
// The allocator is experimental
func WithAllocator() ServerOption {
	return func(s *Server) error {
		alloc := newAllocator()
		s.pktMgr.alloc = alloc
		s.conn.alloc = alloc
		return nil
	}
}

// WithServerWorkingDirectory sets a working directory to use as base
// for relative paths.
// If unset the default is current working directory (os.Getwd).
func WithServerWorkingDirectory(workDir string) ServerOption {
	return func(s *Server) error {
		s.workDir = cleanPath(workDir)
		return nil
	}
}

// WithMaxTxPacket sets the maximum size of the payload returned to the client,
// measured in bytes. The default value is 32768 bytes, and this option
// can only be used to increase it. Setting this option to a larger value
// should be safe, because the client decides the size of the requested payload.
//
// The default maximum packet size is 32768 bytes.
func WithMaxTxPacket(size uint32) ServerOption {
	return func(s *Server) error {
		if size < defaultMaxTxPacket {
			return errors.New("size must be greater than or equal to 32768")
		}

		s.maxTxPacket = size

		return nil
	}
}

type rxPacket struct {
	pktType  fxp
	pktBytes []byte
}

// Up to N parallel servers
func (svr *Server) sftpServerWorker(pktChan chan orderedRequest) error {
	for pkt := range pktChan {
		// readonly checks
		readonly := true
		switch pkt := pkt.requestPacket.(type) {
		case notReadOnly:
			readonly = false
		case *sshFxpOpenPacket:
			readonly = pkt.readonly()
		case *sshFxpExtendedPacket:
			readonly = pkt.readonly()
		}

		// If server is operating read-only and a write operation is requested,
		// return permission denied
		if !readonly && svr.readOnly {
			svr.pktMgr.readyPacket(
				svr.pktMgr.newOrderedResponse(statusFromError(pkt.id(), syscall.EPERM), pkt.orderID()),
			)
			continue
		}

		if err := handlePacket(svr, pkt); err != nil {
			return err
		}
	}
	return nil
}

func handlePacket(s *Server, p orderedRequest) error {
	var rpkt responsePacket
	orderID := p.orderID()
	switch p := p.requestPacket.(type) {
	case *sshFxInitPacket:
		rpkt = &sshFxVersionPacket{
			Version:    sftpProtocolVersion,
			Extensions: sftpExtensions,
		}
	case *sshFxpStatPacket:
		// stat the requested file
		info, err := os.Stat(s.toLocalPath(p.Path))
		rpkt = &sshFxpStatResponse{
			ID:   p.ID,
			info: info,
		}
		if err != nil {
			rpkt = statusFromError(p.ID, err)
		}
	case *sshFxpLstatPacket:
		// stat the requested file
		info, err := s.lstat(s.toLocalPath(p.Path))
		rpkt = &sshFxpStatResponse{
			ID:   p.ID,
			info: info,
		}
		if err != nil {
			rpkt = statusFromError(p.ID, err)
		}
	case *sshFxpFstatPacket:
		f, ok := s.getHandle(p.Handle)
		var err error = EBADF
		var info os.FileInfo
		if ok {
			info, err = f.Stat()
			rpkt = &sshFxpStatResponse{
				ID:   p.ID,
				info: info,
			}
		}
		if err != nil {
			rpkt = statusFromError(p.ID, err)
		}
	case *sshFxpMkdirPacket:
		// TODO FIXME: ignore flags field
		err := os.Mkdir(s.toLocalPath(p.Path), 0o755)
		rpkt = statusFromError(p.ID, err)
	case *sshFxpRmdirPacket:
		err := os.Remove(s.toLocalPath(p.Path))
		rpkt = statusFromError(p.ID, err)
	case *sshFxpRemovePacket:
		err := os.Remove(s.toLocalPath(p.Filename))
		rpkt = statusFromError(p.ID, err)
	case *sshFxpRenamePacket:
		err := os.Rename(s.toLocalPath(p.Oldpath), s.toLocalPath(p.Newpath))
		rpkt = statusFromError(p.ID, err)
	case *sshFxpSymlinkPacket:
		err := os.Symlink(s.toLocalPath(p.Targetpath), s.toLocalPath(p.Linkpath))
		rpkt = statusFromError(p.ID, err)
	case *sshFxpClosePacket:
		rpkt = statusFromError(p.ID, s.closeHandle(p.Handle))
	case *sshFxpReadlinkPacket:
		f, err := os.Readlink(s.toLocalPath(p.Path))
		rpkt = &sshFxpNamePacket{
			ID: p.ID,
			NameAttrs: []*sshFxpNameAttr{
				{
					Name:     f,
					LongName: f,
					Attrs:    emptyFileStat,
				},
			},
		}
		if err != nil {
			rpkt = statusFromError(p.ID, err)
		}
	case *sshFxpRealpathPacket:
		f, err := filepath.Abs(s.toLocalPath(p.Path))
		f = cleanPath(f)
		rpkt = &sshFxpNamePacket{
			ID: p.ID,
			NameAttrs: []*sshFxpNameAttr{
				{
					Name:     f,
					LongName: f,
					Attrs:    emptyFileStat,
				},
			},
		}
		if err != nil {
			rpkt = statusFromError(p.ID, err)
		}
	case *sshFxpOpendirPacket:
		lp := s.toLocalPath(p.Path)

		if stat, err := s.stat(lp); err != nil {
			rpkt = statusFromError(p.ID, err)
		} else if !stat.IsDir() {
			rpkt = statusFromError(p.ID, &os.PathError{
				Path: lp, Err: syscall.ENOTDIR,
			})
		} else {
			rpkt = (&sshFxpOpenPacket{
				ID:     p.ID,
				Path:   p.Path,
				Pflags: sshFxfRead,
			}).respond(s)
		}
	case *sshFxpReadPacket:
		var err error = EBADF
		f, ok := s.getHandle(p.Handle)
		if ok {
			err = nil
			data := p.getDataSlice(s.pktMgr.alloc, orderID, s.maxTxPacket)
			n, _err := f.ReadAt(data, int64(p.Offset))
			if _err != nil && (_err != io.EOF || n == 0) {
				err = _err
			}
			rpkt = &sshFxpDataPacket{
				ID:     p.ID,
				Length: uint32(n),
				Data:   data[:n],
				// do not use data[:n:n] here to clamp the capacity, we allocated extra capacity above to avoid reallocations
			}
		}
		if err != nil {
			rpkt = statusFromError(p.ID, err)
		}

	case *sshFxpWritePacket:
		f, ok := s.getHandle(p.Handle)
		var err error = EBADF
		if ok {
			_, err = f.WriteAt(p.Data, int64(p.Offset))
		}
		rpkt = statusFromError(p.ID, err)
	case *sshFxpExtendedPacket:
		if p.SpecificPacket == nil {
			rpkt = statusFromError(p.ID, ErrSSHFxOpUnsupported)
		} else {
			rpkt = p.respond(s)
		}
	case serverRespondablePacket:
		rpkt = p.respond(s)
	default:
		return fmt.Errorf("unexpected packet type %T", p)
	}

	s.pktMgr.readyPacket(s.pktMgr.newOrderedResponse(rpkt, orderID))
	return nil
}

// Serve serves SFTP connections until the streams stop or the SFTP subsystem
// is stopped. It returns nil if the server exits cleanly.
func (svr *Server) Serve() error {
	defer func() {
		if svr.pktMgr.alloc != nil {
			svr.pktMgr.alloc.Free()
		}
	}()
	var wg sync.WaitGroup
	runWorker := func(ch chan orderedRequest) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := svr.sftpServerWorker(ch); err != nil {
				svr.conn.Close() // shuts down recvPacket
			}
		}()
	}
	pktChan := svr.pktMgr.workerChan(runWorker)

	var err error
	var pkt requestPacket
	var pktType uint8
	var pktBytes []byte
	for {
		pktType, pktBytes, err = svr.serverConn.recvPacket(svr.pktMgr.getNextOrderID())
		if err != nil {
			// Check whether the connection terminated cleanly in-between packets.
			if err == io.EOF {
				err = nil
			}
			// we don't care about releasing allocated pages here, the server will quit and the allocator freed
			break
		}

		pkt, err = makePacket(rxPacket{fxp(pktType), pktBytes})
		if err != nil {
			switch {
			case errors.Is(err, errUnknownExtendedPacket):
				//if err := svr.serverConn.sendError(pkt, ErrSshFxOpUnsupported); err != nil {
				//	debug("failed to send err packet: %v", err)
				//	svr.conn.Close() // shuts down recvPacket
				//	break
				//}
			default:
				debug("makePacket err: %v", err)
				svr.conn.Close() // shuts down recvPacket
				break
			}
		}

		pktChan <- svr.pktMgr.newOrderedRequest(pkt)
	}

	close(pktChan) // shuts down sftpServerWorkers
	wg.Wait()      // wait for all workers to exit

	// close any still-open files
	for handle, file := range svr.openFiles {
		fmt.Fprintf(svr.debugStream, "sftp server file with handle %q left open: %v\n", handle, file.Name())
		file.Close()
	}
	return err // error from recvPacket
}

type ider interface {
	id() uint32
}

// The init packet has no ID, so we just return a zero-value ID
func (p *sshFxInitPacket) id() uint32 { return 0 }

type sshFxpStatResponse struct {
	ID   uint32
	info os.FileInfo
}

func (p *sshFxpStatResponse) marshalPacket() ([]byte, []byte, error) {
	l := 4 + 1 + 4 // uint32(length) + byte(type) + uint32(id)

	b := make([]byte, 4, l)
	b = append(b, sshFxpAttrs)
	b = marshalUint32(b, p.ID)

	var payload []byte
	payload = marshalFileInfo(payload, p.info)

	return b, payload, nil
}

func (p *sshFxpStatResponse) MarshalBinary() ([]byte, error) {
	header, payload, err := p.marshalPacket()
	return append(header, payload...), err
}

var emptyFileStat = []interface{}{uint32(0)}

func (p *sshFxpOpenPacket) readonly() bool {
	return !p.hasPflags(sshFxfWrite)
}

func (p *sshFxpOpenPacket) hasPflags(flags ...uint32) bool {
	for _, f := range flags {
		if p.Pflags&f == 0 {
			return false
		}
	}
	return true
}

func (p *sshFxpOpenPacket) respond(svr *Server) responsePacket {
	var osFlags int
	if p.hasPflags(sshFxfRead, sshFxfWrite) {
		osFlags |= os.O_RDWR
	} else if p.hasPflags(sshFxfWrite) {
		osFlags |= os.O_WRONLY
	} else if p.hasPflags(sshFxfRead) {
		osFlags |= os.O_RDONLY
	} else {
		// how are they opening?
		return statusFromError(p.ID, syscall.EINVAL)
	}

	// Don't use O_APPEND flag as it conflicts with WriteAt.
	// The sshFxfAppend flag is a no-op here as the client sends the offsets.

	if p.hasPflags(sshFxfCreat) {
		osFlags |= os.O_CREATE
	}
	if p.hasPflags(sshFxfTrunc) {
		osFlags |= os.O_TRUNC
	}
	if p.hasPflags(sshFxfExcl) {
		osFlags |= os.O_EXCL
	}

	mode := os.FileMode(0o644)
	// Like OpenSSH, we only handle permissions here, and only when the file is being created.
	// Otherwise, the permissions are ignored.
	if p.Flags&sshFileXferAttrPermissions != 0 {
		fs, err := p.unmarshalFileStat(p.Flags)
		if err != nil {
			return statusFromError(p.ID, err)
		}
		mode = fs.FileMode() & os.ModePerm
	}

	f, err := svr.openfile(svr.toLocalPath(p.Path), osFlags, mode)
	if err != nil {
		return statusFromError(p.ID, err)
	}

	handle := svr.nextHandle(f)
	return &sshFxpHandlePacket{ID: p.ID, Handle: handle}
}

func (p *sshFxpReaddirPacket) respond(svr *Server) responsePacket {
	f, ok := svr.getHandle(p.Handle)
	if !ok {
		return statusFromError(p.ID, EBADF)
	}

	dirents, err := f.Readdir(128)
	if err != nil {
		return statusFromError(p.ID, err)
	}

	idLookup := osIDLookup{}

	ret := &sshFxpNamePacket{ID: p.ID}
	for _, dirent := range dirents {
		ret.NameAttrs = append(ret.NameAttrs, &sshFxpNameAttr{
			Name:     dirent.Name(),
			LongName: runLs(idLookup, dirent),
			Attrs:    []interface{}{dirent},
		})
	}
	return ret
}

func (p *sshFxpSetstatPacket) respond(svr *Server) responsePacket {
	path := svr.toLocalPath(p.Path)

	debug("setstat name %q", path)

	fs, err := p.unmarshalFileStat(p.Flags)

	if err == nil && (p.Flags&sshFileXferAttrSize) != 0 {
		err = os.Truncate(path, int64(fs.Size))
	}
	if err == nil && (p.Flags&sshFileXferAttrPermissions) != 0 {
		err = os.Chmod(path, fs.FileMode())
	}
	if err == nil && (p.Flags&sshFileXferAttrUIDGID) != 0 {
		err = os.Chown(path, int(fs.UID), int(fs.GID))
	}
	if err == nil && (p.Flags&sshFileXferAttrACmodTime) != 0 {
		err = os.Chtimes(path, fs.AccessTime(), fs.ModTime())
	}

	return statusFromError(p.ID, err)
}

func (p *sshFxpFsetstatPacket) respond(svr *Server) responsePacket {
	f, ok := svr.getHandle(p.Handle)
	if !ok {
		return statusFromError(p.ID, EBADF)
	}

	path := f.Name()

	debug("fsetstat name %q", path)

	fs, err := p.unmarshalFileStat(p.Flags)

	if err == nil && (p.Flags&sshFileXferAttrSize) != 0 {
		err = f.Truncate(int64(fs.Size))
	}
	if err == nil && (p.Flags&sshFileXferAttrPermissions) != 0 {
		err = f.Chmod(fs.FileMode())
	}
	if err == nil && (p.Flags&sshFileXferAttrUIDGID) != 0 {
		err = f.Chown(int(fs.UID), int(fs.GID))
	}
	if err == nil && (p.Flags&sshFileXferAttrACmodTime) != 0 {
		type chtimer interface {
			Chtimes(atime, mtime time.Time) error
		}

		switch f := interface{}(f).(type) {
		case chtimer:
			// future-compatible, for when/if *os.File supports Chtimes.
			err = f.Chtimes(fs.AccessTime(), fs.ModTime())
		default:
			err = os.Chtimes(path, fs.AccessTime(), fs.ModTime())
		}
	}

	return statusFromError(p.ID, err)
}

func statusFromError(id uint32, err error) *sshFxpStatusPacket {
	ret := &sshFxpStatusPacket{
		ID: id,
		StatusError: StatusError{
			// sshFXOk               = 0
			// sshFXEOF              = 1
			// sshFXNoSuchFile       = 2 ENOENT
			// sshFXPermissionDenied = 3
			// sshFXFailure          = 4
			// sshFXBadMessage       = 5
			// sshFXNoConnection     = 6
			// sshFXConnectionLost   = 7
			// sshFXOPUnsupported    = 8
			Code: sshFxOk,
		},
	}
	if err == nil {
		return ret
	}

	debug("statusFromError: error is %T %#v", err, err)
	ret.StatusError.Code = sshFxFailure
	ret.StatusError.msg = err.Error()

	if os.IsNotExist(err) {
		ret.StatusError.Code = sshFxNoSuchFile
		return ret
	}
	if code, ok := translateSyscallError(err); ok {
		ret.StatusError.Code = code
		return ret
	}

	if errors.Is(err, io.EOF) {
		ret.StatusError.Code = sshFxEOF
		return ret
	}

	var e fxerr
	if errors.As(err, &e) {
		ret.StatusError.Code = uint32(e)
		return ret
	}

	return ret
}
