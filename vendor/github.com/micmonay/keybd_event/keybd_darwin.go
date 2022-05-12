package keybd_event

/*
 #cgo CFLAGS: -x objective-c
 #cgo LDFLAGS: -framework Cocoa
 #import <Foundation/Foundation.h>
 CGEventRef CreateDown(int k){
	CGEventRef event = CGEventCreateKeyboardEvent (NULL, (CGKeyCode)k, true);
	return event;
 }
 CGEventRef CreateUp(int k){
	CGEventRef event = CGEventCreateKeyboardEvent (NULL, (CGKeyCode)k, false);
	return event;
 }
 void KeyTap(CGEventRef event){
	CGEventPost(kCGAnnotatedSessionEventTap, event);
	CFRelease(event);
 }
 void AddActionKey(CGEventFlags type,CGEventRef event){
 	CGEventSetFlags(event, type);
 }
*/
import "C"
import "time"

const (
	_AShift          = C.kCGEventFlagMaskAlphaShift
	_VK_SHIFT        = C.kCGEventFlagMaskShift
	_VK_CTRL         = C.kCGEventFlagMaskControl
	_VK_ALT          = C.kCGEventFlagMaskAlternate
	_VK_CMD          = C.kCGEventFlagMaskCommand
	_Help            = C.kCGEventFlagMaskHelp
	_VK_FN           = C.kCGEventFlagMaskSecondaryFn
	_NumPad          = C.kCGEventFlagMaskNumericPad
	_Coalesced       = C.kCGEventFlagMaskNonCoalesced
	_VK_Control      = 0x3B
	_VK_RightShift   = 0x3C
	_VK_RightControl = 0x3E
	_VK_Command      = 0x37
	_VK_Shift        = 0x38
)

func initKeyBD() error { return nil }

// Press key(s)
func (k *KeyBonding) Press() error {
	for _, key := range k.keys {
		k.keyPress(key)
	}
	return nil
}

// Release key(s)
func (k *KeyBonding) Release() error {
	for _, key := range k.keys {
		k.keyRelease(key)
	}
	return nil
}

// Launch key bounding
func (k *KeyBonding) Launching() error {

	for _, key := range k.keys {
		k.tapKey(key)
	}
	return nil
}
func altgr(event C.CGEventRef) {
	alt(event)
}
func shift(event C.CGEventRef) {
	C.AddActionKey(_VK_SHIFT, event)
}
func ctrl(event C.CGEventRef) {
	C.AddActionKey(_VK_CTRL, event)
}
func alt(event C.CGEventRef) {
	C.AddActionKey(_VK_ALT, event)
}
func cmd(event C.CGEventRef) {
	C.AddActionKey(_VK_CMD, event)
}
func (k KeyBonding) keyPress(key int) {
	downEvent := C.CreateDown(C.int(key))
	if k.hasALT {
		alt(downEvent)
	}
	if k.hasCTRL {
		ctrl(downEvent)
	}
	if k.hasSHIFT {
		shift(downEvent)
	}
	if k.hasRCTRL { //not support on mac
		ctrl(downEvent)
	}
	if k.hasRSHIFT { //not support on mac
		shift(downEvent)
	}
	if k.hasALTGR {
		altgr(downEvent)
	}
	if k.hasSuper {
		cmd(downEvent)
	}
	C.KeyTap(downEvent)
}
func (k KeyBonding) keyRelease(key int) {
	upEvent := C.CreateUp(C.int(key))
	if k.hasALT {
		alt(upEvent)
	}
	if k.hasCTRL {
		ctrl(upEvent)
	}
	if k.hasSHIFT {
		shift(upEvent)
	}
	if k.hasRCTRL { //not support on mac
		ctrl(upEvent)
	}
	if k.hasRSHIFT { //not support on mac
		shift(upEvent)
	}
	if k.hasALTGR {
		altgr(upEvent)
	}
	if k.hasSuper {
		cmd(upEvent)
	}
	C.KeyTap(upEvent)
}
func (k KeyBonding) tapKey(key int) {
	k.keyPress(key)
	time.Sleep(100 * time.Millisecond) //ignore if speed is most in my test system
	k.keyRelease(key)
}

