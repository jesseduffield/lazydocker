package parser

import (
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/internal/errors"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
	"golang.org/x/xerrors"
)

type parser struct{}

func (p *parser) parseMapping(ctx *context) (*ast.MappingNode, error) {
	mapTk := ctx.currentToken()
	node := ast.Mapping(mapTk, true)
	node.SetPath(ctx.path)
	ctx.progress(1) // skip MappingStart token
	for ctx.next() {
		tk := ctx.currentToken()
		if tk.Type == token.MappingEndType {
			node.End = tk
			return node, nil
		} else if tk.Type == token.CollectEntryType {
			ctx.progress(1)
			continue
		}

		value, err := p.parseMappingValue(ctx)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse mapping value in mapping node")
		}
		mvnode, ok := value.(*ast.MappingValueNode)
		if !ok {
			return nil, errors.ErrSyntax("failed to parse flow mapping node", value.GetToken())
		}
		node.Values = append(node.Values, mvnode)
		ctx.progress(1)
	}
	return nil, errors.ErrSyntax("unterminated flow mapping", node.GetToken())
}

func (p *parser) parseSequence(ctx *context) (*ast.SequenceNode, error) {
	node := ast.Sequence(ctx.currentToken(), true)
	node.SetPath(ctx.path)
	ctx.progress(1) // skip SequenceStart token
	for ctx.next() {
		tk := ctx.currentToken()
		if tk.Type == token.SequenceEndType {
			node.End = tk
			break
		} else if tk.Type == token.CollectEntryType {
			ctx.progress(1)
			continue
		}

		value, err := p.parseToken(ctx.withIndex(uint(len(node.Values))), tk)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse sequence value in flow sequence node")
		}
		node.Values = append(node.Values, value)
		ctx.progress(1)
	}
	return node, nil
}

func (p *parser) parseTag(ctx *context) (*ast.TagNode, error) {
	tagToken := ctx.currentToken()
	node := ast.Tag(tagToken)
	node.SetPath(ctx.path)
	ctx.progress(1) // skip tag token
	var (
		value ast.Node
		err   error
	)
	switch token.ReservedTagKeyword(tagToken.Value) {
	case token.MappingTag,
		token.OrderedMapTag:
		value, err = p.parseMapping(ctx)
	case token.IntegerTag,
		token.FloatTag,
		token.StringTag,
		token.BinaryTag,
		token.TimestampTag,
		token.NullTag:
		typ := ctx.currentToken().Type
		if typ == token.LiteralType || typ == token.FoldedType {
			value, err = p.parseLiteral(ctx)
		} else {
			value = p.parseScalarValue(ctx.currentToken())
		}
	case token.SequenceTag,
		token.SetTag:
		err = errors.ErrSyntax(fmt.Sprintf("sorry, currently not supported %s tag", tagToken.Value), tagToken)
	default:
		// custom tag
		value, err = p.parseToken(ctx, ctx.currentToken())
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse tag value")
	}
	node.Value = value
	return node, nil
}

func (p *parser) removeLeftSideNewLineCharacter(src string) string {
	// CR or LF or CRLF
	return strings.TrimLeft(strings.TrimLeft(strings.TrimLeft(src, "\r"), "\n"), "\r\n")
}

func (p *parser) existsNewLineCharacter(src string) bool {
	if strings.Index(src, "\n") > 0 {
		return true
	}
	if strings.Index(src, "\r") > 0 {
		return true
	}
	return false
}

func (p *parser) validateMapKey(tk *token.Token) error {
	if tk.Type != token.StringType {
		return nil
	}
	origin := p.removeLeftSideNewLineCharacter(tk.Origin)
	if p.existsNewLineCharacter(origin) {
		return errors.ErrSyntax("unexpected key name", tk)
	}
	return nil
}

func (p *parser) createNullToken(base *token.Token) *token.Token {
	pos := *(base.Position)
	pos.Column++
	return token.New("null", "null", &pos)
}

func (p *parser) parseMapValue(ctx *context, key ast.MapKeyNode, colonToken *token.Token) (ast.Node, error) {
	node, err := p.createMapValueNode(ctx, key, colonToken)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create map value node")
	}
	if node != nil && node.GetPath() == "" {
		node.SetPath(ctx.path)
	}
	return node, nil
}

