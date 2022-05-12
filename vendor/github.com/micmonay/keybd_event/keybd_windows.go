package keybd_event

import (
	"syscall"
)

var dll = syscall.NewLazyDLL("user32.dll")
var procKeyBd = dll.NewProc("keybd_event")

// Press key(s)
func (k *KeyBonding) Press() error {
	if k.hasALT {
		downKey(_VK_ALT)
	}
	if k.hasALTGR {
		downKey(_VK_ALT)
		downKey(_VK_CTRL)
	}
	if k.hasSHIFT {
		downKey(_VK_SHIFT)
	}
	if k.hasCTRL {
		downKey(_VK_CTRL)
	}
	if k.hasRSHIFT {
		downKey(_VK_RSHIFT)
	}
	if k.hasRCTRL {
		downKey(_VK_RCONTROL)
	}
	if k.hasSuper {
		downKey(_VK_LWIN)
	}
	for _, key := range k.keys {
		downKey(key)
	}
	return nil
}

//Release key(s)
func (k *KeyBonding) Release() error {
	if k.hasALT {
		upKey(_VK_ALT)
	}
	if k.hasALTGR {
		upKey(_VK_ALT)
		upKey(_VK_CTRL)
	}
	if k.hasSHIFT {
		upKey(_VK_SHIFT)
	}
	if k.hasCTRL {
		upKey(_VK_CTRL)
	}
	if k.hasRSHIFT {
		upKey(_VK_RSHIFT)
	}
	if k.hasRCTRL {
		upKey(_VK_RCONTROL)
	}
	for _, key := range k.keys {
		upKey(key)
	}
	if k.hasSuper {
		upKey(_VK_LWIN)
	}
	return nil
}

// Launch key bounding
func (k *KeyBonding) Launching() error {
	err := k.Press()
	if err != nil {
		return err
	}
	err = k.Release()
	return err
}
func downKey(key int) {
	flag := 0
	if key < 0xFFF { // Detect if the key code is virtual or no
		flag |= _KEYEVENTF_SCANCODE
	} else {
		key -= 0xFFF
	}
	vkey := key + 0x80
	procKeyBd.Call(uintptr(key), uintptr(vkey), uintptr(flag), 0)
}
func upKey(key int) {
	flag := _KEYEVENTF_KEYUP
	if key < 0xFFF {
		flag |= _KEYEVENTF_SCANCODE
	} else {
		key -= 0xFFF
	}
	vkey := key + 0x80
	procKeyBd.Call(uintptr(key), uintptr(vkey), uintptr(flag), 0)
}
func initKeyBD() error { return nil }

