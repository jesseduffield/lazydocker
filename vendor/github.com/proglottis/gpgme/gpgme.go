// Package gpgme provides a Go wrapper for the GPGME library
package gpgme

// #cgo pkg-config: gpgme
// #cgo CPPFLAGS: -D_FILE_OFFSET_BITS=64
// #include <stdlib.h>
// #include <gpgme.h>
// #include "go_gpgme.h"
import "C"

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/cgo"
	"time"
	"unsafe"
)

var Version string

func init() {
	Version = C.GoString(C.gpgme_check_version(nil))
}

// Callback is the function that is called when a passphrase is required
type Callback func(uidHint string, prevWasBad bool, f *os.File) error

//export gogpgme_passfunc
func gogpgme_passfunc(hook unsafe.Pointer, uid_hint, passphrase_info *C.char, prev_was_bad, fd C.int) C.gpgme_error_t {
	h := *(*cgo.Handle)(hook)
	c := h.Value().(*Context)
	go_uid_hint := C.GoString(uid_hint)
	f := os.NewFile(uintptr(fd), go_uid_hint)
	defer f.Close()
	err := c.callback(go_uid_hint, prev_was_bad != 0, f)
	if err != nil {
		return C.GPG_ERR_CANCELED
	}
	return 0
}

type Protocol int

const (
	ProtocolOpenPGP  Protocol = C.GPGME_PROTOCOL_OpenPGP
	ProtocolCMS      Protocol = C.GPGME_PROTOCOL_CMS
	ProtocolGPGConf  Protocol = C.GPGME_PROTOCOL_GPGCONF
	ProtocolAssuan   Protocol = C.GPGME_PROTOCOL_ASSUAN
	ProtocolG13      Protocol = C.GPGME_PROTOCOL_G13
	ProtocolUIServer Protocol = C.GPGME_PROTOCOL_UISERVER
	ProtocolDefault  Protocol = C.GPGME_PROTOCOL_DEFAULT
	ProtocolUnknown  Protocol = C.GPGME_PROTOCOL_UNKNOWN
)

type PinEntryMode int

const (
	PinEntryDefault  PinEntryMode = C.GPGME_PINENTRY_MODE_DEFAULT
	PinEntryAsk      PinEntryMode = C.GPGME_PINENTRY_MODE_ASK
	PinEntryCancel   PinEntryMode = C.GPGME_PINENTRY_MODE_CANCEL
	PinEntryError    PinEntryMode = C.GPGME_PINENTRY_MODE_ERROR
	PinEntryLoopback PinEntryMode = C.GPGME_PINENTRY_MODE_LOOPBACK
)

type EncryptFlag uint

const (
	EncryptAlwaysTrust EncryptFlag = C.GPGME_ENCRYPT_ALWAYS_TRUST
	EncryptNoEncryptTo EncryptFlag = C.GPGME_ENCRYPT_NO_ENCRYPT_TO
	EncryptPrepare     EncryptFlag = C.GPGME_ENCRYPT_PREPARE
	EncryptExceptSign  EncryptFlag = C.GPGME_ENCRYPT_EXPECT_SIGN
)

type HashAlgo int

// const values for HashAlgo values should be added when necessary.

type KeyListMode uint

const (
	KeyListModeLocal        KeyListMode = C.GPGME_KEYLIST_MODE_LOCAL
	KeyListModeExtern       KeyListMode = C.GPGME_KEYLIST_MODE_EXTERN
	KeyListModeSigs         KeyListMode = C.GPGME_KEYLIST_MODE_SIGS
	KeyListModeSigNotations KeyListMode = C.GPGME_KEYLIST_MODE_SIG_NOTATIONS
	KeyListModeEphemeral    KeyListMode = C.GPGME_KEYLIST_MODE_EPHEMERAL
	KeyListModeModeValidate KeyListMode = C.GPGME_KEYLIST_MODE_VALIDATE
)

type PubkeyAlgo int

// const values for PubkeyAlgo values should be added when necessary.

type SigMode int