func (p *parser) createMapValueNode(ctx *context, key ast.MapKeyNode, colonToken *token.Token) (ast.Node, error) {
	tk := ctx.currentToken()
	if tk == nil {
		nullToken := p.createNullToken(colonToken)
		ctx.insertToken(ctx.idx, nullToken)
		return ast.Null(nullToken), nil
	}

	if tk.Position.Column == key.GetToken().Position.Column && tk.Type == token.StringType {
		// in this case,
		// ----
		// key: <value does not defined>
		// next
		nullToken := p.createNullToken(colonToken)
		ctx.insertToken(ctx.idx, nullToken)
		return ast.Null(nullToken), nil
	}

	if tk.Position.Column < key.GetToken().Position.Column {
		// in this case,
		// ----
		//   key: <value does not defined>
		// next
		nullToken := p.createNullToken(colonToken)
		ctx.insertToken(ctx.idx, nullToken)
		return ast.Null(nullToken), nil
	}

	value, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse mapping 'value' node")
	}
	return value, nil
}

func (p *parser) validateMapValue(ctx *context, key, value ast.Node) error {
	keyColumn := key.GetToken().Position.Column
	valueColumn := value.GetToken().Position.Column
	if keyColumn != valueColumn {
		return nil
	}
	if value.Type() != ast.StringType {
		return nil
	}
	ntk := ctx.nextToken()
	if ntk == nil || (ntk.Type != token.MappingValueType && ntk.Type != token.SequenceEntryType) {
		return errors.ErrSyntax("could not found expected ':' token", value.GetToken())
	}
	return nil
}

func (p *parser) parseMappingValue(ctx *context) (ast.Node, error) {
	key, err := p.parseMapKey(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse map key")
	}
	keyText := key.GetToken().Value
	key.SetPath(ctx.withChild(keyText).path)
	if err := p.validateMapKey(key.GetToken()); err != nil {
		return nil, errors.Wrapf(err, "validate mapping key error")
	}
	ctx.progress(1)          // progress to mapping value token
	tk := ctx.currentToken() // get mapping value token
	if tk == nil {
		return nil, errors.ErrSyntax("unexpected map", key.GetToken())
	}
	ctx.progress(1) // progress to value token
	if err := p.setSameLineCommentIfExists(ctx.withChild(keyText), key); err != nil {
		return nil, errors.Wrapf(err, "failed to set same line comment to node")
	}
	if key.GetComment() != nil {
		// if current token is comment, GetComment() is not nil.
		// then progress to value token
		ctx.progressIgnoreComment(1)
	}

	value, err := p.parseMapValue(ctx.withChild(keyText), key, tk)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse map value")
	}
	if err := p.validateMapValue(ctx, key, value); err != nil {
		return nil, errors.Wrapf(err, "failed to validate map value")
	}

	mvnode := ast.MappingValue(tk, key, value)
	mvnode.SetPath(ctx.withChild(keyText).path)
	node := ast.Mapping(tk, false, mvnode)
	node.SetPath(ctx.withChild(keyText).path)

	ntk := ctx.nextNotCommentToken()
	antk := ctx.afterNextNotCommentToken()
	for antk != nil && antk.Type == token.MappingValueType &&
		ntk.Position.Column == key.GetToken().Position.Column {
		ctx.progressIgnoreComment(1)
		value, err := p.parseToken(ctx, ctx.currentToken())
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse mapping node")
		}
		switch value.Type() {
		case ast.MappingType:
			c := value.(*ast.MappingNode)
			comment := c.GetComment()
			for idx, v := range c.Values {
				if idx == 0 && comment != nil {
					if err := v.SetComment(comment); err != nil {
						return nil, errors.Wrapf(err, "failed to set comment token to node")
					}
				}
				node.Values = append(node.Values, v)
			}
		case ast.MappingValueType:
			node.Values = append(node.Values, value.(*ast.MappingValueNode))
		default:
			return nil, xerrors.Errorf("failed to parse mapping value node node is %s", value.Type())
		}
		ntk = ctx.nextNotCommentToken()
		antk = ctx.afterNextNotCommentToken()
	}
	if len(node.Values) == 1 {
		mapKeyCol := mvnode.Key.GetToken().Position.Column
		commentTk := ctx.nextToken()
		if commentTk != nil && commentTk.Type == token.CommentType && mapKeyCol <= commentTk.Position.Column {
			// If the comment is in the same or deeper column as the last element column in map value,
			// treat it as a footer comment for the last element.
			comment := p.parseFootComment(ctx, mapKeyCol)
			mvnode.FootComment = comment
		}
		return mvnode, nil
	}
	mapCol := node.GetToken().Position.Column
	commentTk := ctx.nextToken()
	if commentTk != nil && commentTk.Type == token.CommentType && mapCol <= commentTk.Position.Column {
		// If the comment is in the same or deeper column as the last element column in map value,
		// treat it as a footer comment for the last element.
		comment := p.parseFootComment(ctx, mapCol)
		node.FootComment = comment
	}
	return node, nil
}

