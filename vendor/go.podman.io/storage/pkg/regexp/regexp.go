package regexp

import (
	"io"
	"regexp"
	"sync"
)

// Regexp is a wrapper struct used for wrapping MustCompile regex expressions
// used as global variables. Using this structure helps speed the startup time
// of apps that want to use global regex variables. This library initializes them on
// first use as opposed to the start of the executable.
type Regexp struct {
	*regexpStruct
}

type regexpStruct struct {
	_      noCopy
	once   sync.Once
	regexp *regexp.Regexp
	val    string
}

func Delayed(val string) Regexp {
	re := &regexpStruct{
		val: val,
	}
	if precompile {
		re.regexp = regexp.MustCompile(re.val)
	}
	return Regexp{re}
}

func (re *regexpStruct) compile() {
	if precompile {
		return
	}
	re.once.Do(func() {
		re.regexp = regexp.MustCompile(re.val)
	})
}

func (re *regexpStruct) Expand(dst []byte, template []byte, src []byte, match []int) []byte {
	re.compile()
	return re.regexp.Expand(dst, template, src, match)
}

func (re *regexpStruct) ExpandString(dst []byte, template string, src string, match []int) []byte {
	re.compile()
	return re.regexp.ExpandString(dst, template, src, match)
}

func (re *regexpStruct) Find(b []byte) []byte {
	re.compile()
	return re.regexp.Find(b)
}

func (re *regexpStruct) FindAll(b []byte, n int) [][]byte {
	re.compile()
	return re.regexp.FindAll(b, n)
}

func (re *regexpStruct) FindAllIndex(b []byte, n int) [][]int {
	re.compile()
	return re.regexp.FindAllIndex(b, n)
}

func (re *regexpStruct) FindAllString(s string, n int) []string {
	re.compile()
	return re.regexp.FindAllString(s, n)
}

func (re *regexpStruct) FindAllStringIndex(s string, n int) [][]int {
	re.compile()
	return re.regexp.FindAllStringIndex(s, n)
}

func (re *regexpStruct) FindAllStringSubmatch(s string, n int) [][]string {
	re.compile()
	return re.regexp.FindAllStringSubmatch(s, n)
}

func (re *regexpStruct) FindAllStringSubmatchIndex(s string, n int) [][]int {
	re.compile()
	return re.regexp.FindAllStringSubmatchIndex(s, n)
}

func (re *regexpStruct) FindAllSubmatch(b []byte, n int) [][][]byte {
	re.compile()
	return re.regexp.FindAllSubmatch(b, n)
}

func (re *regexpStruct) FindAllSubmatchIndex(b []byte, n int) [][]int {
	re.compile()
	return re.regexp.FindAllSubmatchIndex(b, n)
}

func (re *regexpStruct) FindIndex(b []byte) (loc []int) {
	re.compile()
	return re.regexp.FindIndex(b)
}

func (re *regexpStruct) FindReaderIndex(r io.RuneReader) (loc []int) {
	re.compile()
	return re.regexp.FindReaderIndex(r)
}

func (re *regexpStruct) FindReaderSubmatchIndex(r io.RuneReader) []int {
	re.compile()
	return re.regexp.FindReaderSubmatchIndex(r)
}

func (re *regexpStruct) FindString(s string) string {
	re.compile()
	return re.regexp.FindString(s)
}

func (re *regexpStruct) FindStringIndex(s string) (loc []int) {
	re.compile()
	return re.regexp.FindStringIndex(s)
}

func (re *regexpStruct) FindStringSubmatch(s string) []string {
	re.compile()
	return re.regexp.FindStringSubmatch(s)
}

func (re *regexpStruct) FindStringSubmatchIndex(s string) []int {
	re.compile()
	return re.regexp.FindStringSubmatchIndex(s)
}

func (re *regexpStruct) FindSubmatch(b []byte) [][]byte {
	re.compile()
	return re.regexp.FindSubmatch(b)
}

func (re *regexpStruct) FindSubmatchIndex(b []byte) []int {
	re.compile()
	return re.regexp.FindSubmatchIndex(b)
}

func (re *regexpStruct) LiteralPrefix() (prefix string, complete bool) {
	re.compile()
	return re.regexp.LiteralPrefix()
}

func (re *regexpStruct) Longest() {
	re.compile()
	re.regexp.Longest()
}

func (re *regexpStruct) Match(b []byte) bool {
	re.compile()
	return re.regexp.Match(b)
}

func (re *regexpStruct) MatchReader(r io.RuneReader) bool {
	re.compile()
	return re.regexp.MatchReader(r)
}

func (re *regexpStruct) MatchString(s string) bool {
	re.compile()
	return re.regexp.MatchString(s)
}

func (re *regexpStruct) NumSubexp() int {
	re.compile()
	return re.regexp.NumSubexp()
}

func (re *regexpStruct) ReplaceAll(src, repl []byte) []byte {
	re.compile()
	return re.regexp.ReplaceAll(src, repl)
}

func (re *regexpStruct) ReplaceAllFunc(src []byte, repl func([]byte) []byte) []byte {
	re.compile()
	return re.regexp.ReplaceAllFunc(src, repl)
}

func (re *regexpStruct) ReplaceAllLiteral(src, repl []byte) []byte {
	re.compile()
	return re.regexp.ReplaceAllLiteral(src, repl)
}

func (re *regexpStruct) ReplaceAllLiteralString(src, repl string) string {
	re.compile()
	return re.regexp.ReplaceAllLiteralString(src, repl)
}

func (re *regexpStruct) ReplaceAllString(src, repl string) string {
	re.compile()
	return re.regexp.ReplaceAllString(src, repl)
}

func (re *regexpStruct) ReplaceAllStringFunc(src string, repl func(string) string) string {
	re.compile()
	return re.regexp.ReplaceAllStringFunc(src, repl)
}

func (re *regexpStruct) Split(s string, n int) []string {
	re.compile()
	return re.regexp.Split(s, n)
}

func (re *regexpStruct) String() string {
	re.compile()
	return re.regexp.String()
}

func (re *regexpStruct) SubexpIndex(name string) int {
	re.compile()
	return re.regexp.SubexpIndex(name)
}

func (re *regexpStruct) SubexpNames() []string {
	re.compile()
	return re.regexp.SubexpNames()
}

// noCopy may be added to structs which must not be copied
// after the first use.
//
// See https://golang.org/issues/8005#issuecomment-190753527
// for details.
//
// Note that it must not be embedded, due to the Lock and Unlock methods.
type noCopy struct{}

// Lock is a no-op used by -copylocks checker from `go vet`.
func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}
