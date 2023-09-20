package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/mattn/go-runewidth"

	// "github.com/jesseduffield/yaml"

	"github.com/fatih/color"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/printer"
)

// SplitLines takes a multiline string and splits it on newlines
// currently we are also stripping \r's which may have adverse effects for
// windows users (but no issues have been raised yet)
func SplitLines(multilineString string) []string {
	multilineString = strings.Replace(multilineString, "\r", "", -1)
	if multilineString == "" || multilineString == "\n" {
		return make([]string, 0)
	}
	lines := strings.Split(multilineString, "\n")
	if lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	return lines
}

// WithPadding pads a string as much as you want
func WithPadding(str string, padding int) string {
	uncoloredStr := Decolorise(str)
	if padding < runewidth.StringWidth(uncoloredStr) {
		return str
	}
	return str + strings.Repeat(" ", padding-runewidth.StringWidth(uncoloredStr))
}

// ColoredString takes a string and a colour attribute and returns a colored
// string with that attribute
func ColoredString(str string, colorAttribute color.Attribute) string {
	// fatih/color does not have a color.Default attribute, so unless we fork that repo the only way for us to express that we don't want to color a string different to the terminal's default is to not call the function in the first place, but that's annoying when you want a streamlined code path. Because I'm too lazy to fork the repo right now, we'll just assume that by FgWhite you really mean Default, for the sake of supporting users with light themed terminals.
	if colorAttribute == color.FgWhite {
		return str
	}
	colour := color.New(colorAttribute)
	return ColoredStringDirect(str, colour)
}

// ColoredYamlString takes an YAML formatted string and returns a colored string
// with colors hardcoded as:
// keys: cyan
// Booleans: magenta
// Numbers: yellow
// Strings: green
func ColoredYamlString(str string) string {
	format := func(attr color.Attribute) string {
		return fmt.Sprintf("%s[%dm", "\x1b", attr)
	}
	tokens := lexer.Tokenize(str)
	var p printer.Printer
	p.Bool = func() *printer.Property {
		return &printer.Property{
			Prefix: format(color.FgMagenta),
			Suffix: format(color.Reset),
		}
	}
	p.Number = func() *printer.Property {
		return &printer.Property{
			Prefix: format(color.FgYellow),
			Suffix: format(color.Reset),
		}
	}
	p.MapKey = func() *printer.Property {
		return &printer.Property{
			Prefix: format(color.FgCyan),
			Suffix: format(color.Reset),
		}
	}
	p.String = func() *printer.Property {
		return &printer.Property{
			Prefix: format(color.FgGreen),
			Suffix: format(color.Reset),
		}
	}
	return p.PrintTokens(tokens)
}

// MultiColoredString takes a string and an array of colour attributes and returns a colored
// string with those attributes
func MultiColoredString(str string, colorAttribute ...color.Attribute) string {
	colour := color.New(colorAttribute...)
	return ColoredStringDirect(str, colour)
}

// ColoredStringDirect used for aggregating a few color attributes rather than
// just sending a single one
func ColoredStringDirect(str string, colour *color.Color) string {
	return colour.SprintFunc()(fmt.Sprint(str))
}

// NormalizeLinefeeds - Removes all Windows and Mac style line feeds
func NormalizeLinefeeds(str string) string {
	str = strings.Replace(str, "\r\n", "\n", -1)
	str = strings.Replace(str, "\r", "", -1)
	return str
}

// Loader dumps a string to be displayed as a loader
func Loader() string {
	characters := "|/-\\"
	now := time.Now()
	nanos := now.UnixNano()
	index := nanos / 50000000 % int64(len(characters))
	return characters[index : index+1]
}

// ResolvePlaceholderString populates a template with values
func ResolvePlaceholderString(str string, arguments map[string]string) string {
	for key, value := range arguments {
		str = strings.Replace(str, "{{"+key+"}}", value, -1)
	}
	return str
}

// Max returns the maximum of two integers
func Max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

// RenderTable takes an array of string arrays and returns a table containing the values
func RenderTable(rows [][]string) (string, error) {
	if len(rows) == 0 {
		return "", nil
	}
	if !displayArraysAligned(rows) {
		return "", errors.New("Each item must return the same number of strings to display")
	}

	columnPadWidths := getPadWidths(rows)
	paddedDisplayRows := getPaddedDisplayStrings(rows, columnPadWidths)

	return strings.Join(paddedDisplayRows, "\n"), nil
}

// Decolorise strips a string of color
func Decolorise(str string) string {
	re := regexp.MustCompile(`\x1B\[([0-9]{1,2}(;[0-9]{1,2})?)?[mK]`)
	return re.ReplaceAllString(str, "")
}

func getPadWidths(rows [][]string) []int {
	if len(rows[0]) <= 1 {
		return []int{}
	}
	columnPadWidths := make([]int, len(rows[0])-1)
	for i := range columnPadWidths {
		for _, cells := range rows {
			uncoloredCell := Decolorise(cells[i])

			if runewidth.StringWidth(uncoloredCell) > columnPadWidths[i] {
				columnPadWidths[i] = runewidth.StringWidth(uncoloredCell)
			}
		}
	}
	return columnPadWidths
}

func getPaddedDisplayStrings(rows [][]string, columnPadWidths []int) []string {
	paddedDisplayRows := make([]string, len(rows))
	for i, cells := range rows {
		for j, columnPadWidth := range columnPadWidths {
			paddedDisplayRows[i] += WithPadding(cells[j], columnPadWidth) + " "
		}
		paddedDisplayRows[i] += cells[len(columnPadWidths)]
	}
	return paddedDisplayRows
}

