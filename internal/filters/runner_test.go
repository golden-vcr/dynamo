package filters

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_parseColor(t *testing.T) {
	got, err := parseColor("#ffee01\nsomething else\n")
	assert.NoError(t, err)
	assert.Equal(t, "#ffee01", got)

	got, err = parseColor("hello world")
	assert.Error(t, err)
	assert.Equal(t, "", got)
}