func (p *parser) parseSequenceEntry(ctx *context) (*ast.SequenceNode, error) {
	tk := ctx.currentToken()
	sequenceNode := ast.Sequence(tk, false)
	sequenceNode.SetPath(ctx.path)
	curColumn := tk.Position.Column
	for tk.Type == token.SequenceEntryType {
		ctx.progress(1) // skip sequence token
		tk = ctx.currentToken()
		if tk == nil {
			return nil, errors.ErrSyntax("empty sequence entry", ctx.previousToken())
		}
		var comment *ast.CommentGroupNode
		if tk.Type == token.CommentType {
			comment = p.parseCommentOnly(ctx)
			tk = ctx.currentToken()
			if tk.Type != token.SequenceEntryType {
				break
			}
			ctx.progress(1) // skip sequence token
		}
		value, err := p.parseToken(ctx.withIndex(uint(len(sequenceNode.Values))), ctx.currentToken())
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse sequence")
		}
		if comment != nil {
			comment.SetPath(ctx.withIndex(uint(len(sequenceNode.Values))).path)
			sequenceNode.ValueHeadComments = append(sequenceNode.ValueHeadComments, comment)
		} else {
			sequenceNode.ValueHeadComments = append(sequenceNode.ValueHeadComments, nil)
		}
		sequenceNode.Values = append(sequenceNode.Values, value)
		tk = ctx.nextNotCommentToken()
		if tk == nil {
			break
		}
		if tk.Type != token.SequenceEntryType {
			break
		}
		if tk.Position.Column != curColumn {
			break
		}
		ctx.progressIgnoreComment(1)
	}
	commentTk := ctx.nextToken()
	if commentTk != nil && commentTk.Type == token.CommentType && curColumn <= commentTk.Position.Column {
		// If the comment is in the same or deeper column as the last element column in sequence value,
		// treat it as a footer comment for the last element.
		comment := p.parseFootComment(ctx, curColumn)
		sequenceNode.FootComment = comment
	}
	return sequenceNode, nil
}

func (p *parser) parseAnchor(ctx *context) (*ast.AnchorNode, error) {
	tk := ctx.currentToken()
	anchor := ast.Anchor(tk)
	anchor.SetPath(ctx.path)
	ntk := ctx.nextToken()
	if ntk == nil {
		return nil, errors.ErrSyntax("unexpected anchor. anchor name is undefined", tk)
	}
	ctx.progress(1) // skip anchor token
	name, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parser anchor name node")
	}
	anchor.Name = name
	ntk = ctx.nextToken()
	if ntk == nil {
		return nil, errors.ErrSyntax("unexpected anchor. anchor value is undefined", ctx.currentToken())
	}
	ctx.progress(1)
	value, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parser anchor name node")
	}
	anchor.Value = value
	return anchor, nil
}

func (p *parser) parseAlias(ctx *context) (*ast.AliasNode, error) {
	tk := ctx.currentToken()
	alias := ast.Alias(tk)
	alias.SetPath(ctx.path)
	ntk := ctx.nextToken()
	if ntk == nil {
		return nil, errors.ErrSyntax("unexpected alias. alias name is undefined", tk)
	}
	ctx.progress(1) // skip alias token
	name, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parser alias name node")
	}
	alias.Value = name
	return alias, nil
}

func (p *parser) parseMapKey(ctx *context) (ast.MapKeyNode, error) {
	tk := ctx.currentToken()
	if value := p.parseScalarValue(tk); value != nil {
		return value, nil
	}
	switch tk.Type {
	case token.MergeKeyType:
		return ast.MergeKey(tk), nil
	case token.MappingKeyType:
		return p.parseMappingKey(ctx)
	}
	return nil, errors.ErrSyntax("unexpected mapping key", tk)
}

