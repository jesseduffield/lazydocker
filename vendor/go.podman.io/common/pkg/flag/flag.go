package flag

import (
	"strconv"

	"github.com/spf13/pflag"
)

// OptionalBool is a boolean with a separate presence flag and value.
type OptionalBool struct {
	present bool
	value   bool
}

// Present returns the bool's presence flag.
func (ob *OptionalBool) Present() bool {
	return ob.present
}

// Value returns the bool's value. Should only be used if Present() is true.
func (ob *OptionalBool) Value() bool {
	return ob.value
}

// optionalBool is a cli.Generic == flag.Value implementation equivalent to
// the one underlying flag.Bool, except that it records whether the flag has been set.
// This is distinct from optionalBool to (pretend to) force callers to use
// optionalBoolFlag.
type optionalBoolValue OptionalBool

// OptionalBoolFlag creates new flag for an optional in the specified flag with
// the specified name and usage.
func OptionalBoolFlag(fs *pflag.FlagSet, p *OptionalBool, name, usage string) *pflag.Flag {
	flag := fs.VarPF(internalNewOptionalBoolValue(p), name, "", usage)
	flag.NoOptDefVal = "true"
	flag.DefValue = "false"
	return flag
}

// WARNING: Do not directly use this method to define optionalBool flag.
// Caller should use optionalBoolFlag.
func internalNewOptionalBoolValue(p *OptionalBool) pflag.Value {
	p.present = false
	return (*optionalBoolValue)(p)
}

// Set parses the string to a bool and sets it.
func (ob *optionalBoolValue) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	ob.value = v
	ob.present = true
	return nil
}

// String returns the string representation of the string.
func (ob *optionalBoolValue) String() string {
	if !ob.present {
		return "" // This is, sadly, not round-trip safe: --flag is interpreted as --flag=true
	}
	return strconv.FormatBool(ob.value)
}

// Type returns the type.
func (ob *optionalBoolValue) Type() string {
	return "bool"
}

// IsBoolFlag indicates that it's a bool flag.
func (ob *optionalBoolValue) IsBoolFlag() bool {
	return true
}

// OptionalString is a string with a separate presence flag.
type OptionalString struct {
	present bool
	value   string
}

// Present returns the strings's presence flag.
func (os *OptionalString) Present() bool {
	return os.present
}

// Value returns the string's value. Should only be used if Present() is true.
func (os *OptionalString) Value() string {
	return os.value
}

// optionalString is a cli.Generic == flag.Value implementation equivalent to
// the one underlying flag.String, except that it records whether the flag has been set.
// This is distinct from optionalString to (pretend to) force callers to use
// newoptionalString.
type optionalStringValue OptionalString

// NewOptionalStringValue returns a pflag.Value for the string.
func NewOptionalStringValue(p *OptionalString) pflag.Value {
	p.present = false
	return (*optionalStringValue)(p)
}

// Set sets the string.
func (ob *optionalStringValue) Set(s string) error {
	ob.value = s
	ob.present = true
	return nil
}

// String returns the string if present.
func (ob *optionalStringValue) String() string {
	if !ob.present {
		return "" // This is, sadly, not round-trip safe: --flag= is interpreted as {present:true, value:""}
	}
	return ob.value
}

// Type returns the string type.
func (ob *optionalStringValue) Type() string {
	return "string"
}

// OptionalInt is a int with a separate presence flag.
type OptionalInt struct {
	present bool
	value   int
}

// Present returns the int's presence flag.
func (oi *OptionalInt) Present() bool {
	return oi.present
}

// Value returns the int's value. Should only be used if Present() is true.
func (oi *OptionalInt) Value() int {
	return oi.value
}

// optionalInt is a cli.Generic == flag.Value implementation equivalent to
// the one underlying flag.Int, except that it records whether the flag has been set.
// This is distinct from optionalInt to (pretend to) force callers to use
// newoptionalIntValue.
type optionalIntValue OptionalInt

// NewOptionalIntValue returns the pflag.Value of the int.
func NewOptionalIntValue(p *OptionalInt) pflag.Value {
	p.present = false
	return (*optionalIntValue)(p)
}

// Set parses the string to an int and sets it.
func (ob *optionalIntValue) Set(s string) error {
	v, err := strconv.ParseInt(s, 0, strconv.IntSize)
	if err != nil {
		return err
	}
	ob.value = int(v)
	ob.present = true
	return nil
}

// String returns the string representation of the int.
func (ob *optionalIntValue) String() string {
	if !ob.present {
		return "" // If the value is not present, just return an empty string, any other value wouldn't make sense.
	}
	return strconv.Itoa(ob.value)
}

// Type returns the int's type.
func (ob *optionalIntValue) Type() string {
	return "int"
}
