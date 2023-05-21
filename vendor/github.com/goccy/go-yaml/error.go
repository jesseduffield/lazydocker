package yaml

import (
	"github.com/goccy/go-yaml/ast"
	"golang.org/x/xerrors"
)

var (
	ErrInvalidQuery               = xerrors.New("invalid query")
	ErrInvalidPath                = xerrors.New("invalid path instance")
	ErrInvalidPathString          = xerrors.New("invalid path string")
	ErrNotFoundNode               = xerrors.New("node not found")
	ErrUnknownCommentPositionType = xerrors.New("unknown comment position type")
	ErrInvalidCommentMapValue     = xerrors.New("invalid comment map value. it must be not nil value")
)

func ErrUnsupportedHeadPositionType(node ast.Node) error {
	return xerrors.Errorf("unsupported comment head position for %s", node.Type())
}

func ErrUnsupportedLinePositionType(node ast.Node) error {
	return xerrors.Errorf("unsupported comment line position for %s", node.Type())
}

func ErrUnsupportedFootPositionType(node ast.Node) error {
	return xerrors.Errorf("unsupported comment foot position for %s", node.Type())
}

// IsInvalidQueryError whether err is ErrInvalidQuery or not.
func IsInvalidQueryError(err error) bool {
	return xerrors.Is(err, ErrInvalidQuery)
}

// IsInvalidPathError whether err is ErrInvalidPath or not.
func IsInvalidPathError(err error) bool {
	return xerrors.Is(err, ErrInvalidPath)
}

// IsInvalidPathStringError whether err is ErrInvalidPathString or not.
func IsInvalidPathStringError(err error) bool {
	return xerrors.Is(err, ErrInvalidPathString)
}

// IsNotFoundNodeError whether err is ErrNotFoundNode or not.
func IsNotFoundNodeError(err error) bool {
	return xerrors.Is(err, ErrNotFoundNode)
}

// IsInvalidTokenTypeError whether err is ast.ErrInvalidTokenType or not.
func IsInvalidTokenTypeError(err error) bool {
	return xerrors.Is(err, ast.ErrInvalidTokenType)
}

// IsInvalidAnchorNameError whether err is ast.ErrInvalidAnchorName or not.
func IsInvalidAnchorNameError(err error) bool {
	return xerrors.Is(err, ast.ErrInvalidAnchorName)
}

// IsInvalidAliasNameError whether err is ast.ErrInvalidAliasName or not.
func IsInvalidAliasNameError(err error) bool {
	return xerrors.Is(err, ast.ErrInvalidAliasName)
}