const (
	VK_SP1  = 0x0A
	VK_SP2  = 0x1B
	VK_SP3  = 0x18
	VK_SP4  = 0x21
	VK_SP5  = 0x1E
	VK_SP6  = 0x29
	VK_SP7  = 0x27
	VK_SP8  = 0x2A
	VK_SP9  = 0x2B
	VK_SP10 = 0x2F
	VK_SP11 = 0x2C
	VK_SP12 = 0x32

	VK_A              = 0x00
	VK_S              = 0x01
	VK_D              = 0x02
	VK_F              = 0x03
	VK_H              = 0x04
	VK_G              = 0x05
	VK_Z              = 0x06
	VK_X              = 0x07
	VK_C              = 0x08
	VK_V              = 0x09
	VK_B              = 0x0B
	VK_Q              = 0x0C
	VK_W              = 0x0D
	VK_E              = 0x0E
	VK_R              = 0x0F
	VK_Y              = 0x10
	VK_T              = 0x11
	VK_1              = 0x12
	VK_2              = 0x13
	VK_3              = 0x14
	VK_4              = 0x15
	VK_6              = 0x16
	VK_5              = 0x17
	VK_EQUAL          = 0x18
	VK_9              = 0x19
	VK_7              = 0x1A
	VK_MINUS          = 0x1B
	VK_8              = 0x1C
	VK_0              = 0x1D
	VK_RightBracket   = 0x1E
	VK_O              = 0x1F
	VK_U              = 0x20
	VK_LeftBracket    = 0x21
	VK_I              = 0x22
	VK_P              = 0x23
	VK_L              = 0x25
	VK_J              = 0x26
	VK_Quote          = 0x27
	VK_K              = 0x28
	VK_SEMICOLON      = 0x29
	VK_BACKSLASH      = 0x2A
	VK_COMMA          = 0x2B
	VK_SLASH          = 0x2C
	VK_N              = 0x2D
	VK_M              = 0x2E
	VK_Period         = 0x2F
	VK_GRAVE          = 0x32
	VK_KeypadDecimal  = 0x41
	VK_KeypadMultiply = 0x43
	VK_KeypadPlus     = 0x45
	VK_KeypadClear    = 0x47
	VK_KeypadDivide   = 0x4B
	VK_KeypadEnter    = 0x4C
	VK_KeypadMinus    = 0x4E
	VK_KeypadEquals   = 0x51
	VK_Keypad0        = 0x52
	VK_Keypad1        = 0x53
	VK_Keypad2        = 0x54
	VK_Keypad3        = 0x55
	VK_Keypad4        = 0x56
	VK_Keypad5        = 0x57
	VK_Keypad6        = 0x58
	VK_Keypad7        = 0x59
	VK_Keypad8        = 0x5B
	VK_Keypad9        = 0x5C

	VK_ENTER         = 0x24
	VK_TAB           = 0x30
	VK_SPACE         = 0x31
	VK_DELETE        = 0x33
	VK_ESC           = 0x35
	VK_CAPSLOCK      = 0x39
	VK_Option        = 0x3A
	VK_RightOption   = 0x3D
	VK_Function      = 0x3F
	VK_F17           = 0x40
	VK_VOLUMEUP      = 0x48
	VK_VOLUMEDOWN    = 0x49
	VK_MUTE          = 0x4A
	VK_F18           = 0x4F
	VK_F19           = 0x50
	VK_F20           = 0x5A
	VK_F5            = 0x60
	VK_F6            = 0x61
	VK_F7            = 0x62
	VK_F3            = 0x63
	VK_F8            = 0x64
	VK_F9            = 0x65
	VK_F11           = 0x67
	VK_F13           = 0x69
	VK_F16           = 0x6A
	VK_F14           = 0x6B
	VK_F10           = 0x6D
	VK_F12           = 0x6F
	VK_F15           = 0x71
	VK_HELP          = 0x72
	VK_HOME          = 0x73
	VK_PAGEUP        = 0x74
	VK_ForwardDelete = 0x75
	VK_F4            = 0x76
	VK_END           = 0x77
	VK_F2            = 0x78
	VK_PAGEDOWN      = 0x79
	VK_F1            = 0x7A
	VK_LEFT          = 0x7B
	VK_RIGHT         = 0x7C
	VK_DOWN          = 0x7D
	VK_UP            = 0x7E
)
