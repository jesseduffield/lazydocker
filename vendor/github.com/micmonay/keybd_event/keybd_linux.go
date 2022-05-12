package keybd_event

/*
 #include <linux/uinput.h>
*/
import "C"
import (
	"encoding/binary"
	"errors"
	"os"
	"syscall"
)

type uinput_user_dev C.struct_uinput_user_dev
type timeval C.struct_timeval
type input_event C.struct_input_event

var fd *os.File
var X11 = true

const (
	_EV_KEY               = C.EV_KEY
	_EV_SYN               = C.EV_SYN
	_UI_SET_EVBIT         = C.UI_SET_EVBIT
	_UI_SET_KEYBIT        = C.UI_SET_KEYBIT
	_UI_DEV_CREATE        = C.UI_DEV_CREATE
	_UI_DEV_DESTROY       = C.UI_DEV_DESTROY
	_UINPUT_MAX_NAME_SIZE = C.UINPUT_MAX_NAME_SIZE
	_VK_LEFTCTRL          = 29
	_VK_RIGHTCTRL         = 97
	_VK_CTRL              = 29
	_VK_LEFTSHIFT         = 42
	_VK_RIGHTSHIFT        = 54
	_VK_SHIFT             = 42
	_VK_LEFTALT           = 56
	_VK_RIGHTALT          = 100
	_VK_ALT               = 56
	_VK_LEFTMETA          = 125
	_VK_RIGHTMETA         = 126
)

func initKeyBD() error {
	if fd != nil {
		return nil
	}
	path, err := getFileUInput()
	if err != nil {
		return err
	}
	fdInit, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, os.ModeDevice)
	fd = fdInit
	if err != nil {
		if os.IsPermission(err) {
			return errors.New("permission error for " + path + " try cmd : sudo chmod +0666 " + path)
		} else {
			return err
		}
	}
	_, _, errCall := syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), _UI_SET_EVBIT, uintptr(_EV_KEY))
	if errCall != 0 {
		return err
	}
	_, _, errCall = syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), _UI_SET_EVBIT, uintptr(_EV_SYN))
	if errCall != 0 {
		return err
	}
	keyEventSet()
	uidev := uinput_user_dev{}
	for i, c := range "keybd interface" {
		uidev.name[i] = C.char(c)
	}
	uidev.id.bustype = C.BUS_USB
	uidev.id.vendor = 0x1
	uidev.id.product = 0x1
	uidev.id.version = 0x1
	err = binary.Write(fd, binary.LittleEndian, &uidev)
	if err != nil {
		return err
	}
	//Create device
	_, _, errCall = syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), _UI_DEV_CREATE, 0)
	if errCall != 0 {
		return err
	}
	return nil
}

//actualy don't use
func destroyLinuxUInput() {
	syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), _UI_DEV_DESTROY, 0)
	fd.Close()
}
func getFileUInput() (string, error) {
	if _, err := os.Stat("/dev/uinput"); err == nil {
		return "/dev/uinput", nil
	}
	if _, err := os.Stat("/dev/input/uinput"); err == nil {
		return "/dev/input/uinput", nil
	}
	err := errors.New("Not found uinput file. Try this cmd 'sudo modprobe uinput'")
	return "", err
}

// Press key(s)
func (k *KeyBonding) Press() error {
	var err error
	if k.hasALT {
		err = downKey(_VK_ALT)
		if err != nil {
			return err
		}
	}
	if k.hasSHIFT {
		err = downKey(_VK_SHIFT)
		if err != nil {
			return err
		}
	}
	if k.hasCTRL {
		err = downKey(_VK_CTRL)
		if err != nil {
			return err
		}
	}
	if k.hasRSHIFT {
		err = downKey(_VK_RIGHTSHIFT)
		if err != nil {
			return err
		}
	}
	if k.hasRCTRL {
		err = downKey(_VK_RIGHTCTRL)
		if err != nil {
			return err
		}
	}
	if k.hasALTGR {
		err = downKey(_VK_RIGHTALT)
		if err != nil {
			return err
		}
	}
	if k.hasSuper {
		err = downKey(_VK_LEFTMETA)
		if err != nil {
			return err
		}
	}
	for _, key := range k.keys {
		err = downKey(key)
		if err != nil {
			return err
		}
	}
	err = sync()
	if err != nil {
		return err
	}
	return nil
}

