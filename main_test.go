package gometalinter

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTempDir(t *testing.T) (string, func()) {
	tmpdir, err := ioutil.TempDir("", "test-expand-paths")
	require.NoError(t, err)

	oldwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpdir))

	return tmpdir, func() {
		os.RemoveAll(tmpdir)
		require.NoError(t, os.Chdir(oldwd))
	}
}

func TestRelativePackagePath(t *testing.T) {
	var testcases = []struct {
		dir      string
		expected string
	}{
		{
			dir:      "/abs/path",
			expected: "/abs/path",
		},
		{
			dir:      ".",
			expected: ".",
		},
		{
			dir:      "./foo",
			expected: "./foo",
		},
		{
			dir:      "relative/path",
			expected: "./relative/path",
		},
	}

	for _, testcase := range testcases {
		assert.Equal(t, testcase.expected, relativePackagePath(testcase.dir))
	}
}

func TestResolvePathsNoPaths(t *testing.T) {
	paths := resolvePaths(nil, nil)
	assert.Equal(t, []string{"."}, paths)
}

func TestResolvePathsNoExpands(t *testing.T) {
	// Non-expanded paths should not be filtered by the skip path list
	paths := resolvePaths([]string{".", "foo", "foo/bar"}, []string{"foo/bar"})
	expected := []string{".", "./foo", "./foo/bar"}
	assert.Equal(t, expected, paths)
}

func TestResolvePathsWithExpands(t *testing.T) {
	tmpdir, cleanup := setupTempDir(t)
	defer cleanup()

	mkGoFile(t, tmpdir, "file.go")
	mkDir(t, tmpdir, "exclude")
	mkDir(t, tmpdir, "other", "exclude")
	mkDir(t, tmpdir, "include")
	mkDir(t, tmpdir, "include", "foo")
	mkDir(t, tmpdir, "duplicate")
	mkDir(t, tmpdir, ".exclude")
	mkDir(t, tmpdir, "include", ".exclude")
	mkDir(t, tmpdir, "_exclude")
	mkDir(t, tmpdir, "include", "_exclude")

	filterPaths := []string{"exclude", "other/exclude"}
	paths := resolvePaths([]string{"./...", "foo", "duplicate"}, filterPaths)

	expected := []string{
		".",
		"./duplicate",
		"./foo",
		"./include",
		"./include/foo",
	}
	assert.Equal(t, expected, paths)
}

func TestPathFilter(t *testing.T) {
	skip := []string{"exclude", "skip.go"}
	pathFilter := newPathFilter(skip)

	var testcases = []struct {
		path     string
		expected bool
	}{
		{path: "exclude", expected: true},
		{path: "something/skip.go", expected: true},
		{path: "skip.go", expected: true},
		{path: ".git", expected: true},
		{path: "_ignore", expected: true},
		{path: "include.go", expected: false},
		{path: ".", expected: false},
		{path: "..", expected: false},
	}

	for _, testcase := range testcases {
		assert.Equal(t, testcase.expected, pathFilter(testcase.path), testcase.path)
	}
}

func TestAddPath(t *testing.T) {
	paths := []string{"existing"}
	assert.Equal(t, paths, addPath(paths, "existing"))
	expected := []string{"existing", "new"}
	assert.Equal(t, expected, addPath(paths, "new"))
}