// displayArraysAligned returns true if every string array returned from our
// list of displayables has the same length
func displayArraysAligned(stringArrays [][]string) bool {
	for _, strings := range stringArrays {
		if len(strings) != len(stringArrays[0]) {
			return false
		}
	}
	return true
}

func FormatBinaryBytes(b int) string {
	n := float64(b)
	units := []string{"B", "kiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB", "YiB"}
	for _, unit := range units {
		if n > math.Pow(2, 10) {
			n /= math.Pow(2, 10)
		} else {
			val := fmt.Sprintf("%.2f%s", n, unit)
			if val == "0.00B" {
				return "0B"
			}
			return val
		}
	}
	return "a lot"
}

func FormatDecimalBytes(b int) string {
	n := float64(b)
	units := []string{"B", "kB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}
	for _, unit := range units {
		if n > math.Pow(10, 3) {
			n /= math.Pow(10, 3)
		} else {
			val := fmt.Sprintf("%.2f%s", n, unit)
			if val == "0.00B" {
				return "0B"
			}
			return val
		}
	}
	return "a lot"
}

func ApplyTemplate(str string, object interface{}) string {
	var buf bytes.Buffer
	_ = template.Must(template.New("").Parse(str)).Execute(&buf, object)
	return buf.String()
}

// GetGocuiAttribute gets the gocui color attribute from the string
func GetGocuiAttribute(key string) gocui.Attribute {
	colorMap := map[string]gocui.Attribute{
		"default":   gocui.ColorDefault,
		"black":     gocui.ColorBlack,
		"red":       gocui.ColorRed,
		"green":     gocui.ColorGreen,
		"yellow":    gocui.ColorYellow,
		"blue":      gocui.ColorBlue,
		"magenta":   gocui.ColorMagenta,
		"cyan":      gocui.ColorCyan,
		"white":     gocui.ColorWhite,
		"bold":      gocui.AttrBold,
		"reverse":   gocui.AttrReverse,
		"underline": gocui.AttrUnderline,
	}
	value, present := colorMap[key]
	if present {
		return value
	}
	return gocui.ColorDefault
}

// GetColorAttribute gets the color attribute from the string
func GetColorAttribute(key string) color.Attribute {
	colorMap := map[string]color.Attribute{
		"default":   color.FgWhite,
		"black":     color.FgBlack,
		"red":       color.FgRed,
		"green":     color.FgGreen,
		"yellow":    color.FgYellow,
		"blue":      color.FgBlue,
		"magenta":   color.FgMagenta,
		"cyan":      color.FgCyan,
		"white":     color.FgWhite,
		"bold":      color.Bold,
		"underline": color.Underline,
	}
	value, present := colorMap[key]
	if present {
		return value
	}
	return color.FgWhite
}

// WithShortSha returns a command but with a shorter SHA. in the terminal we're all used to 10 character SHAs but under the hood they're actually 64 characters long. No need including all the characters when we're just displaying a command
func WithShortSha(str string) string {
	split := strings.Split(str, " ")
	for i, word := range split {
		// good enough proxy for now
		if len(word) == 64 {
			split[i] = word[0:10]
		}
	}
	return strings.Join(split, " ")
}

// FormatMapItem is for displaying items in a map
func FormatMapItem(padding int, k string, v interface{}) string {
	return fmt.Sprintf("%s%s %v\n", strings.Repeat(" ", padding), ColoredString(k+":", color.FgYellow), fmt.Sprintf("%v", v))
}

// FormatMap is for displaying a map
func FormatMap(padding int, m map[string]string) string {
	if len(m) == 0 {
		return "none\n"
	}

	output := "\n"

	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		output += FormatMapItem(padding, key, m[key])
	}

	return output
}

type multiErr []error

func (m multiErr) Error() string {
	var b bytes.Buffer
	b.WriteString("encountered multiple errors:")
	for _, err := range m {
		b.WriteString("\n\t... " + err.Error())
	}
	return b.String()
}

func CloseMany(closers []io.Closer) error {
	errs := make([]error, 0, len(closers))
	for _, c := range closers {
		err := c.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return multiErr(errs)
	}
	return nil
}

func SafeTruncate(str string, limit int) string {
	if len(str) > limit {
		return str[0:limit]
	} else {
		return str
	}
}

func IsValidHexValue(v string) bool {
	if len(v) != 4 && len(v) != 7 {
		return false
	}

	if v[0] != '#' {
		return false
	}

	for _, char := range v[1:] {
		switch char {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f', 'A', 'B', 'C', 'D', 'E', 'F':
			continue
		default:
			return false
		}
	}

	return true
}

// Style used on menu items that open another menu
func OpensMenuStyle(str string) string {
	return ColoredString(fmt.Sprintf("%s...", str), color.FgMagenta)
}

// MarshalIntoYaml gets any json-tagged data and marshal it into yaml saving original json structure.
// Useful for structs from 3rd-party libs without yaml tags.
func MarshalIntoYaml(data interface{}) ([]byte, error) {
	return marshalIntoFormat(data, "yaml")
}

func marshalIntoFormat(data interface{}, format string) ([]byte, error) {
	// First marshal struct->json to get the resulting structure declared by json tags
	dataJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	switch format {
	case "json":
		return dataJSON, err
	case "yaml":
		// Use Unmarshal->Marshal hack to convert json into yaml with the original structure preserved
		var dataMirror yaml.MapSlice
		if err := yaml.Unmarshal(dataJSON, &dataMirror); err != nil {
			return nil, err
		}
		return yaml.Marshal(dataMirror)
	default:
		return nil, errors.New(fmt.Sprintf("Unsupported detailization format: %s", format))
	}
}
