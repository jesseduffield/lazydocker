package luksy

type V2JSON struct {
	Config   V2JSONConfig             `json:"config"`
	Keyslots map[string]V2JSONKeyslot `json:"keyslots"`
	Digests  map[string]V2JSONDigest  `json:"digests"`
	Segments map[string]V2JSONSegment `json:"segments"`
	Tokens   map[string]V2JSONToken   `json:"tokens"`
}

type V2JSONKeyslotPriority int

func (p V2JSONKeyslotPriority) String() string {
	switch p {
	case V2JSONKeyslotPriorityIgnore:
		return "ignore"
	case V2JSONKeyslotPriorityNormal:
		return "normal"
	case V2JSONKeyslotPriorityHigh:
		return "high"
	}
	return "unknown"
}

const (
	V2JSONKeyslotPriorityIgnore = V2JSONKeyslotPriority(0)
	V2JSONKeyslotPriorityNormal = V2JSONKeyslotPriority(1)
	V2JSONKeyslotPriorityHigh   = V2JSONKeyslotPriority(2)
)

type V2JSONKeyslot struct {
	Type                    string                 `json:"type"`
	KeySize                 int                    `json:"key_size"`
	Area                    V2JSONArea             `json:"area"`
	Priority                *V2JSONKeyslotPriority `json:"priority,omitempty"`
	*V2JSONKeyslotLUKS2                            // type = "luks2"
	*V2JSONKeyslotReencrypt                        // type = "reencrypt"
}

type V2JSONKeyslotLUKS2 struct {
	AF  V2JSONAF  `json:"af"`
	Kdf V2JSONKdf `json:"kdf"`
}

type V2JSONKeyslotReencrypt struct {
	Mode      string `json:"mode"`      // only "reencrypt", "encrypt", "decrypt"
	Direction string `json:"direction"` // only "forward", "backward"
}

type V2JSONArea struct {
	Type                         string `json:"type"` // only "raw", "none", "journal", "checksum", "datashift", "datashift-journal", "datashift-checksum"
	Offset                       int64  `json:"offset,string"`
	Size                         int64  `json:"size,string"`
	*V2JSONAreaRaw                      // type = "raw"
	*V2JSONAreaChecksum                 // type = "checksum"
	*V2JSONAreaDatashift                // type = "datashift"
	*V2JSONAreaDatashiftChecksum        // type = "datashift-checksum"
}

type V2JSONAreaRaw struct {
	Encryption string `json:"encryption"`
	KeySize    int    `json:"key_size"`
}

type V2JSONAreaChecksum struct {
	Hash       string `json:"hash"`
	SectorSize int    `json:"sector_size"`
}

type V2JSONAreaDatashift struct {
	ShiftSize int `json:"shift_size,string"`
}

type V2JSONAreaDatashiftChecksum struct {
	V2JSONAreaChecksum
	V2JSONAreaDatashift
}

type V2JSONAF struct {
	Type           string `json:"type"` // "luks1"
	*V2JSONAFLUKS1        // type == "luks1"
}

type V2JSONAFLUKS1 struct {
	Stripes int    `json:"stripes"` // 4000
	Hash    string `json:"hash"`    // "sha256"
}

type V2JSONKdf struct {
	Type              string `json:"type"`
	Salt              []byte `json:"salt"`
	*V2JSONKdfPbkdf2         // type = "pbkdf2"
	*V2JSONKdfArgon2i        // type = "argon2i" or type = "argon2id"
}

type V2JSONKdfPbkdf2 struct {
	Hash       string `json:"hash"`
	Iterations int    `json:"iterations"`
}

type V2JSONKdfArgon2i struct {
	Time   int `json:"time"`
	Memory int `json:"memory"`
	CPUs   int `json:"cpus"`
}

type V2JSONSegment struct {
	Type                string              `json:"type"` // only "linear", "crypt"
	Offset              string              `json:"offset"`
	Size                string              `json:"size"` // numeric value or "dynamic"
	Flags               []string            `json:"flags,omitempty"`
	*V2JSONSegmentCrypt `json:",omitempty"` // type = "crypt"
}

type V2JSONSegmentCrypt struct {
	IVTweak    int                     `json:"iv_tweak,string"`
	Encryption string                  `json:"encryption"`
	SectorSize int                     `json:"sector_size"` // 512 or 1024 or 2048 or 4096
	Integrity  *V2JSONSegmentIntegrity `json:"integrity,omitempty"`
}

type V2JSONSegmentIntegrity struct {
	Type              string `json:"type"`
	JournalEncryption string `json:"journal_encryption"`
	JournalIntegrity  string `json:"journal_integrity"`
}

type V2JSONDigest struct {
	Type                string   `json:"type"`
	Keyslots            []string `json:"keyslots"`
	Segments            []string `json:"segments"`
	Salt                []byte   `json:"salt"`
	Digest              []byte   `json:"digest"`
	*V2JSONDigestPbkdf2          // type == "pbkdf2"
}

type V2JSONDigestPbkdf2 struct {
	Hash       string `json:"hash"`
	Iterations int    `json:"iterations"`
}

type V2JSONConfig struct {
	JsonSize     int      `json:"json_size,string"`
	KeyslotsSize int      `json:"keyslots_size,string,omitempty"`
	Flags        []string `json:"flags,omitempty"` // one or more of "allow-discards", "same-cpu-crypt", "submit-from-crypt-cpus", "no-journal", "no-read-workqueue", "no-write-workqueue"
	Requirements []string `json:"requirements,omitempty"`
}

type V2JSONToken struct {
	Type                     string   `json:"type"` // "luks2-keyring"
	Keyslots                 []string `json:"keyslots,omitempty"`
	*V2JSONTokenLUKS2Keyring          // type == "luks2-keyring"
}

type V2JSONTokenLUKS2Keyring struct {
	KeyDescription string `json:"key_description"`
}
