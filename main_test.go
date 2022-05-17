package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func runLetterKnife(t *testing.T, args []string, filename string) *bytes.Buffer {
	f, err := os.Open(filepath.Join("testdata", filename))
	assert.NilError(t, err)

	var buf bytes.Buffer
	lk := LetterKnife{In: f, Out: &buf}
	err = lk.ParseFlags(args)
	assert.NilError(t, err)

	lk.Run()

	return &buf
}

func TestRunMain(t *testing.T) {
	buf := runLetterKnife(t, []string{"--plain"}, "multipart.eml")
	assert.Check(t, cmp.Contains(buf.String(), "Hello! 😊"))

	buf = runLetterKnife(t, []string{}, "plain.eml")
	t.Log(buf.String())
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
