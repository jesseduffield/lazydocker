// Package keybd_event is used for a key press simulated in Windows, Linux and Mac
package keybd_event

//KeyBonding type for keybd_event
type KeyBonding struct {
	hasCTRL   bool
	hasALT    bool
	hasSHIFT  bool
	hasRCTRL  bool
	hasRSHIFT bool
	hasALTGR  bool
	hasSuper  bool
	keys      []int
}

//NewKeyBonding Use for create struct KeyBounding
func NewKeyBonding() (KeyBonding, error) {
	keyBounding := KeyBonding{}
	keyBounding.Clear()
	err := initKeyBD()
	if err != nil {
		return keyBounding, err
	}
	return keyBounding, nil
}

//Clear clean instance
func (k *KeyBonding) Clear() {
	k.hasALT = false
	k.hasCTRL = false
	k.hasSHIFT = false
	k.hasRCTRL = false
	k.hasRSHIFT = false
	k.hasALTGR = false
	k.hasSuper = false
	k.keys = []int{}
}

//SetKeys set keys
func (k *KeyBonding) SetKeys(keys ...int) {
	k.keys = keys
}

//AddKey add one key pressed
func (k *KeyBonding) AddKey(key int) {
	k.keys = append(k.keys, key)
}

//HasALT If key ALT pressed
func (k *KeyBonding) HasALT(b bool) {
	k.hasALT = b
}

//HasCTRL If key CTRL pressed
func (k *KeyBonding) HasCTRL(b bool) {
	k.hasCTRL = b
}

//HasSHIFT If key SHIFT pressed
func (k *KeyBonding) HasSHIFT(b bool) {
	k.hasSHIFT = b
}

//HasALTGR If key ALTGR pressed
func (k *KeyBonding) HasALTGR(b bool) {
	k.hasALTGR = b
}

//HasSuper If key Super pressed
func (k *KeyBonding) HasSuper(b bool) {
	k.hasSuper = b
}

//HasCTRLR If key CTRLR pressed
//
//This is currently not supported on macOS
func (k *KeyBonding) HasCTRLR(b bool) {
	k.hasRCTRL = b
}

//HasSHIFTR If key SHIFTR pressed
//
//This is currently not supported on macOS
func (k *KeyBonding) HasSHIFTR(b bool) {
	k.hasRSHIFT = b
}
