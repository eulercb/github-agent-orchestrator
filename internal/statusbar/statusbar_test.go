package statusbar

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviderFallback(t *testing.T) {
	called := false
	p := NewProvider("", func() string {
		called = true
		return "fallback text"
	})

	p.Refresh()

	assert.True(t, called, "fallback function was not called")
	assert.Equal(t, "fallback text", p.Text())
}

func TestProviderCommand(t *testing.T) {
	p := NewProvider("echo hello", nil)
	p.Refresh()

	assert.Equal(t, "hello", p.Text())
}

func TestProviderCommandFailsFallsBack(t *testing.T) {
	p := NewProvider("false", func() string {
		return "fallback"
	})
	p.Refresh()

	assert.Equal(t, "fallback", p.Text())
}
