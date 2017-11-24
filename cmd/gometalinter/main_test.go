package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alecthomas/gometalinter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/alecthomas/kingpin.v3-unstable"
)

func mkDir(t *testing.T, paths ...string) {
	fullPath := filepath.Join(paths...)
	require.NoError(t, os.MkdirAll(fullPath, 0755))
	mkGoFile(t, fullPath, "file.go")
}

func mkGoFile(t *testing.T, path string, filename string) {
	content := []byte("package foo")
	err := ioutil.WriteFile(filepath.Join(path, filename), content, 0644)
	require.NoError(t, err)
}

func TestLoadConfigWithDeadline(t *testing.T) {
	originalConfig := *gometalinter.Configuration
	defer func() { gometalinter.Configuration = &originalConfig }()

	tmpfile, err := ioutil.TempFile("", "test-config")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(`{"Deadline": "3m"}`))
	require.NoError(t, err)
	require.NoError(t, tmpfile.Close())

	filename := tmpfile.Name()
	err = loadConfig(nil, &kingpin.ParseElement{Value: &filename}, nil)
	require.NoError(t, err)

	require.Equal(t, 3*time.Minute, gometalinter.Configuration.Deadline.Duration())
}

func TestDeadlineFlag(t *testing.T) {
	app := kingpin.New("test-app", "")
	setupFlags(app)
	_, err := app.Parse([]string{"--deadline", "2m"})
	require.NoError(t, err)
	require.Equal(t, 2*time.Minute, gometalinter.Configuration.Deadline.Duration())
}

func TestSetupFlagsLinterFlag(t *testing.T) {
	originalConfig := *gometalinter.Configuration
	defer func() { gometalinter.Configuration = &originalConfig }()

	app := kingpin.New("test-app", "")
	setupFlags(app)
	_, err := app.Parse([]string{"--linter", "a:b:c"})
	require.NoError(t, err)
	linter, ok := gometalinter.Configuration.Linters["a"]
	assert.True(t, ok)
	assert.Equal(t, "b", linter.Command)
	assert.Equal(t, "c", linter.Pattern)
}

func TestSetupFlagsConfigWithLinterString(t *testing.T) {
	originalConfig := *gometalinter.Configuration
	defer func() { gometalinter.Configuration = &originalConfig }()

	tmpfile, err := ioutil.TempFile("", "test-config")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(`{"Linters": {"linter": "command:path"} }`))
	require.NoError(t, err)
	require.NoError(t, tmpfile.Close())

	app := kingpin.New("test-app", "")
	setupFlags(app)

	_, err = app.Parse([]string{"--config", tmpfile.Name()})
	require.NoError(t, err)
	linter, ok := gometalinter.Configuration.Linters["linter"]
	assert.True(t, ok)
	assert.Equal(t, "command", linter.Command)
	assert.Equal(t, "path", linter.Pattern)
}

func TestSetupFlagsConfigWithLinterMap(t *testing.T) {
	originalConfig := *gometalinter.Configuration
	defer func() { gometalinter.Configuration = &originalConfig }()

	tmpfile, err := ioutil.TempFile("", "test-config")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(`{"Linters":
		{"linter":
			{ "Command": "command" }}}`))
	require.NoError(t, err)
	require.NoError(t, tmpfile.Close())

	app := kingpin.New("test-app", "")
	setupFlags(app)

	_, err = app.Parse([]string{"--config", tmpfile.Name()})
	require.NoError(t, err)
	linter, ok := gometalinter.Configuration.Linters["linter"]
	assert.True(t, ok)
	assert.Equal(t, "command", linter.Command)
	assert.Equal(t, "", linter.Pattern)
}

func TestSetupFlagsConfigAndLinterFlag(t *testing.T) {
	originalConfig := *gometalinter.Configuration
	defer func() { gometalinter.Configuration = &originalConfig }()

	tmpfile, err := ioutil.TempFile("", "test-config")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(`{"Linters":
		{"linter": { "Command": "some-command" }}}`))
	require.NoError(t, err)
	require.NoError(t, tmpfile.Close())

	app := kingpin.New("test-app", "")
	setupFlags(app)

	_, err = app.Parse([]string{
		"--config", tmpfile.Name(),
		"--linter", "linter:command:pattern"})
	require.NoError(t, err)
	linter, ok := gometalinter.Configuration.Linters["linter"]
	assert.True(t, ok)
	assert.Equal(t, "command", linter.Command)
	assert.Equal(t, "pattern", linter.Pattern)
}