func (p *parser) parseStringValue(tk *token.Token) *ast.StringNode {
	switch tk.Type {
	case token.StringType,
		token.SingleQuoteType,
		token.DoubleQuoteType:
		return ast.String(tk)
	}
	return nil
}

func (p *parser) parseScalarValueWithComment(ctx *context, tk *token.Token) (ast.ScalarNode, error) {
	node := p.parseScalarValue(tk)
	if node == nil {
		return nil, nil
	}
	node.SetPath(ctx.path)
	if p.isSameLineComment(ctx.nextToken(), node) {
		ctx.progress(1)
		if err := p.setSameLineCommentIfExists(ctx, node); err != nil {
			return nil, errors.Wrapf(err, "failed to set same line comment to node")
		}
	}
	return node, nil
}

func (p *parser) parseScalarValue(tk *token.Token) ast.ScalarNode {
	if node := p.parseStringValue(tk); node != nil {
		return node
	}
	switch tk.Type {
	case token.NullType:
		return ast.Null(tk)
	case token.BoolType:
		return ast.Bool(tk)
	case token.IntegerType,
		token.BinaryIntegerType,
		token.OctetIntegerType,
		token.HexIntegerType:
		return ast.Integer(tk)
	case token.FloatType:
		return ast.Float(tk)
	case token.InfinityType:
		return ast.Infinity(tk)
	case token.NanType:
		return ast.Nan(tk)
	}
	return nil
}

func (p *parser) parseDirective(ctx *context) (*ast.DirectiveNode, error) {
	node := ast.Directive(ctx.currentToken())
	ctx.progress(1) // skip directive token
	value, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse directive value")
	}
	node.Value = value
	ctx.progress(1)
	tk := ctx.currentToken()
	if tk == nil {
		// Since current token is nil, use the previous token to specify
		// the syntax error location.
		return nil, errors.ErrSyntax("unexpected directive value. document not started", ctx.previousToken())
	}
	if tk.Type != token.DocumentHeaderType {
		return nil, errors.ErrSyntax("unexpected directive value. document not started", ctx.currentToken())
	}
	return node, nil
}

func (p *parser) parseLiteral(ctx *context) (*ast.LiteralNode, error) {
	node := ast.Literal(ctx.currentToken())
	ctx.progress(1) // skip literal/folded token

	tk := ctx.currentToken()
	var comment *ast.CommentGroupNode
	if tk.Type == token.CommentType {
		comment = p.parseCommentOnly(ctx)
		comment.SetPath(ctx.path)
		if err := node.SetComment(comment); err != nil {
			return nil, errors.Wrapf(err, "failed to set comment to literal")
		}
		tk = ctx.currentToken()
	}
	value, err := p.parseToken(ctx, tk)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse literal/folded value")
	}
	snode, ok := value.(*ast.StringNode)
	if !ok {
		return nil, errors.ErrSyntax("unexpected token. required string token", value.GetToken())
	}
	node.Value = snode
	return node, nil
}

func (p *parser) isSameLineComment(tk *token.Token, node ast.Node) bool {
	if tk == nil {
		return false
	}
	if tk.Type != token.CommentType {
		return false
	}
	return tk.Position.Line == node.GetToken().Position.Line
}

func (p *parser) setSameLineCommentIfExists(ctx *context, node ast.Node) error {
	tk := ctx.currentToken()
	if !p.isSameLineComment(tk, node) {
		return nil
	}
	comment := ast.CommentGroup([]*token.Token{tk})
	comment.SetPath(ctx.path)
	if err := node.SetComment(comment); err != nil {
		return errors.Wrapf(err, "failed to set comment token to ast.Node")
	}
	return nil
}

func (p *parser) parseDocument(ctx *context) (*ast.DocumentNode, error) {
	startTk := ctx.currentToken()
	ctx.progress(1) // skip document header token
	body, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse document body")
	}
	node := ast.Document(startTk, body)
	if ntk := ctx.nextToken(); ntk != nil && ntk.Type == token.DocumentEndType {
		node.End = ntk
		ctx.progress(1)
	}
	return node, nil
}

func (p *parser) parseCommentOnly(ctx *context) *ast.CommentGroupNode {
	commentTokens := []*token.Token{}
	for {
		tk := ctx.currentToken()
		if tk == nil {
			break
		}
		if tk.Type != token.CommentType {
			break
		}
		commentTokens = append(commentTokens, tk)
		ctx.progressIgnoreComment(1) // skip comment token
	}
	return ast.CommentGroup(commentTokens)
}

