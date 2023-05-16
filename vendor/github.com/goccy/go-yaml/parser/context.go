package parser

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/token"
)

// context context at parsing
type context struct {
	parent *context
	idx    int
	size   int
	tokens token.Tokens
	mode   Mode
	path   string
}

var pathSpecialChars = []string{
	"$", "*", ".", "[", "]",
}

func containsPathSpecialChar(path string) bool {
	for _, char := range pathSpecialChars {
		if strings.Contains(path, char) {
			return true
		}
	}
	return false
}

func normalizePath(path string) string {
	if containsPathSpecialChar(path) {
		return fmt.Sprintf("'%s'", path)
	}
	return path
}

func (c *context) withChild(path string) *context {
	ctx := c.copy()
	path = normalizePath(path)
	ctx.path += fmt.Sprintf(".%s", path)
	return ctx
}

func (c *context) withIndex(idx uint) *context {
	ctx := c.copy()
	ctx.path += fmt.Sprintf("[%d]", idx)
	return ctx
}

func (c *context) copy() *context {
	return &context{
		parent: c,
		idx:    c.idx,
		size:   c.size,
		tokens: append(token.Tokens{}, c.tokens...),
		mode:   c.mode,
		path:   c.path,
	}
}

func (c *context) next() bool {
	return c.idx < c.size
}

func (c *context) previousToken() *token.Token {
	if c.idx > 0 {
		return c.tokens[c.idx-1]
	}
	return nil
}

func (c *context) insertToken(idx int, tk *token.Token) {
	if c.parent != nil {
		c.parent.insertToken(idx, tk)
	}
	if c.size < idx {
		return
	}
	if c.size == idx {
		curToken := c.tokens[c.size-1]
		tk.Next = curToken
		curToken.Prev = tk

		c.tokens = append(c.tokens, tk)
		c.size = len(c.tokens)
		return
	}

	curToken := c.tokens[idx]
	tk.Next = curToken
	curToken.Prev = tk

	c.tokens = append(c.tokens[:idx+1], c.tokens[idx:]...)
	c.tokens[idx] = tk
	c.size = len(c.tokens)
}

func (c *context) currentToken() *token.Token {
	if c.idx >= c.size {
		return nil
	}
	return c.tokens[c.idx]
}

func (c *context) nextToken() *token.Token {
	if c.idx+1 >= c.size {
		return nil
	}
	return c.tokens[c.idx+1]
}

func (c *context) afterNextToken() *token.Token {
	if c.idx+2 >= c.size {
		return nil
	}
	return c.tokens[c.idx+2]
}

func (c *context) nextNotCommentToken() *token.Token {
	for i := c.idx + 1; i < c.size; i++ {
		tk := c.tokens[i]
		if tk.Type == token.CommentType {
			continue
		}
		return tk
	}
	return nil
}

func (c *context) afterNextNotCommentToken() *token.Token {
	notCommentTokenCount := 0
	for i := c.idx + 1; i < c.size; i++ {
		tk := c.tokens[i]
		if tk.Type == token.CommentType {
			continue
		}
		notCommentTokenCount++
		if notCommentTokenCount == 2 {
			return tk
		}
	}
	return nil
}

func (c *context) enabledComment() bool {
	return c.mode&ParseComments != 0
}

func (c *context) isCurrentCommentToken() bool {
	tk := c.currentToken()
	if tk == nil {
		return false
	}
	return tk.Type == token.CommentType
}

func (c *context) progressIgnoreComment(num int) {
	if c.parent != nil {
		c.parent.progressIgnoreComment(num)
	}
	if c.size <= c.idx+num {
		c.idx = c.size
	} else {
		c.idx += num
	}
}

func (c *context) progress(num int) {
	if c.isCurrentCommentToken() {
		return
	}
	c.progressIgnoreComment(num)
}

func newContext(tokens token.Tokens, mode Mode) *context {
	filteredTokens := []*token.Token{}
	if mode&ParseComments != 0 {
		filteredTokens = tokens
	} else {
		for _, tk := range tokens {
			if tk.Type == token.CommentType {
				continue
			}
			// keep prev/next reference between tokens containing comments
			// https://github.com/goccy/go-yaml/issues/254
			filteredTokens = append(filteredTokens, tk)
		}
	}
	return &context{
		idx:    0,
		size:   len(filteredTokens),
		tokens: token.Tokens(filteredTokens),
		mode:   mode,
		path:   "$",
	}
}
