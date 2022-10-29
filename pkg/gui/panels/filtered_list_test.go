package panels

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilteredListGet(t *testing.T) {
	tests := []struct {
		f    *FilteredList[int]
		args int
		want int
	}{
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			args: 1,
			want: 2,
		},
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			args: 2,
			want: 3,
		},
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{1}},
			args: 0,
			want: 2,
		},
	}

	for _, tt := range tests {
		if got := tt.f.Get(tt.args); got != tt.want {
			t.Errorf("FilteredList.Get() = %v, want %v", got, tt.want)
		}
	}
}

func TestFilteredListLen(t *testing.T) {
	tests := []struct {
		f    *FilteredList[int]
		want int
	}{
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			want: 3,
		},
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{1}},
			want: 1,
		},
	}

	for _, tt := range tests {
		if got := tt.f.Len(); got != tt.want {
			t.Errorf("FilteredList.Len() = %v, want %v", got, tt.want)
		}
	}
}

func TestFilteredListFilter(t *testing.T) {
	tests := []struct {
		f    *FilteredList[int]
		args func(int, int) bool
		want *FilteredList[int]
	}{
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			args: func(i int, _ int) bool { return i%2 == 0 },
			want: &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{1}},
		},
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			args: func(i int, _ int) bool { return i%2 == 1 },
			want: &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 2}},
		},
	}

	for _, tt := range tests {
		tt.f.Filter(tt.args)
		assert.EqualValues(t, tt.f.indices, tt.want.indices)
	}
}

func TestFilteredListSort(t *testing.T) {
	tests := []struct {
		f    *FilteredList[int]
		args func(int, int) bool
		want *FilteredList[int]
	}{
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			args: func(i int, j int) bool { return i < j },
			want: &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
		},
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			args: func(i int, j int) bool { return i > j },
			want: &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{2, 1, 0}},
		},
	}

	for _, tt := range tests {
		tt.f.Sort(tt.args)
		assert.EqualValues(t, tt.f.indices, tt.want.indices)
	}
}

func TestFilteredListGetIndex(t *testing.T) {
	tests := []struct {
		f    *FilteredList[int]
		args int
		want int
	}{
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			args: 1,
			want: 0,
		},
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			args: 2,
			want: 1,
		},
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{1}},
			args: 0,
			want: -1,
		},
	}

	for _, tt := range tests {
		if got := tt.f.GetIndex(tt.args); got != tt.want {
			t.Errorf("FilteredList.GetIndex() = %v, want %v", got, tt.want)
		}
	}
}

func TestFilteredListGetItems(t *testing.T) {
	tests := []struct {
		f    *FilteredList[int]
		want []int
	}{
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			want: []int{1, 2, 3},
		},
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{1}},
			want: []int{2},
		},
	}

	for _, tt := range tests {
		got := tt.f.GetItems()
		assert.EqualValues(t, got, tt.want)
	}
}

func TestFilteredListSetItems(t *testing.T) {
	tests := []struct {
		f    *FilteredList[int]
		args []int
		want *FilteredList[int]
	}{
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{0, 1, 2}},
			args: []int{4, 5, 6},
			want: &FilteredList[int]{allItems: []int{4, 5, 6}, indices: []int{0, 1, 2}},
		},
		{
			f:    &FilteredList[int]{allItems: []int{1, 2, 3}, indices: []int{1}},
			args: []int{4},
			want: &FilteredList[int]{allItems: []int{4}, indices: []int{0}},
		},
	}

	for _, tt := range tests {
		tt.f.SetItems(tt.args)
		assert.EqualValues(t, tt.f.indices, tt.want.indices)
		assert.EqualValues(t, tt.f.allItems, tt.want.allItems)
	}
}
