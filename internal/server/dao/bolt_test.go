package dao

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDao_Register(t *testing.T) {
	d := New("my.db")
	err := d.Register("foo", "bar")
	require.NoError(t, err)
}

func TestDao_Login(t *testing.T) {
	d := New("my.db")
	err := d.Login("foo", "bar")
	require.NoError(t, err)
}