const (
	// I add 0xFFF for all Virtual key
	_VK_SHIFT           = 0x10 + 0xFFF
	_VK_CTRL            = 0x11 + 0xFFF
	_VK_ALT             = 0x12 + 0xFFF
	_VK_LSHIFT          = 0xA0 + 0xFFF
	_VK_RSHIFT          = 0xA1 + 0xFFF
	_VK_LCONTROL        = 0xA2 + 0xFFF
	_VK_RCONTROL        = 0xA3 + 0xFFF
	_VK_LWIN            = 0x5B + 0xFFF
	_VK_RWIN            = 0x5C + 0xFFF
	_KEYEVENTF_KEYUP    = 0x0002
	_KEYEVENTF_SCANCODE = 0x0008
)
const (
	VK_SP1  = 41
	VK_SP2  = 12
	VK_SP3  = 13
	VK_SP4  = 26
	VK_SP5  = 27
	VK_SP6  = 39
	VK_SP7  = 40
	VK_SP8  = 43
	VK_SP9  = 51
	VK_SP10 = 52
	VK_SP11 = 53
	VK_SP12 = 86

	VK_ESC = 1
	VK_1   = 2
	VK_2   = 3
	VK_3   = 4
	VK_4   = 5
	VK_5   = 6
	VK_6   = 7
	VK_7   = 8
	VK_8   = 9
	VK_9   = 10
	VK_0   = 11
	VK_Q   = 16
	VK_W   = 17
	VK_E   = 18
	VK_R   = 19
	VK_T   = 20
	VK_Y   = 21
	VK_U   = 22
	VK_I   = 23
	VK_O   = 24
	VK_P   = 25
	VK_A   = 30
	VK_S   = 31
	VK_D   = 32
	VK_F   = 33
	VK_G   = 34
	VK_H   = 35
	VK_J   = 36
	VK_K   = 37
	VK_L   = 38
	VK_Z   = 44
	VK_X   = 45
	VK_C   = 46
	VK_V   = 47
	VK_B   = 48
	VK_N   = 49
	VK_M   = 50
	VK_F1  = 59
	VK_F2  = 60
	VK_F3  = 61
	VK_F4  = 62
	VK_F5  = 63
	VK_F6  = 64
	VK_F7  = 65
	VK_F8  = 66
	VK_F9  = 67
	VK_F10 = 68
	VK_F11 = 87
	VK_F12 = 88

	VK_F13 = 0x7C + 0xFFF
	VK_F14 = 0x7D + 0xFFF
	VK_F15 = 0x7E + 0xFFF
	VK_F16 = 0x7F + 0xFFF
	VK_F17 = 0x80 + 0xFFF
	VK_F18 = 0x81 + 0xFFF
	VK_F19 = 0x82 + 0xFFF
	VK_F20 = 0x83 + 0xFFF
	VK_F21 = 0x84 + 0xFFF
	VK_F22 = 0x85 + 0xFFF
	VK_F23 = 0x86 + 0xFFF
	VK_F24 = 0x87 + 0xFFF
	
	VK_NUMLOCK    = 69
	VK_SCROLLLOCK = 70
	VK_RESERVED   = 0
	VK_MINUS      = 12
	VK_EQUAL      = 13
	VK_BACKSPACE  = 14
	VK_TAB        = 15
	VK_LEFTBRACE  = 26
	VK_RIGHTBRACE = 27
	VK_ENTER      = 28
	VK_SEMICOLON  = 39
	VK_APOSTROPHE = 40
	VK_GRAVE      = 41
	VK_BACKSLASH  = 43
	VK_COMMA      = 51
	VK_DOT        = 52
	VK_SLASH      = 53
	VK_KPASTERISK = 55
	VK_SPACE      = 57
	VK_CAPSLOCK   = 58

	VK_KP0     = 82
	VK_KP1     = 79
	VK_KP2     = 80
	VK_KP3     = 81
	VK_KP4     = 75
	VK_KP5     = 76
	VK_KP6     = 77
	VK_KP7     = 71
	VK_KP8     = 72
	VK_KP9     = 73
	VK_KPMINUS = 74
	VK_KPPLUS  = 78
	VK_KPDOT   = 83

	// I add 0xFFF for all Virtual key
	VK_LBUTTON    = 0x01 + 0xFFF
	VK_RBUTTON    = 0x02 + 0xFFF
	VK_CANCEL     = 0x03 + 0xFFF
	VK_MBUTTON    = 0x04 + 0xFFF
	VK_XBUTTON1   = 0x05 + 0xFFF
	VK_XBUTTON2   = 0x06 + 0xFFF
	VK_BACK       = 0x08 + 0xFFF
	VK_CLEAR      = 0x0C + 0xFFF
	VK_PAUSE      = 0x13 + 0xFFF
	VK_CAPITAL    = 0x14 + 0xFFF
	VK_KANA       = 0x15 + 0xFFF
	VK_HANGUEL    = 0x15 + 0xFFF
	VK_HANGUL     = 0x15 + 0xFFF
	VK_JUNJA      = 0x17 + 0xFFF
	VK_FINAL      = 0x18 + 0xFFF
	VK_HANJA      = 0x19 + 0xFFF
	VK_KANJI      = 0x19 + 0xFFF
	VK_CONVERT    = 0x1C + 0xFFF
	VK_NONCONVERT = 0x1D + 0xFFF
	VK_ACCEPT     = 0x1E + 0xFFF
	VK_MODECHANGE = 0x1F + 0xFFF
	VK_PAGEUP     = 0x21 + 0xFFF
	VK_PAGEDOWN   = 0x22 + 0xFFF
	VK_END        = 0x23 + 0xFFF
	VK_HOME       = 0x24 + 0xFFF
	VK_LEFT       = 0x25 + 0xFFF
	VK_UP         = 0x26 + 0xFFF
	VK_RIGHT      = 0x27 + 0xFFF
	VK_DOWN       = 0x28 + 0xFFF
	VK_SELECT     = 0x29 + 0xFFF
	VK_PRINT      = 0x2A + 0xFFF
	VK_EXECUTE    = 0x2B + 0xFFF
	VK_SNAPSHOT   = 0x2C + 0xFFF
	VK_INSERT     = 0x2D + 0xFFF
	VK_DELETE     = 0x2E + 0xFFF
	VK_HELP       = 0x2F + 0xFFF

	VK_SCROLL              = 0x91 + 0xFFF
	VK_LMENU               = 0xA4 + 0xFFF
	VK_RMENU               = 0xA5 + 0xFFF
	VK_BROWSER_BACK        = 0xA6 + 0xFFF
	VK_BROWSER_FORWARD     = 0xA7 + 0xFFF
	VK_BROWSER_REFRESH     = 0xA8 + 0xFFF
	VK_BROWSER_STOP        = 0xA9 + 0xFFF
	VK_BROWSER_SEARCH      = 0xAA + 0xFFF
	VK_BROWSER_FAVORITES   = 0xAB + 0xFFF
	VK_BROWSER_HOME        = 0xAC + 0xFFF
	VK_VOLUME_MUTE         = 0xAD + 0xFFF
	VK_VOLUME_DOWN         = 0xAE + 0xFFF
	VK_VOLUME_UP           = 0xAF + 0xFFF
	VK_MEDIA_NEXT_TRACK    = 0xB0 + 0xFFF
	VK_MEDIA_PREV_TRACK    = 0xB1 + 0xFFF
	VK_MEDIA_STOP          = 0xB2 + 0xFFF
	VK_MEDIA_PLAY_PAUSE    = 0xB3 + 0xFFF
	VK_LAUNCH_MAIL         = 0xB4 + 0xFFF
	VK_LAUNCH_MEDIA_SELECT = 0xB5 + 0xFFF
	VK_LAUNCH_APP1         = 0xB6 + 0xFFF
	VK_LAUNCH_APP2         = 0xB7 + 0xFFF
	VK_OEM_1               = 0xBA + 0xFFF
	VK_OEM_PLUS            = 0xBB + 0xFFF
	VK_OEM_COMMA           = 0xBC + 0xFFF
	VK_OEM_MINUS           = 0xBD + 0xFFF
	VK_OEM_PERIOD          = 0xBE + 0xFFF
	VK_OEM_2               = 0xBF + 0xFFF
	VK_OEM_3               = 0xC0 + 0xFFF
	VK_OEM_4               = 0xDB + 0xFFF
	VK_OEM_5               = 0xDC + 0xFFF
	VK_OEM_6               = 0xDD + 0xFFF
	VK_OEM_7               = 0xDE + 0xFFF
	VK_OEM_8               = 0xDF + 0xFFF
	VK_OEM_102             = 0xE2 + 0xFFF
	VK_PROCESSKEY          = 0xE5 + 0xFFF
	VK_PACKET              = 0xE7 + 0xFFF
	VK_ATTN                = 0xF6 + 0xFFF
	VK_CRSEL               = 0xF7 + 0xFFF
	VK_EXSEL               = 0xF8 + 0xFFF
	VK_EREOF               = 0xF9 + 0xFFF
	VK_PLAY                = 0xFA + 0xFFF
	VK_ZOOM                = 0xFB + 0xFFF
	VK_NONAME              = 0xFC + 0xFFF
	VK_PA1                 = 0xFD + 0xFFF
	VK_OEM_CLEAR           = 0xFE + 0xFFF
)
