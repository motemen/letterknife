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

func runLetterKnife(t *testing.T, args []string, filename string) (*bytes.Buffer, error) {
	f, err := os.Open(filepath.Join("testdata", filename))
	assert.NilError(t, err)

	var buf bytes.Buffer
	lk := LetterKnife{}
	err = lk.ParseFlags(args)
	assert.NilError(t, err)

	err = lk.Run(f, &buf)

	return &buf, err
}

func TestRunMain_PrintContent(t *testing.T) {
	t.Run("selecting text/plain part from multipart content", func(t *testing.T) {
		out, err := runLetterKnife(t, []string{"--plain"}, "multipart.eml")
		assert.NilError(t, err)
		assert.Check(t, cmp.Contains(out.String(), "Hello! 😊"))
	})

	t.Run("prints raw input if none is selected", func(t *testing.T) {
		out, err := runLetterKnife(t, []string{}, "plain.eml")
		assert.NilError(t, err)
		in, err := os.ReadFile("testdata/plain.eml")
		assert.NilError(t, err)
		assert.Check(t, cmp.Equal(string(in), out.String()))
	})

	t.Run("decodes quoted-printable contents on singlepart content", func(t *testing.T) {
		out, err := runLetterKnife(t, []string{"--html"}, "singlepart-quotedprintable.eml")
		assert.NilError(t, err)
		assert.Check(t, cmp.Equal(
			`<div>Hello</div><div><a href="https://www.example.com/">Example Link</a></div>`+"\n\n",
			out.String(),
		))
	})
}

func TestRunMain_SaveFile(t *testing.T) {
	t.Run("saves as .eml when no part selected", func(t *testing.T) {
		out, err := runLetterKnife(t, []string{"--save-file"}, "plain.eml")
		assert.NilError(t, err)
		assert.Check(t, cmp.Regexp(`(?m)\.eml$`, out.String()))

		source, err := os.ReadFile("testdata/plain.eml")
		assert.NilError(t, err)

		savedContent, err := os.ReadFile(strings.Split(out.String(), "\n")[0])
		assert.NilError(t, err)

		assert.Check(t, cmp.Equal(string(source), string(savedContent)))
	})

	t.Run("saves as .html and .txt", func(t *testing.T) {
		buf, err := runLetterKnife(t, []string{"--select-part=*", "--save-file"}, "multipart.eml")
		assert.NilError(t, err)
		lines := buf.String()
		assert.Check(t, cmp.Regexp(`(?m)\.txt$`, lines))
		assert.Check(t, cmp.Regexp(`(?m)\.html$`, lines))
	})

	t.Run("saves attachment with original filename", func(t *testing.T) {
		buf, err := runLetterKnife(t, []string{"--select-attachment=*", "--save-file"}, "multipart.eml")
		assert.NilError(t, err)
		lines := buf.String()
		assert.Check(t, cmp.Regexp(`(?m)4x4\.png$`, lines))
	})
}

func TestRunMain_MatchHeader(t *testing.T) {
	_, err := runLetterKnife(t, []string{"--match-header", "Subject:*mail ✉️"}, "plain.eml")
	assert.NilError(t, err)
	_, err = runLetterKnife(t, []string{"--match-header", "Subject:Hello️"}, "plain.eml")
	assert.ErrorIs(t, err, ErrHeaderMatchFailed)
}

func TestRunMain_MatchAddress(t *testing.T) {
	_, err := runLetterKnife(t, []string{"--from", "motemen@gmail.com"}, "plain.eml")
	assert.NilError(t, err)
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
