package main

import (
	"testing"

	"gotest.tools/v3/assert"
)

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
