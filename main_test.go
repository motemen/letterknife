package main

import (
	"bytes"
	"os"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestRunMain(t *testing.T) {
	f, err := os.Open("testdata/example.eml")
	assert.NilError(t, err)

	var buf bytes.Buffer
	lk := LetterKnife{In: f, Out: &buf}
	err = lk.ParseFlags([]string{"--plain"})
	assert.NilError(t, err)

	lk.Run()

	assert.Check(t, cmp.Contains(buf.String(), "Hello! 😊"))
}

func TestRegexpFromPattern(t *testing.T) {
	tests := []struct {
		pattern string
		regexp  string
	}{
		{"*@gmail.com", `^.+?@gmail\.com$`},
		{"/foobar/", `foobar`},
	}

	for _, test := range tests {
		r, err := regexpFromPattern(test.pattern)
		assert.NilError(t, err)
		assert.Equal(t, r.String(), test.regexp)
	}
}