const (
	SigModeNormal SigMode = C.GPGME_SIG_MODE_NORMAL
	SigModeDetach SigMode = C.GPGME_SIG_MODE_DETACH
	SigModeClear  SigMode = C.GPGME_SIG_MODE_CLEAR
)

type SigSum int

const (
	SigSumValid      SigSum = C.GPGME_SIGSUM_VALID
	SigSumGreen      SigSum = C.GPGME_SIGSUM_GREEN
	SigSumRed        SigSum = C.GPGME_SIGSUM_RED
	SigSumKeyRevoked SigSum = C.GPGME_SIGSUM_KEY_REVOKED
	SigSumKeyExpired SigSum = C.GPGME_SIGSUM_KEY_EXPIRED
	SigSumSigExpired SigSum = C.GPGME_SIGSUM_SIG_EXPIRED
	SigSumKeyMissing SigSum = C.GPGME_SIGSUM_KEY_MISSING
	SigSumCRLMissing SigSum = C.GPGME_SIGSUM_CRL_MISSING
	SigSumCRLTooOld  SigSum = C.GPGME_SIGSUM_CRL_TOO_OLD
	SigSumBadPolicy  SigSum = C.GPGME_SIGSUM_BAD_POLICY
	SigSumSysError   SigSum = C.GPGME_SIGSUM_SYS_ERROR
)

type Validity int

const (
	ValidityUnknown   Validity = C.GPGME_VALIDITY_UNKNOWN
	ValidityUndefined Validity = C.GPGME_VALIDITY_UNDEFINED
	ValidityNever     Validity = C.GPGME_VALIDITY_NEVER
	ValidityMarginal  Validity = C.GPGME_VALIDITY_MARGINAL
	ValidityFull      Validity = C.GPGME_VALIDITY_FULL
	ValidityUltimate  Validity = C.GPGME_VALIDITY_ULTIMATE
)

type ErrorCode int

const (
	ErrorNoError ErrorCode = C.GPG_ERR_NO_ERROR
	ErrorEOF     ErrorCode = C.GPG_ERR_EOF
)

// Error is a wrapper for GPGME errors
type Error struct {
	err C.gpgme_error_t
}

func (e Error) Code() ErrorCode {
	return ErrorCode(C.gpgme_err_code(e.err))
}

func (e Error) Error() string {
	return C.GoString(C.gpgme_strerror(e.err))
}

func handleError(err C.gpgme_error_t) error {
	e := Error{err: err}
	if e.Code() == ErrorNoError {
		return nil
	}
	return e
}

func cbool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

func EngineCheckVersion(p Protocol) error {
	return handleError(C.gpgme_engine_check_version(C.gpgme_protocol_t(p)))
}

type EngineInfo struct {
	next            *EngineInfo
	protocol        Protocol
	fileName        string
	homeDir         string
	version         string
	requiredVersion string
}

func copyEngineInfo(info C.gpgme_engine_info_t) *EngineInfo {
	res := &EngineInfo{
		next:            nil,
		protocol:        Protocol(info.protocol),
		fileName:        C.GoString(info.file_name),
		homeDir:         C.GoString(info.home_dir),
		version:         C.GoString(info.version),
		requiredVersion: C.GoString(info.req_version),
	}
	if info.next != nil {
		res.next = copyEngineInfo(info.next)
	}
	return res
}

func (e *EngineInfo) Next() *EngineInfo {
	return e.next
}

func (e *EngineInfo) Protocol() Protocol {
	return e.protocol
}

func (e *EngineInfo) FileName() string {
	return e.fileName
}

func (e *EngineInfo) Version() string {
	return e.version
}

func (e *EngineInfo) RequiredVersion() string {
	return e.requiredVersion
}

func (e *EngineInfo) HomeDir() string {
	return e.homeDir
}

func GetEngineInfo() (*EngineInfo, error) {
	var cInfo C.gpgme_engine_info_t
	err := handleError(C.gpgme_get_engine_info(&cInfo))
	if err != nil {
		return nil, err
	}
	return copyEngineInfo(cInfo), nil // It is up to the caller not to invalidate cInfo concurrently until this is done.
}

