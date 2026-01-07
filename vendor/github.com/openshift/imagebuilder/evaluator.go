package imagebuilder

import (
	"fmt"
	"io"
	"strings"

	buildkitparser "github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/openshift/imagebuilder/dockerfile/command"
	"github.com/openshift/imagebuilder/dockerfile/parser"
)

// ParseDockerfile parses the provided stream as a canonical Dockerfile
func ParseDockerfile(r io.Reader) (*parser.Node, error) {
	result, err := parser.Parse(r)
	if err != nil {
		return nil, err
	}
	return result.AST, nil
}

// Environment variable interpolation will happen on these statements only.
var replaceEnvAllowed = map[string]bool{
	command.Env:        true,
	command.Label:      true,
	command.Add:        true,
	command.Copy:       true,
	command.Workdir:    true,
	command.Expose:     true,
	command.Volume:     true,
	command.User:       true,
	command.StopSignal: true,
	command.Arg:        true,
}

// Certain commands are allowed to have their args split into more
// words after env var replacements. Meaning:
//
//	ENV foo="123 456"
//	EXPOSE $foo
//
// should result in the same thing as:
//
//	EXPOSE 123 456
//
// and not treat "123 456" as a single word.
// Note that: EXPOSE "$foo" and EXPOSE $foo are not the same thing.
// Quotes will cause it to still be treated as single word.
var allowWordExpansion = map[string]bool{
	command.Expose: true,
}

// Step represents the input Env and the output command after all
// post processing of the command arguments is done.
type Step struct {
	Env []string

	Command  string
	Args     []string
	Flags    []string
	Attrs    map[string]bool
	Message  string
	Heredocs []buildkitparser.Heredoc
	Original string
}

// Resolve transforms a parsed Dockerfile line into a command to execute,
// resolving any arguments.
//
// Almost all nodes will have this structure:
// Child[Node, Node, Node] where Child is from parser.Node.Children and each
// node comes from parser.Node.Next. This forms a "line" with a statement and
// arguments and we process them in this normalized form by hitting
// evaluateTable with the leaf nodes of the command and the Builder object.
//
// ONBUILD is a special case; in this case the parser will emit:
// Child[Node, Child[Node, Node...]] where the first node is the literal
// "onbuild" and the child entrypoint is the command of the ONBUILD statement,
// such as `RUN` in ONBUILD RUN foo. There is special case logic in here to
// deal with that, at least until it becomes more of a general concern with new
// features.
func (b *Step) Resolve(ast *parser.Node) error {
	b.Heredocs = ast.Heredocs
	cmd := ast.Value
	upperCasedCmd := strings.ToUpper(cmd)

	// To ensure the user is given a decent error message if the platform
	// on which the daemon is running does not support a builder command.
	if err := platformSupports(strings.ToLower(cmd)); err != nil {
		return err
	}

	attrs := ast.Attributes
	original := ast.Original
	flags := ast.Flags
	strList := []string{}
	msg := upperCasedCmd

	if len(ast.Flags) > 0 {
		msg += " " + strings.Join(ast.Flags, " ")
	}

	if cmd == "onbuild" {
		if ast.Next == nil {
			return fmt.Errorf("ONBUILD requires at least one argument")
		}
		ast = ast.Next.Children[0]
		strList = append(strList, ast.Value)
		msg += " " + ast.Value

		if len(ast.Flags) > 0 {
			msg += " " + strings.Join(ast.Flags, " ")
		}

	}

	// count the number of nodes that we are going to traverse first
	// so we can pre-create the argument and message array. This speeds up the
	// allocation of those list a lot when they have a lot of arguments
	cursor := ast
	var n int
	for cursor.Next != nil {
		cursor = cursor.Next
		n++
	}
	msgList := make([]string, n)

	var i int
	envs := b.Env
	for ast.Next != nil {
		ast = ast.Next
		str := ast.Value
		if replaceEnvAllowed[cmd] {
			var err error
			var words []string

			if allowWordExpansion[cmd] {
				words, err = ProcessWords(str, envs)
				if err != nil {
					return err
				}
				strList = append(strList, words...)
			} else {
				str, err = ProcessWord(str, envs)
				if err != nil {
					return err
				}
				strList = append(strList, str)
			}
		} else {
			strList = append(strList, str)
		}
		msgList[i] = ast.Value
		i++
	}

	msg += " " + strings.Join(msgList, " ")

	b.Message = msg
	b.Command = cmd
	b.Args = strList
	b.Original = original
	b.Attrs = attrs
	b.Flags = flags
	return nil
}
