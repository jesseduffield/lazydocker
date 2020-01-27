package utils

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"

	"github.com/fatih/color"
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
	if padding < len(uncoloredStr) {
		return str
	}
	return str + strings.Repeat(" ", padding-len(uncoloredStr))
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

type Displayable interface {
	GetDisplayStrings(bool) []string
}

type RenderListConfig struct {
	IsFocused bool
	Header    []string
}

func IsFocused(isFocused bool) func(c *RenderListConfig) {
	return func(c *RenderListConfig) {
		c.IsFocused = isFocused
	}
}

func WithHeader(header []string) func(c *RenderListConfig) {
	return func(c *RenderListConfig) {
		c.Header = header
	}
}

// RenderList takes a slice of items, confirms they implement the Displayable
// interface, then generates a list of their displaystrings to write to a panel's
// buffer
func RenderList(slice interface{}, options ...func(*RenderListConfig)) (string, error) {
	config := &RenderListConfig{}
	for _, option := range options {
		option(config)
	}

	s := reflect.ValueOf(slice)
	if s.Kind() != reflect.Slice {
		return "", errors.New("RenderList given a non-slice type")
	}

	displayables := make([]Displayable, s.Len())

	for i := 0; i < s.Len(); i++ {
		value, ok := s.Index(i).Interface().(Displayable)
		if !ok {
			return "", errors.New("item does not implement the Displayable interface")
		}
		displayables[i] = value
	}

	return renderDisplayableList(displayables, *config)
}

// renderDisplayableList takes a list of displayable items, obtains their display
// strings via GetDisplayStrings() and then returns a single string containing
// each item's string representation on its own line, with appropriate horizontal
// padding between the item's own strings
func renderDisplayableList(items []Displayable, config RenderListConfig) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	stringArrays := getDisplayStringArrays(items, config.IsFocused)
	if len(config.Header) > 0 {
		stringArrays = append([][]string{config.Header}, stringArrays...)
	}

	return RenderTable(stringArrays)
}

// RenderTable takes an array of string arrays and returns a table containing the values
func RenderTable(stringArrays [][]string) (string, error) {
	if !displayArraysAligned(stringArrays) {
		return "", errors.New("Each item must return the same number of strings to display")
	}

	padWidths := getPadWidths(stringArrays)
	paddedDisplayStrings := getPaddedDisplayStrings(stringArrays, padWidths)

	return strings.Join(paddedDisplayStrings, "\n"), nil
}

// Decolorise strips a string of color
func Decolorise(str string) string {
	re := regexp.MustCompile(`\x1B\[([0-9]{1,2}(;[0-9]{1,2})?)?[m|K]`)
	return re.ReplaceAllString(str, "")
}

func getPadWidths(stringArrays [][]string) []int {
	if len(stringArrays[0]) <= 1 {
		return []int{}
	}
	padWidths := make([]int, len(stringArrays[0])-1)
	for i := range padWidths {
		for _, strings := range stringArrays {
			uncoloredString := Decolorise(strings[i])
			if len(uncoloredString) > padWidths[i] {
				padWidths[i] = len(uncoloredString)
			}
		}
	}
	return padWidths
}

func getPaddedDisplayStrings(stringArrays [][]string, padWidths []int) []string {
	paddedDisplayStrings := make([]string, len(stringArrays))
	for i, stringArray := range stringArrays {
		if len(stringArray) == 0 {
			continue
		}
		for j, padWidth := range padWidths {
			paddedDisplayStrings[i] += WithPadding(stringArray[j], padWidth) + " "
		}
		paddedDisplayStrings[i] += stringArray[len(padWidths)]
	}
	return paddedDisplayStrings
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

func getDisplayStringArrays(displayables []Displayable, isFocused bool) [][]string {
	stringArrays := make([][]string, len(displayables))
	for i, item := range displayables {
		stringArrays[i] = item.GetDisplayStrings(isFocused)
	}
	return stringArrays
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
	template.Must(template.New("").Parse(str)).Execute(&buf, object)
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
	return gocui.ColorWhite
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