func SetEngineInfo(proto Protocol, fileName, homeDir string) error {
	var cfn, chome *C.char
	if fileName != "" {
		cfn = C.CString(fileName)
		defer C.free(unsafe.Pointer(cfn))
	}
	if homeDir != "" {
		chome = C.CString(homeDir)
		defer C.free(unsafe.Pointer(chome))
	}
	return handleError(C.gpgme_set_engine_info(C.gpgme_protocol_t(proto), cfn, chome))
}

func GetDirInfo(what string) string {
	cwhat := C.CString(what)
	defer C.free(unsafe.Pointer(cwhat))

	cdir := C.gpgme_get_dirinfo(cwhat)
	if cdir == nil {
		return ""
	}
	return C.GoString(cdir)
}

func FindKeys(pattern string, secretOnly bool) ([]*Key, error) {
	var keys []*Key
	ctx, err := New()
	if err != nil {
		return keys, err
	}
	defer ctx.Release()
	if err := ctx.KeyListStart(pattern, secretOnly); err != nil {
		return keys, err
	}
	defer func() { _ = ctx.KeyListEnd() }()
	for ctx.KeyListNext() {
		keys = append(keys, ctx.Key)
	}
	if ctx.KeyError != nil {
		return keys, ctx.KeyError
	}
	return keys, nil
}

func Decrypt(r io.Reader) (*Data, error) {
	ctx, err := New()
	if err != nil {
		return nil, err
	}
	defer ctx.Release()
	cipher, err := NewDataReader(r)
	if err != nil {
		return nil, err
	}
	defer cipher.Close()
	plain, err := NewData()
	if err != nil {
		return nil, err
	}
	if err := ctx.Decrypt(cipher, plain); err != nil {
		return nil, err
	}
	_, err = plain.Seek(0, SeekSet)
	return plain, err
}

type Context struct {
	Key      *Key
	KeyError error

	callback Callback
	cbc      cgo.Handle // WARNING: Call runtime.KeepAlive(c) after ANY use of c.cbc in C (typically via c.ctx)

	ctx C.gpgme_ctx_t // WARNING: Call runtime.KeepAlive(c) after ANY passing of c.ctx to C
}

func New() (*Context, error) {
	c := &Context{}
	err := C.gpgme_new(&c.ctx)
	runtime.SetFinalizer(c, (*Context).Release)
	return c, handleError(err)
}

func (c *Context) Release() {
	if c.ctx == nil {
		return
	}
	if c.cbc > 0 {
		c.cbc.Delete()
	}
	C.gpgme_release(c.ctx)
	runtime.KeepAlive(c)
	c.ctx = nil
}

func (c *Context) SetArmor(yes bool) {
	C.gpgme_set_armor(c.ctx, cbool(yes))
	runtime.KeepAlive(c)
}

func (c *Context) Armor() bool {
	res := C.gpgme_get_armor(c.ctx) != 0
	runtime.KeepAlive(c)
	return res
}

func (c *Context) SetTextMode(yes bool) {
	C.gpgme_set_textmode(c.ctx, cbool(yes))
	runtime.KeepAlive(c)
}

func (c *Context) TextMode() bool {
	res := C.gpgme_get_textmode(c.ctx) != 0
	runtime.KeepAlive(c)
	return res
}

func (c *Context) SetProtocol(p Protocol) error {
	err := handleError(C.gpgme_set_protocol(c.ctx, C.gpgme_protocol_t(p)))
	runtime.KeepAlive(c)
	return err
}

func (c *Context) Protocol() Protocol {
	res := Protocol(C.gpgme_get_protocol(c.ctx))
	runtime.KeepAlive(c)
	return res
}

func (c *Context) SetKeyListMode(m KeyListMode) error {
	err := handleError(C.gpgme_set_keylist_mode(c.ctx, C.gpgme_keylist_mode_t(m)))
	runtime.KeepAlive(c)
	return err
}