func (p *parser) parseFootComment(ctx *context, col int) *ast.CommentGroupNode {
	commentTokens := []*token.Token{}
	for {
		ctx.progressIgnoreComment(1)
		commentTokens = append(commentTokens, ctx.currentToken())

		nextTk := ctx.nextToken()
		if nextTk == nil {
			break
		}
		if nextTk.Type != token.CommentType {
			break
		}
		if col > nextTk.Position.Column {
			break
		}
	}
	return ast.CommentGroup(commentTokens)
}

func (p *parser) parseComment(ctx *context) (ast.Node, error) {
	group := p.parseCommentOnly(ctx)
	node, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse node after comment")
	}
	if node == nil {
		return group, nil
	}
	group.SetPath(node.GetPath())
	if err := node.SetComment(group); err != nil {
		return nil, errors.Wrapf(err, "failed to set comment token to node")
	}
	return node, nil
}

func (p *parser) parseMappingKey(ctx *context) (*ast.MappingKeyNode, error) {
	keyTk := ctx.currentToken()
	node := ast.MappingKey(keyTk)
	node.SetPath(ctx.path)
	ctx.progress(1) // skip mapping key token
	value, err := p.parseToken(ctx.withChild(keyTk.Value), ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse map key")
	}
	node.Value = value
	return node, nil
}

func (p *parser) parseToken(ctx *context, tk *token.Token) (ast.Node, error) {
	node, err := p.createNodeFromToken(ctx, tk)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create node from token")
	}
	if node != nil && node.GetPath() == "" {
		node.SetPath(ctx.path)
	}
	return node, nil
}

func (p *parser) createNodeFromToken(ctx *context, tk *token.Token) (ast.Node, error) {
	if tk == nil {
		return nil, nil
	}
	if tk.NextType() == token.MappingValueType {
		node, err := p.parseMappingValue(ctx)
		return node, err
	}
	node, err := p.parseScalarValueWithComment(ctx, tk)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse scalar value")
	}
	if node != nil {
		return node, nil
	}
	switch tk.Type {
	case token.CommentType:
		return p.parseComment(ctx)
	case token.MappingKeyType:
		return p.parseMappingKey(ctx)
	case token.DocumentHeaderType:
		return p.parseDocument(ctx)
	case token.MappingStartType:
		return p.parseMapping(ctx)
	case token.SequenceStartType:
		return p.parseSequence(ctx)
	case token.SequenceEntryType:
		return p.parseSequenceEntry(ctx)
	case token.AnchorType:
		return p.parseAnchor(ctx)
	case token.AliasType:
		return p.parseAlias(ctx)
	case token.DirectiveType:
		return p.parseDirective(ctx)
	case token.TagType:
		return p.parseTag(ctx)
	case token.LiteralType, token.FoldedType:
		return p.parseLiteral(ctx)
	}
	return nil, nil
}

func (p *parser) parse(tokens token.Tokens, mode Mode) (*ast.File, error) {
	ctx := newContext(tokens, mode)
	file := &ast.File{Docs: []*ast.DocumentNode{}}
	for ctx.next() {
		node, err := p.parseToken(ctx, ctx.currentToken())
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse")
		}
		ctx.progressIgnoreComment(1)
		if node == nil {
			continue
		}
		if doc, ok := node.(*ast.DocumentNode); ok {
			file.Docs = append(file.Docs, doc)
		} else {
			file.Docs = append(file.Docs, ast.Document(nil, node))
		}
	}
	return file, nil
}

type Mode uint

const (
	ParseComments Mode = 1 << iota // parse comments and add them to AST
)

// ParseBytes parse from byte slice, and returns ast.File
func ParseBytes(bytes []byte, mode Mode) (*ast.File, error) {
	tokens := lexer.Tokenize(string(bytes))
	f, err := Parse(tokens, mode)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse")
	}
	return f, nil
}

// Parse parse from token instances, and returns ast.File
func Parse(tokens token.Tokens, mode Mode) (*ast.File, error) {
	var p parser
	f, err := p.parse(tokens, mode)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse")
	}
	return f, nil
}

// Parse parse from filename, and returns ast.File
func ParseFile(filename string, mode Mode) (*ast.File, error) {
	file, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read file: %s", filename)
	}
	f, err := ParseBytes(file, mode)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse")
	}
	f.Name = filename
	return f, nil
}