// Release key(s)
func (k *KeyBonding) Release() error {
	var err error
	if k.hasALT {
		err = upKey(_VK_ALT)
		if err != nil {
			return err
		}
	}
	if k.hasSHIFT {
		err = upKey(_VK_SHIFT)
		if err != nil {
			return err
		}
	}
	if k.hasCTRL {
		err = upKey(_VK_CTRL)
		if err != nil {
			return err
		}
	}
	if k.hasRSHIFT {
		err = upKey(_VK_RIGHTSHIFT)
		if err != nil {
			return err
		}
	}
	if k.hasRCTRL {
		err = upKey(_VK_RIGHTCTRL)
		if err != nil {
			return err
		}
	}
	if k.hasALTGR {
		err = upKey(_VK_RIGHTALT)
		if err != nil {
			return err
		}
	}
	if k.hasSuper {
		err := upKey(_VK_LEFTMETA)
		if err != nil {
			return err
		}
	}
	for _, key := range k.keys {
		err = upKey(key)
		if err != nil {
			return err
		}
	}
	err = sync()
	if err != nil {
		return err
	}
	//Destroy device

	return nil
}

// Launch key bounding
func (k *KeyBonding) Launching() error {
	k.Press()
	//key up
	k.Release()
	return nil
}
func keyEventSet() error {
	for i := 0; i < 256; i++ {
		_, _, errCall := syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), _UI_SET_KEYBIT, uintptr(i))
		if errCall != 0 {
			return errCall
		}
	}
	return nil
}
func downKey(key int) error {
	ev := input_event{}
	ev._type = _EV_KEY
	ev.code = C.__u16(key)
	ev.value = 1
	err := binary.Write(fd, binary.LittleEndian, &ev)
	if err != nil {
		return err
	}
	return nil
}
func sync() error {
	ev := input_event{}
	ev._type = _EV_SYN
	ev.code = 0
	ev.value = 0
	err := binary.Write(fd, binary.LittleEndian, &ev)
	if err != nil {
		return err
	}
	return nil
}
func upKey(key int) error {
	ev := input_event{}
	ev._type = _EV_KEY
	ev.code = C.__u16(key)
	ev.value = 0
	err := binary.Write(fd, binary.LittleEndian, &ev)
	if err != nil {
		return err
	}
	return nil
}

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

	VK_UP    = 103
	VK_DOWN  = 108
	VK_LEFT  = 105
	VK_RIGHT = 106

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
	VK_SPACE      = 57
	VK_CAPSLOCK   = 58

	VK_KP0         = 82
	VK_KP1         = 79
	VK_KP2         = 80
	VK_KP3         = 81
	VK_KP4         = 75
	VK_KP5         = 76
	VK_KP6         = 77
	VK_KP7         = 71
	VK_KP8         = 72
	VK_KP9         = 73
	VK_KPMINUS     = 74
	VK_KPPLUS      = 78
	VK_KPDOT       = 83
	VK_KPJPCOMMA   = 95
	VK_KPENTER     = 96
	VK_KPSLASH     = 98
	VK_KPASTERISK  = 55
	VK_KPEQUAL     = 117
	VK_KPPLUSMINUS = 118
	VK_KPCOMMA     = 121

	VK_ZENKAKUHANKAKU   = 85
	VK_102ND            = 86
	VK_RO               = 89
	VK_KATAKANA         = 90
	VK_HIRAGANA         = 91
	VK_HENKAN           = 92
	VK_KATAKANAHIRAGANA = 93
	VK_MUHENKAN         = 94
	VK_SYSRQ            = 99
	VK_LINEFEED         = 101
	VK_HOME             = 102
	VK_PAGEUP           = 104
	VK_END              = 107
	VK_PAGEDOWN         = 109
	VK_INSERT           = 110
	VK_DELETE           = 111
	VK_MACRO            = 112
	VK_MUTE             = 113
	VK_VOLUMEDOWN       = 114
	VK_VOLUMEUP         = 115
	VK_POWER            = 116 /* SC System Power Down */
	VK_PAUSE            = 119
	VK_SCALE            = 120 /* AL Compiz Scale (Expose) */

	VK_HANGEUL   = 122
	VK_HANGUEL   = VK_HANGEUL
	VK_HANJA     = 123
	VK_YEN       = 124
	VK_LEFTMETA  = 125
	VK_RIGHTMETA = 126
	VK_COMPOSE   = 127

	VK_STOP           = 128 /* AC Stop */
	VK_AGAIN          = 129
	VK_PROPS          = 130 /* AC Properties */
	VK_UNDO           = 131 /* AC Undo */
	VK_FRONT          = 132
	VK_COPY           = 133 /* AC Copy */
	VK_OPEN           = 134 /* AC Open */
	VK_PASTE          = 135 /* AC Paste */
	VK_FIND           = 136 /* AC Search */
	VK_CUT            = 137 /* AC Cut */
	VK_HELP           = 138 /* AL Integrated Help Center */
	VK_MENU           = 139 /* Menu (show menu) */
	VK_CALC           = 140 /* AL Calculator */
	VK_SETUP          = 141
	VK_SLEEP          = 142 /* SC System Sleep */
	VK_WAKEUP         = 143 /* System Wake Up */
	VK_FILE           = 144 /* AL Local Machine Browser */
	VK_SENDFILE       = 145
	VK_DELETEFILE     = 146
	VK_XFER           = 147
	VK_PROG1          = 148
	VK_PROG2          = 149
	VK_WWW            = 150 /* AL Internet Browser */
	VK_MSDOS          = 151
	VK_COFFEE         = 152 /* AL Terminal Lock/Screensaver */
	VK_SCREENLOCK     = VK_COFFEE
	VK_ROTATE_DISPLAY = 153 /* Display orientation for e.g. tablets */
	VK_DIRECTION      = VK_ROTATE_DISPLAY
	VK_CYCLEWINDOWS   = 154
	VK_MAIL           = 155
	VK_BOOKMARKS      = 156 /* AC Bookmarks */
	VK_COMPUTER       = 157
	VK_BACK           = 158 /* AC Back */
	VK_FORWARD        = 159 /* AC Forward */
	VK_CLOSECD        = 160
	VK_EJECTCD        = 161
	VK_EJECTCLOSECD   = 162
	VK_NEXTSONG       = 163
	VK_PLAYPAUSE      = 164
	VK_PREVIOUSSONG   = 165
	VK_STOPCD         = 166
	VK_RECORD         = 167
	VK_REWIND         = 168
	VK_PHONE          = 169 /* Media Select Telephone */
	VK_ISO            = 170
	VK_CONFIG         = 171 /* AL Consumer Control Configuration */
	VK_HOMEPAGE       = 172 /* AC Home */
	VK_REFRESH        = 173 /* AC Refresh */
	VK_EXIT           = 174 /* AC Exit */
	VK_MOVE           = 175
	VK_EDIT           = 176
	VK_SCROLLUP       = 177
	VK_SCROLLDOWN     = 178
	VK_KPLEFTPAREN    = 179
	VK_KPRIGHTPAREN   = 180
	VK_NEW            = 181 /* AC New */
	VK_REDO           = 182 /* AC Redo/Repeat */

	VK_F13 = 183
	VK_F14 = 184
	VK_F15 = 185
	VK_F16 = 186
	VK_F17 = 187
	VK_F18 = 188
	VK_F19 = 189
	VK_F20 = 190
	VK_F21 = 191
	VK_F22 = 192
	VK_F23 = 193
	VK_F24 = 194

	VK_PLAYCD         = 200
	VK_PAUSECD        = 201
	VK_PROG3          = 202
	VK_PROG4          = 203
	VK_DASHBOARD      = 204 /* AL Dashboard */
	VK_SUSPEND        = 205
	VK_CLOSE          = 206 /* AC Close */
	VK_PLAY           = 207
	VK_FASTFORWARD    = 208
	VK_BASSBOOST      = 209
	VK_PRINT          = 210 /* AC Print */
	VK_HP             = 211
	VK_CAMERA         = 212
	VK_SOUND          = 213
	VK_QUESTION       = 214
	VK_EMAIL          = 215
	VK_CHAT           = 216
	VK_SEARCH         = 217
	VK_CONNECT        = 218
	VK_FINANCE        = 219 /* AL Checkbook/Finance */
	VK_SPORT          = 220
	VK_SHOP           = 221
	VK_ALTERASE       = 222
	VK_CANCEL         = 223 /* AC Cancel */
	VK_BRIGHTNESSDOWN = 224
	VK_BRIGHTNESSUP   = 225
	VK_MEDIA          = 226

	VK_SWITCHVIDEOMODE = 227 /* Cycle between available video
	   outputs (Monitor/LCD/TV-out/etc) */
	VK_KBDILLUMTOGGLE = 228
	VK_KBDILLUMDOWN   = 229
	VK_KBDILLUMUP     = 230

	VK_SEND        = 231 /* AC Send */
	VK_REPLY       = 232 /* AC Reply */
	VK_FORWARDMAIL = 233 /* AC Forward Msg */
	VK_SAVE        = 234 /* AC Save */
	VK_DOCUMENTS   = 235

	VK_BATTERY = 236

	VK_BLUETOOTH = 237
	VK_WLAN      = 238
	VK_UWB       = 239

	VK_UNKNOWN = 240

	VK_VIDEO_NEXT       = 241 /* drive next video source */
	VK_VIDEO_PREV       = 242 /* drive previous video source */
	VK_BRIGHTNESS_CYCLE = 243 /* brightness up, after max is min */
	VK_BRIGHTNESS_AUTO  = 244 /* Set Auto Brightness: manual
	brightness control is off,
	rely on ambient */
	VK_BRIGHTNESS_ZERO = VK_BRIGHTNESS_AUTO
	VK_DISPLAY_OFF     = 245 /* display device to off state */

	VK_WWAN   = 246 /* Wireless WAN (LTE, UMTS, GSM, etc.) */
	VK_WIMAX  = VK_WWAN
	VK_RFKILL = 247 /* Key that controls all radios */

	VK_MICMUTE = 248 /* Mute / unmute the microphone */
)

//http://thiemonge.org/getting-started-with-uinput