func (c *Context) KeyListMode() KeyListMode {
	res := KeyListMode(C.gpgme_get_keylist_mode(c.ctx))
	runtime.KeepAlive(c)
	return res
}

func (c *Context) SetPinEntryMode(m PinEntryMode) error {
	err := handleError(C.gpgme_set_pinentry_mode(c.ctx, C.gpgme_pinentry_mode_t(m)))
	runtime.KeepAlive(c)
	return err
}

func (c *Context) PinEntryMode() PinEntryMode {
	res := PinEntryMode(C.gpgme_get_pinentry_mode(c.ctx))
	runtime.KeepAlive(c)
	return res
}

func (c *Context) SetCallback(callback Callback) error {
	var err error
	c.callback = callback
	if c.cbc > 0 {
		c.cbc.Delete()
	}
	if callback != nil {
		c.cbc = cgo.NewHandle(c)
		_, err = C.gpgme_set_passphrase_cb(c.ctx, C.gpgme_passphrase_cb_t(C.gogpgme_passfunc), unsafe.Pointer(&c.cbc))
	} else {
		c.cbc = 0
		_, err = C.gpgme_set_passphrase_cb(c.ctx, nil, nil)
	}
	runtime.KeepAlive(c)
	return err
}

func (c *Context) EngineInfo() *EngineInfo {
	cInfo := C.gpgme_ctx_get_engine_info(c.ctx)
	runtime.KeepAlive(c)
	// NOTE: c must be live as long as we are accessing cInfo.
	res := copyEngineInfo(cInfo)
	runtime.KeepAlive(c) // for accesses to cInfo
	return res
}

func (c *Context) SetEngineInfo(proto Protocol, fileName, homeDir string) error {
	var cfn, chome *C.char
	if fileName != "" {
		cfn = C.CString(fileName)
		defer C.free(unsafe.Pointer(cfn))
	}
	if homeDir != "" {
		chome = C.CString(homeDir)
		defer C.free(unsafe.Pointer(chome))
	}
	err := handleError(C.gpgme_ctx_set_engine_info(c.ctx, C.gpgme_protocol_t(proto), cfn, chome))
	runtime.KeepAlive(c)
	return err
}

func (c *Context) KeyListStart(pattern string, secretOnly bool) error {
	cpattern := C.CString(pattern)
	defer C.free(unsafe.Pointer(cpattern))
	err := handleError(C.gpgme_op_keylist_start(c.ctx, cpattern, cbool(secretOnly)))
	runtime.KeepAlive(c)
	return err
}

func (c *Context) KeyListNext() bool {
	c.Key = newKey()
	err := handleError(C.gpgme_op_keylist_next(c.ctx, &c.Key.k))
	runtime.KeepAlive(c) // implies runtime.KeepAlive(c.Key)
	if err != nil {
		if e, ok := err.(Error); ok && e.Code() == ErrorEOF {
			c.KeyError = nil
		} else {
			c.KeyError = err
		}
		return false
	}
	c.KeyError = nil
	return true
}

func (c *Context) KeyListEnd() error {
	err := handleError(C.gpgme_op_keylist_end(c.ctx))
	runtime.KeepAlive(c)
	return err
}

