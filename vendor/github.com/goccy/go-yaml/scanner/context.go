package scanner

import (
	"sync"

	"github.com/goccy/go-yaml/token"
)

const whitespace = ' '

// Context context at scanning
type Context struct {
	idx                int
	size               int
	notSpaceCharPos    int
	notSpaceOrgCharPos int
	src                []rune
	buf                []rune
	obuf               []rune
	tokens             token.Tokens
	isRawFolded        bool
	isLiteral          bool
	isFolded           bool
	isSingleLine       bool
	literalOpt         string
}

var (
	ctxPool = sync.Pool{
		New: func() interface{} {
			return createContext()
		},
	}
)

func createContext() *Context {
	return &Context{
		idx:          0,
		tokens:       token.Tokens{},
		isSingleLine: true,
	}
}

func newContext(src []rune) *Context {
	ctx := ctxPool.Get().(*Context)
	ctx.reset(src)
	return ctx
}

func (c *Context) release() {
	ctxPool.Put(c)
}

func (c *Context) reset(src []rune) {
	c.idx = 0
	c.size = len(src)
	c.src = src
	c.tokens = c.tokens[:0]
	c.resetBuffer()
	c.isRawFolded = false
	c.isSingleLine = true
	c.isLiteral = false
	c.isFolded = false
	c.literalOpt = ""
}

func (c *Context) resetBuffer() {
	c.buf = c.buf[:0]
	c.obuf = c.obuf[:0]
	c.notSpaceCharPos = 0
	c.notSpaceOrgCharPos = 0
}

func (c *Context) isSaveIndentMode() bool {
	return c.isLiteral || c.isFolded || c.isRawFolded
}

func (c *Context) breakLiteral() {
	c.isLiteral = false
	c.isRawFolded = false
	c.isFolded = false
	c.literalOpt = ""
}

func (c *Context) addToken(tk *token.Token) {
	if tk == nil {
		return
	}
	c.tokens = append(c.tokens, tk)
}

func (c *Context) addBuf(r rune) {
	if len(c.buf) == 0 && r == ' ' {
		return
	}
	c.buf = append(c.buf, r)
	if r != ' ' && r != '\t' {
		c.notSpaceCharPos = len(c.buf)
	}
}

func (c *Context) addOriginBuf(r rune) {
	c.obuf = append(c.obuf, r)
	if r != ' ' && r != '\t' {
		c.notSpaceOrgCharPos = len(c.obuf)
	}
}

func (c *Context) removeRightSpaceFromBuf() int {
	trimmedBuf := c.obuf[:c.notSpaceOrgCharPos]
	buflen := len(trimmedBuf)
	diff := len(c.obuf) - buflen
	if diff > 0 {
		c.obuf = c.obuf[:buflen]
		c.buf = c.bufferedSrc()
	}
	return diff
}

func (c *Context) isDocument() bool {
	return c.isLiteral || c.isFolded || c.isRawFolded
}

func (c *Context) isEOS() bool {
	return len(c.src)-1 <= c.idx
}

func (c *Context) isNextEOS() bool {
	return len(c.src)-1 <= c.idx+1
}

func (c *Context) next() bool {
	return c.idx < c.size
}

func (c *Context) source(s, e int) string {
	return string(c.src[s:e])
}

func (c *Context) previousChar() rune {
	if c.idx > 0 {
		return c.src[c.idx-1]
	}
	return rune(0)
}

func (c *Context) currentChar() rune {
	if c.size > c.idx {
		return c.src[c.idx]
	}
	return rune(0)
}

func (c *Context) currentCharWithSkipWhitespace() rune {
	idx := c.idx
	for c.size > idx {
		ch := c.src[idx]
		if ch != whitespace {
			return ch
		}
		idx++
	}
	return rune(0)
}

func (c *Context) nextChar() rune {
	if c.size > c.idx+1 {
		return c.src[c.idx+1]
	}
	return rune(0)
}

func (c *Context) repeatNum(r rune) int {
	cnt := 0
	for i := c.idx; i < c.size; i++ {
		if c.src[i] == r {
			cnt++
		} else {
			break
		}
	}
	return cnt
}

func (c *Context) progress(num int) {
	c.idx += num
}

func (c *Context) nextPos() int {
	return c.idx + 1
}

func (c *Context) existsBuffer() bool {
	return len(c.bufferedSrc()) != 0
}

func (c *Context) bufferedSrc() []rune {
	src := c.buf[:c.notSpaceCharPos]
	if len(src) > 0 && src[len(src)-1] == '\n' && c.isDocument() && c.literalOpt == "-" {
		// remove end '\n' character
		src = src[:len(src)-1]
	}
	return src
}

func (c *Context) bufferedToken(pos *token.Position) *token.Token {
	if c.idx == 0 {
		return nil
	}
	source := c.bufferedSrc()
	if len(source) == 0 {
		return nil
	}
	var tk *token.Token
	if c.isDocument() {
		tk = token.String(string(source), string(c.obuf), pos)
	} else {
		tk = token.New(string(source), string(c.obuf), pos)
	}
	c.resetBuffer()
	return tk
}

func (c *Context) lastToken() *token.Token {
	if len(c.tokens) != 0 {
		return c.tokens[len(c.tokens)-1]
	}
	return nil
}
