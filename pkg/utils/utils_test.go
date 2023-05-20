package utils

import (
	"testing"

	"github.com/go-errors/errors"
	"github.com/stretchr/testify/assert"
)

// TestSplitLines is a function.
func TestSplitLines(t *testing.T) {
	type scenario struct {
		multilineString string
		expected        []string
	}

	scenarios := []scenario{
		{
			"",
			[]string{},
		},
		{
			"\n",
			[]string{},
		},
		{
			"hello world !\nhello universe !\n",
			[]string{
				"hello world !",
				"hello universe !",
			},
		},
	}

	for _, s := range scenarios {
		assert.EqualValues(t, s.expected, SplitLines(s.multilineString))
	}
}

// TestWithPadding is a function.
func TestWithPadding(t *testing.T) {
	type scenario struct {
		str      string
		padding  int
		expected string
	}

	scenarios := []scenario{
		{
			"hello world !",
			1,
			"hello world !",
		},
		{
			"hello world !",
			14,
			"hello world ! ",
		},
	}

	for _, s := range scenarios {
		assert.EqualValues(t, s.expected, WithPadding(s.str, s.padding))
	}
}

// TestNormalizeLinefeeds is a function.
func TestNormalizeLinefeeds(t *testing.T) {
	type scenario struct {
		byteArray []byte
		expected  []byte
	}
	scenarios := []scenario{
		{
			// \r\n
			[]byte{97, 115, 100, 102, 13, 10},
			[]byte{97, 115, 100, 102, 10},
		},
		{
			// bash\r\nblah
			[]byte{97, 115, 100, 102, 13, 10, 97, 115, 100, 102},
			[]byte{97, 115, 100, 102, 10, 97, 115, 100, 102},
		},
		{
			// \r
			[]byte{97, 115, 100, 102, 13},
			[]byte{97, 115, 100, 102},
		},
		{
			// \n
			[]byte{97, 115, 100, 102, 10},
			[]byte{97, 115, 100, 102, 10},
		},
	}

	for _, s := range scenarios {
		assert.EqualValues(t, string(s.expected), NormalizeLinefeeds(string(s.byteArray)))
	}
}

// TestResolvePlaceholderString is a function.
func TestResolvePlaceholderString(t *testing.T) {
	type scenario struct {
		templateString string
		arguments      map[string]string
		expected       string
	}

	scenarios := []scenario{
		{
			"",
			map[string]string{},
			"",
		},
		{
			"hello",
			map[string]string{},
			"hello",
		},
		{
			"hello {{arg}}",
			map[string]string{},
			"hello {{arg}}",
		},
		{
			"hello {{arg}}",
			map[string]string{"arg": "there"},
			"hello there",
		},
		{
			"hello",
			map[string]string{"arg": "there"},
			"hello",
		},
		{
			"{{nothing}}",
			map[string]string{"nothing": ""},
			"",
		},
		{
			"{{}} {{ this }} { should not throw}} an {{{{}}}} error",
			map[string]string{
				"blah": "blah",
				"this": "won't match",
			},
			"{{}} {{ this }} { should not throw}} an {{{{}}}} error",
		},
	}

	for _, s := range scenarios {
		assert.EqualValues(t, s.expected, ResolvePlaceholderString(s.templateString, s.arguments))
	}
}

// TestDisplayArraysAligned is a function.
func TestDisplayArraysAligned(t *testing.T) {
	type scenario struct {
		input    [][]string
		expected bool
	}

	scenarios := []scenario{
		{
			[][]string{{"", ""}, {"", ""}},
			true,
		},
		{
			[][]string{{""}, {"", ""}},
			false,
		},
	}

	for _, s := range scenarios {
		assert.EqualValues(t, s.expected, displayArraysAligned(s.input))
	}
}

// TestGetPaddedDisplayStrings is a function.
func TestGetPaddedDisplayStrings(t *testing.T) {
	type scenario struct {
		stringArrays [][]string
		padWidths    []int
		expected     []string
	}

	scenarios := []scenario{
		{
			[][]string{{"a", "b"}, {"c", "d"}},
			[]int{1},
			[]string{"a b", "c d"},
		},
	}

	for _, s := range scenarios {
		assert.EqualValues(t, s.expected, getPaddedDisplayStrings(s.stringArrays, s.padWidths))
	}
}

// TestGetPadWidths is a function.
func TestGetPadWidths(t *testing.T) {
	type scenario struct {
		stringArrays [][]string
		expected     []int
	}

	scenarios := []scenario{
		{
			[][]string{{""}, {""}},
			[]int{},
		},
		{
			[][]string{{"a"}, {""}},
			[]int{},
		},
		{
			[][]string{{"aa", "b", "ccc"}, {"c", "d", "e"}},
			[]int{2, 1},
		},
	}

	for _, s := range scenarios {
		assert.EqualValues(t, s.expected, getPadWidths(s.stringArrays))
	}
}

func TestRenderTable(t *testing.T) {
	type scenario struct {
		input       [][]string
		expected    string
		expectedErr error
	}

	scenarios := []scenario{
		{
			input:       [][]string{{"a", "b"}, {"c", "d"}},
			expected:    "a b\nc d",
			expectedErr: nil,
		},
		{
			input:       [][]string{{"aaaa", "b"}, {"c", "d"}},
			expected:    "aaaa b\nc    d",
			expectedErr: nil,
		},
		{
			input:       [][]string{{"a"}, {"c", "d"}},
			expected:    "",
			expectedErr: errors.New("Each item must return the same number of strings to display"),
		},
	}

	for _, s := range scenarios {
		output, err := RenderTable(s.input)
		assert.EqualValues(t, s.expected, output)
		if s.expectedErr != nil {
			assert.EqualError(t, err, s.expectedErr.Error())
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestMarshalIntoFormat(t *testing.T) {
	type innerData struct {
		Foo int    `json:"foo"`
		Bar string `json:"bar"`
		Baz bool   `json:"baz"`
	}
	type data struct {
		Qux  int       `json:"quz"`
		Quux innerData `json:"quux"`
	}

	type scenario struct {
		input       interface{}
		format      string
		expected    []byte
		expectedErr error
	}

	scenarios := []scenario{
		{
			input:  data{1, innerData{2, "foo", true}},
			format: "json",
			expected: []byte(`{
  "quz": 1,
  "quux": {
    "foo": 2,
    "bar": "foo",
    "baz": true
  }
}`),
			expectedErr: nil,
		},
		{
			input:  data{1, innerData{2, "foo", true}},
			format: "yaml",
			expected: []byte(`quz: 1
quux:
  bar: foo
  baz: true
  foo: 2
`),
			expectedErr: nil,
		},
		{
			input:       data{1, innerData{2, "foo", true}},
			format:      "xml",
			expected:    nil,
			expectedErr: errors.New("Unsupported detailization format: xml"),
		},
	}

	for _, s := range scenarios {
		output, err := marshalIntoFormat(s.input, s.format)
		assert.EqualValues(t, s.expected, output)
		if s.expectedErr != nil {
			assert.EqualError(t, err, s.expectedErr.Error())
		} else {
			assert.NoError(t, err)
		}
	}
}