func (c *Context) GetKey(fingerprint string, secret bool) (*Key, error) {
	key := newKey()
	cfpr := C.CString(fingerprint)
	defer C.free(unsafe.Pointer(cfpr))
	err := handleError(C.gpgme_get_key(c.ctx, cfpr, &key.k, cbool(secret)))
	runtime.KeepAlive(c)
	runtime.KeepAlive(key)
	keyKIsNil := key.k == nil
	runtime.KeepAlive(key)
	if e, ok := err.(Error); keyKIsNil && ok && e.Code() == ErrorEOF {
		return nil, fmt.Errorf("key %q not found", fingerprint)
	}
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (c *Context) Decrypt(ciphertext, plaintext *Data) error {
	err := handleError(C.gpgme_op_decrypt(c.ctx, ciphertext.dh, plaintext.dh))
	runtime.KeepAlive(c)
	runtime.KeepAlive(ciphertext)
	runtime.KeepAlive(plaintext)
	return err
}

func (c *Context) DecryptVerify(ciphertext, plaintext *Data) error {
	err := handleError(C.gpgme_op_decrypt_verify(c.ctx, ciphertext.dh, plaintext.dh))
	runtime.KeepAlive(c)
	runtime.KeepAlive(ciphertext)
	runtime.KeepAlive(plaintext)
	return err
}

type Signature struct {
	Summary        SigSum
	Fingerprint    string
	Status         error
	Timestamp      time.Time
	ExpTimestamp   time.Time
	WrongKeyUsage  bool
	PKATrust       uint
	ChainModel     bool
	Validity       Validity
	ValidityReason error
	PubkeyAlgo     PubkeyAlgo
	HashAlgo       HashAlgo
}

func (c *Context) Verify(sig, signedText, plain *Data) (string, []Signature, error) {
	var signedTextPtr, plainPtr C.gpgme_data_t = nil, nil
	if signedText != nil {
		signedTextPtr = signedText.dh
	}
	if plain != nil {
		plainPtr = plain.dh
	}
	err := handleError(C.gpgme_op_verify(c.ctx, sig.dh, signedTextPtr, plainPtr))
	runtime.KeepAlive(c)
	runtime.KeepAlive(sig)
	if signedText != nil {
		runtime.KeepAlive(signedText)
	}
	if plain != nil {
		runtime.KeepAlive(plain)
	}
	if err != nil {
		return "", nil, err
	}
	res := C.gpgme_op_verify_result(c.ctx)
	runtime.KeepAlive(c)
	// NOTE: c must be live as long as we are accessing res.
	sigs := []Signature{}
	for s := res.signatures; s != nil; s = s.next {
		sig := Signature{
			Summary:     SigSum(s.summary),
			Fingerprint: C.GoString(s.fpr),
			Status:      handleError(s.status),
			// s.notations not implemented
			Timestamp:      time.Unix(int64(s.timestamp), 0),
			ExpTimestamp:   time.Unix(int64(s.exp_timestamp), 0),
			WrongKeyUsage:  C.signature_wrong_key_usage(s) != 0,
			PKATrust:       uint(C.signature_pka_trust(s)),
			ChainModel:     C.signature_chain_model(s) != 0,
			Validity:       Validity(s.validity),
			ValidityReason: handleError(s.validity_reason),
			PubkeyAlgo:     PubkeyAlgo(s.pubkey_algo),
			HashAlgo:       HashAlgo(s.hash_algo),
		}
		sigs = append(sigs, sig)
	}
	fileName := C.GoString(res.file_name)
	runtime.KeepAlive(c) // for all accesses to res above
	return fileName, sigs, nil
}

func (c *Context) Encrypt(recipients []*Key, flags EncryptFlag, plaintext, ciphertext *Data) error {
	size := unsafe.Sizeof(new(C.gpgme_key_t))
	recp := C.calloc(C.size_t(len(recipients)+1), C.size_t(size))
	defer C.free(recp)
	for i := range recipients {
		ptr := (*C.gpgme_key_t)(unsafe.Pointer(uintptr(recp) + size*uintptr(i)))
		*ptr = recipients[i].k
	}
	err := C.gpgme_op_encrypt(c.ctx, (*C.gpgme_key_t)(recp), C.gpgme_encrypt_flags_t(flags), plaintext.dh, ciphertext.dh)
	runtime.KeepAlive(c)
	runtime.KeepAlive(recipients)
	runtime.KeepAlive(plaintext)
	runtime.KeepAlive(ciphertext)
	return handleError(err)
}

func (c *Context) Sign(signers []*Key, plain, sig *Data, mode SigMode) error {
	C.gpgme_signers_clear(c.ctx)
	runtime.KeepAlive(c)
	for _, k := range signers {
		err := handleError(C.gpgme_signers_add(c.ctx, k.k))
		runtime.KeepAlive(c)
		runtime.KeepAlive(k)
		if err != nil {
			C.gpgme_signers_clear(c.ctx)
			runtime.KeepAlive(c)
			return err
		}
	}
	err := handleError(C.gpgme_op_sign(c.ctx, plain.dh, sig.dh, C.gpgme_sig_mode_t(mode)))
	runtime.KeepAlive(c)
	runtime.KeepAlive(plain)
	runtime.KeepAlive(sig)
	return err
}

type (
	AssuanDataCallback    func(data []byte) error
	AssuanInquireCallback func(name, args string) error
	AssuanStatusCallback  func(status, args string) error
)

// AssuanSend sends a raw Assuan command to gpg-agent
func (c *Context) AssuanSend(
	cmd string,
	data AssuanDataCallback,
	inquiry AssuanInquireCallback,
	status AssuanStatusCallback,
) error {
	var operr C.gpgme_error_t

	dataPtr := cgo.NewHandle(&data)
	inquiryPtr := cgo.NewHandle(&inquiry)
	statusPtr := cgo.NewHandle(&status)
	cmdCStr := C.CString(cmd)
	defer C.free(unsafe.Pointer(cmdCStr))
	err := C.gogpgme_op_assuan_transact_ext(
		c.ctx,
		cmdCStr,
		unsafe.Pointer(&dataPtr),
		unsafe.Pointer(&inquiryPtr),
		unsafe.Pointer(&statusPtr),
		&operr,
	)
	runtime.KeepAlive(c)

	if handleError(operr) != nil {
		return handleError(operr)
	}
	return handleError(err)
}

//export gogpgme_assuan_data_callback
func gogpgme_assuan_data_callback(handle unsafe.Pointer, data unsafe.Pointer, datalen C.size_t) C.gpgme_error_t {
	h := *(*cgo.Handle)(handle)
	c := h.Value().(*AssuanDataCallback)
	if *c == nil {
		return 0
	}
	if err := (*c)(C.GoBytes(data, C.int(datalen))); err != nil {
		return C.gpgme_error(C.GPG_ERR_USER_1)
	}
	return 0
}

//export gogpgme_assuan_inquiry_callback
func gogpgme_assuan_inquiry_callback(handle unsafe.Pointer, cName *C.char, cArgs *C.char) C.gpgme_error_t {
	name := C.GoString(cName)
	args := C.GoString(cArgs)
	h := *(*cgo.Handle)(handle)
	c := h.Value().(*AssuanInquireCallback)
	if *c == nil {
		return 0
	}
	if err := (*c)(name, args); err != nil {
		return C.gpgme_error(C.GPG_ERR_USER_1)
	}
	return 0
}

//export gogpgme_assuan_status_callback
func gogpgme_assuan_status_callback(handle unsafe.Pointer, cStatus *C.char, cArgs *C.char) C.gpgme_error_t {
	status := C.GoString(cStatus)
	args := C.GoString(cArgs)
	h := *(*cgo.Handle)(handle)
	c := h.Value().(*AssuanStatusCallback)
	if *c == nil {
		return 0
	}
	if err := (*c)(status, args); err != nil {
		return C.gpgme_error(C.GPG_ERR_USER_1)
	}
	return 0
}

// ExportModeFlags defines how keys are exported from Export
type ExportModeFlags uint

const (
	ExportModeExtern  ExportModeFlags = C.GPGME_EXPORT_MODE_EXTERN
	ExportModeMinimal ExportModeFlags = C.GPGME_EXPORT_MODE_MINIMAL
)

func (c *Context) Export(pattern string, mode ExportModeFlags, data *Data) error {
	pat := C.CString(pattern)
	defer C.free(unsafe.Pointer(pat))
	err := handleError(C.gpgme_op_export(c.ctx, pat, C.gpgme_export_mode_t(mode), data.dh))
	runtime.KeepAlive(c)
	runtime.KeepAlive(data)
	return err
}

// ImportStatusFlags describes the type of ImportStatus.Status. The C API in gpgme.h simply uses "unsigned".
type ImportStatusFlags uint

const (
	ImportNew    ImportStatusFlags = C.GPGME_IMPORT_NEW
	ImportUID    ImportStatusFlags = C.GPGME_IMPORT_UID
	ImportSIG    ImportStatusFlags = C.GPGME_IMPORT_SIG
	ImportSubKey ImportStatusFlags = C.GPGME_IMPORT_SUBKEY
	ImportSecret ImportStatusFlags = C.GPGME_IMPORT_SECRET
)

type ImportStatus struct {
	Fingerprint string
	Result      error
	Status      ImportStatusFlags
}

type ImportResult struct {
	Considered      int
	NoUserID        int
	Imported        int
	ImportedRSA     int
	Unchanged       int
	NewUserIDs      int
	NewSubKeys      int
	NewSignatures   int
	NewRevocations  int
	SecretRead      int
	SecretImported  int
	SecretUnchanged int
	NotImported     int
	Imports         []ImportStatus
}

func (c *Context) Import(keyData *Data) (*ImportResult, error) {
	err := handleError(C.gpgme_op_import(c.ctx, keyData.dh))
	runtime.KeepAlive(c)
	runtime.KeepAlive(keyData)
	if err != nil {
		return nil, err
	}
	res := C.gpgme_op_import_result(c.ctx)
	runtime.KeepAlive(c)
	// NOTE: c must be live as long as we are accessing res.
	imports := []ImportStatus{}
	for s := res.imports; s != nil; s = s.next {
		imports = append(imports, ImportStatus{
			Fingerprint: C.GoString(s.fpr),
			Result:      handleError(s.result),
			Status:      ImportStatusFlags(s.status),
		})
	}
	importResult := &ImportResult{
		Considered:      int(res.considered),
		NoUserID:        int(res.no_user_id),
		Imported:        int(res.imported),
		ImportedRSA:     int(res.imported_rsa),
		Unchanged:       int(res.unchanged),
		NewUserIDs:      int(res.new_user_ids),
		NewSubKeys:      int(res.new_sub_keys),
		NewSignatures:   int(res.new_signatures),
		NewRevocations:  int(res.new_revocations),
		SecretRead:      int(res.secret_read),
		SecretImported:  int(res.secret_imported),
		SecretUnchanged: int(res.secret_unchanged),
		NotImported:     int(res.not_imported),
		Imports:         imports,
	}
	runtime.KeepAlive(c) // for all accesses to res above
	return importResult, nil
}

type Key struct {
	k C.gpgme_key_t // WARNING: Call Runtime.KeepAlive(k) after ANY passing of k.k to C
}

func newKey() *Key {
	k := &Key{}
	runtime.SetFinalizer(k, (*Key).Release)
	return k
}

func (k *Key) Release() {
	C.gpgme_key_release(k.k)
	runtime.KeepAlive(k)
	k.k = nil
}

func (k *Key) Revoked() bool {
	res := C.key_revoked(k.k) != 0
	runtime.KeepAlive(k)
	return res
}

func (k *Key) Expired() bool {
	res := C.key_expired(k.k) != 0
	runtime.KeepAlive(k)
	return res
}

func (k *Key) Disabled() bool {
	res := C.key_disabled(k.k) != 0
	runtime.KeepAlive(k)
	return res
}

func (k *Key) Invalid() bool {
	res := C.key_invalid(k.k) != 0
	runtime.KeepAlive(k)
	return res
}

func (k *Key) CanEncrypt() bool {
	res := C.key_can_encrypt(k.k) != 0
	runtime.KeepAlive(k)
	return res
}

func (k *Key) CanSign() bool {
	res := C.key_can_sign(k.k) != 0
	runtime.KeepAlive(k)
	return res
}

func (k *Key) CanCertify() bool {
	res := C.key_can_certify(k.k) != 0
	runtime.KeepAlive(k)
	return res
}

func (k *Key) Secret() bool {
	res := C.key_secret(k.k) != 0
	runtime.KeepAlive(k)
	return res
}

func (k *Key) CanAuthenticate() bool {
	res := C.key_can_authenticate(k.k) != 0
	runtime.KeepAlive(k)
	return res
}

func (k *Key) IsQualified() bool {
	res := C.key_is_qualified(k.k) != 0
	runtime.KeepAlive(k)
	return res
}

func (k *Key) Protocol() Protocol {
	res := Protocol(k.k.protocol)
	runtime.KeepAlive(k)
	return res
}

func (k *Key) IssuerSerial() string {
	res := C.GoString(k.k.issuer_serial)
	runtime.KeepAlive(k)
	return res
}

func (k *Key) IssuerName() string {
	res := C.GoString(k.k.issuer_name)
	runtime.KeepAlive(k)
	return res
}

func (k *Key) ChainID() string {
	res := C.GoString(k.k.chain_id)
	runtime.KeepAlive(k)
	return res
}

func (k *Key) OwnerTrust() Validity {
	res := Validity(k.k.owner_trust)
	runtime.KeepAlive(k)
	return res
}

func (k *Key) SubKeys() *SubKey {
	subKeys := k.k.subkeys
	runtime.KeepAlive(k)
	if subKeys == nil {
		return nil
	}
	return &SubKey{k: subKeys, parent: k} // The parent: k reference ensures subKeys remains valid
}

func (k *Key) UserIDs() *UserID {
	uids := k.k.uids
	runtime.KeepAlive(k)
	if uids == nil {
		return nil
	}
	return &UserID{u: uids, parent: k} // The parent: k reference ensures uids remains valid
}

func (k *Key) KeyListMode() KeyListMode {
	res := KeyListMode(k.k.keylist_mode)
	runtime.KeepAlive(k)
	return res
}

func (k *Key) Fingerprint() string {
	res := C.GoString(k.k.fpr)
	runtime.KeepAlive(k)
	return res
}

type SubKey struct {
	k      C.gpgme_subkey_t
	parent *Key // make sure the key is not released when we have a reference to a subkey
}

func (k *SubKey) Next() *SubKey {
	if k.k.next == nil {
		return nil
	}
	return &SubKey{k: k.k.next, parent: k.parent}
}

func (k *SubKey) Revoked() bool {
	return C.subkey_revoked(k.k) != 0
}

func (k *SubKey) Expired() bool {
	return C.subkey_expired(k.k) != 0
}

func (k *SubKey) Disabled() bool {
	return C.subkey_disabled(k.k) != 0
}

func (k *SubKey) Invalid() bool {
	return C.subkey_invalid(k.k) != 0
}

func (k *SubKey) Secret() bool {
	return C.subkey_secret(k.k) != 0
}

func (k *SubKey) KeyID() string {
	return C.GoString(k.k.keyid)
}

func (k *SubKey) Fingerprint() string {
	return C.GoString(k.k.fpr)
}

func (k *SubKey) Created() time.Time {
	if k.k.timestamp <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(k.k.timestamp), 0)
}

func (k *SubKey) Expires() time.Time {
	if k.k.expires <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(k.k.expires), 0)
}

func (k *SubKey) CardNumber() string {
	return C.GoString(k.k.card_number)
}

type UserID struct {
	u      C.gpgme_user_id_t
	parent *Key // make sure the key is not released when we have a reference to a user ID
}

func (u *UserID) Next() *UserID {
	if u.u.next == nil {
		return nil
	}
	return &UserID{u: u.u.next, parent: u.parent}
}

func (u *UserID) Revoked() bool {
	return C.uid_revoked(u.u) != 0
}

func (u *UserID) Invalid() bool {
	return C.uid_invalid(u.u) != 0
}

func (u *UserID) Validity() Validity {
	return Validity(u.u.validity)
}

func (u *UserID) UID() string {
	return C.GoString(u.u.uid)
}

func (u *UserID) Name() string {
	return C.GoString(u.u.name)
}

func (u *UserID) Comment() string {
	return C.GoString(u.u.comment)
}

func (u *UserID) Email() string {
	return C.GoString(u.u.email)
}
