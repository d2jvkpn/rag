package tests

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBasePath(t *testing.T) {
	var v string

	v = filepath.Join("", "/")
	fmt.Println("v: ", v)
	assert.Equal(t, v, "/")

	v = filepath.Join("/", "/")
	fmt.Println("v: ", v)
	assert.Equal(t, v, "/")

	v = filepath.Join("/a/", "/")
	fmt.Println("v: ", v)
	assert.Equal(t, v, "/a")
}
