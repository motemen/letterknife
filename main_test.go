package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestRunMain_PrintContent(t *testing.T) {
	out := runLetterKnife(t, []string{"--plain"}, "multipart.eml")
	assert.Check(t, cmp.Contains(out.String(), "Hello! 😊"))

	out = runLetterKnife(t, []string{}, "plain.eml")
	in, err := os.ReadFile("testdata/plain.eml")
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(string(in), out.String()))
}

func TestRunMain_SaveFile(t *testing.T) {
	t.Run("saves as .eml when no part selected", func(t *testing.T) {
		out := runLetterKnife(t, []string{"--save-file"}, "plain.eml")
		assert.Check(t, cmp.Regexp(`(?m)\.eml$`, out.String()))

		source, err := os.ReadFile("testdata/plain.eml")
		assert.NilError(t, err)

		savedContent, err := os.ReadFile(strings.Split(out.String(), "\n")[0])
		assert.NilError(t, err)

		assert.Check(t, cmp.Equal(string(source), string(savedContent)))
	})

	t.Run("saves as .html and .txt", func(t *testing.T) {
		buf := runLetterKnife(t, []string{"--select-part=*", "--save-file"}, "multipart.eml")
		lines := buf.String()
		assert.Check(t, cmp.Regexp(`(?m)\.txt$`, lines))
		assert.Check(t, cmp.Regexp(`(?m)\.html$`, lines))
	})

	t.Run("saves attachment with original filename", func(t *testing.T) {
		buf := runLetterKnife(t, []string{"--select-attachment=*", "--save-file"}, "multipart.eml")
		lines := buf.String()
		assert.Check(t, cmp.Regexp(`(?m)4x4\.png$`, lines))
	})
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
		assert.Equal(t, test.regexp, r.String())
	}
}
