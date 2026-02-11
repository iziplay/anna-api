package isbn

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTo13(t *testing.T) {
	assert.Equal(t, "9780306406157", To13("0306406152"))
	assert.Equal(t, "9780140449112", To13("0140449116"))
	assert.Equal(t, "9780201616224", To13("020161622X"))
	assert.Equal(t, "", To13(""))
	assert.Equal(t, "", To13("123"))
	assert.Equal(t, "", To13("abcdefghij"))
}

func TestTo10(t *testing.T) {
	assert.Equal(t, "0306406152", To10("9780306406157"))
	assert.Equal(t, "0140449116", To10("9780140449112"))
	assert.Equal(t, "020161622X", To10("9780201616224"))
	assert.Equal(t, "", To10(""))
	assert.Equal(t, "", To10("123"))
	assert.Equal(t, "", To10("9790000000000"))
	assert.Equal(t, "", To10("978abcdefghi"))
}

func TestRoundTrip(t *testing.T) {
	assert.Equal(t, "0306406152", To10(To13("0306406152")))
	assert.Equal(t, "0140449116", To10(To13("0140449116")))
	assert.Equal(t, "020161622X", To10(To13("020161622X")))
}
