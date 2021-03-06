package ravendb

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestFirstNonNilString(t *testing.T) {
	tests := [][]string{
		{"", "", ""},
		{"", "foo", "foo"},
		{"foo", "", "foo"},
		{"foo", "bar", "foo"},
	}

	strToPtr := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	for _, test := range tests {
		s1 := strToPtr(test[0])
		s2 := strToPtr(test[1])
		got := firstNonNilString(s1, s2)
		exp := strToPtr(test[2])
		if got == nil || exp == nil {
			assert.Nil(t, got)
			assert.Nil(t, exp)
		} else {
			assert.Equal(t, *exp, *got)
		}
	}
}

func TestMin(t *testing.T) {
	tests := [][]int{
		{0, 0, 0},
		{1, 0, 0},
		{0, 1, 0},
		{-1, 1, -1},
		{-1, -3, -3},
		{3, 8, 3},
	}
	for _, test := range tests {
		got := min(test[0], test[1])
		exp := test[2]
		assert.Equal(t, exp, got, "test: %#v", test)
	}
}

func TestFirstNonZero(t *testing.T) {
	tests := [][]int{
		{0, 0, 0},
		{1, 0, 1},
		{0, 1, 1},
		{0, -81, -81},
		{5, -11, 5},
	}
	for _, test := range tests {
		got := firstNonZero(test[0], test[1])
		exp := test[2]
		assert.Equal(t, exp, got)
	}
}

func TestBuilderWriteInt(t *testing.T) {
	tests := []int{-123, -1, 0, 1, 123}
	for _, test := range tests {
		b := &strings.Builder{}
		builderWriteInt(b, test)
		got := b.String()
		exp := fmt.Sprintf("%d", test)
		assert.Equal(t, exp, got, "test: %d", test)
	}
}

func TestPanicIf(t *testing.T) {
	defer func() {
		if err := recover(); err == nil {
			assert.Fail(t, "expected to panic")
		} else {
			assert.Equal(t, err.(error).Error(), "should fail")
		}
	}()
	panicIf(true, "should fail")
}

func TestDbg(t *testing.T) {
	stdoutSaved := os.Stdout
	verboseSaved := LogVerbose
	path := "util_test_tmp.txt"
	f, err := os.Create(path)
	assert.NoError(t, err)
	os.Stdout = f
	LogVerbose = true

	dbg("v: %d\n", 5)

	os.Stdout = stdoutSaved
	LogVerbose = verboseSaved
	err = f.Close()
	assert.NoError(t, err)

	d, err := ioutil.ReadFile(path)
	assert.NoError(t, err)
	s := string(d)
	assert.Equal(t, s, "v: 5\n")
	_ = os.Remove(path)
}
