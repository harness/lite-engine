package gradle

import (
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var (
	tempFolder    = "temp/"
	cachedReport  = "testdata/profile-cached.html"
	fullRunReport = "testdata/profile-fullrun.html"
)

func TestParseSavings_Cached(t *testing.T) {
	err := createNestedDir("build/reports/profile")
	if err != nil {
		t.Fatal(err)
	}
	err = copyFile(cachedReport, "build/reports/profile/profile-cached.html")
	if err != nil {
		t.Fatal(err)
	}
	defer removeBaseDir() //nolint:errcheck

	workspace := getBaseDir()
	state, time, err := ParseSavings(workspace, logrus.New())
	assert.Nil(t, err)
	assert.Equal(t, types.OPTIMIZED, state)
	assert.Equal(t, 166190, time)
}

func TestParseSavings_FullRun(t *testing.T) {
	err := createNestedDir("build/reports/profile")
	if err != nil {
		t.Fatal(err)
	}
	err = copyFile(fullRunReport, "build/reports/profile/profile-fullrun.html")
	if err != nil {
		t.Fatal(err)
	}
	defer removeBaseDir() //nolint:errcheck

	workspace := getBaseDir()
	state, time, err := ParseSavings(workspace, logrus.New())
	assert.Nil(t, err)
	assert.Equal(t, types.FULL_RUN, state)
	assert.Equal(t, 166190, time)
}

func TestParseSavings_NoProfile(t *testing.T) {
	err := createNestedDir("build/reports/profile")
	if err != nil {
		t.Fatal(err)
	}
	defer removeBaseDir() //nolint:errcheck

	workspace := getBaseDir()
	_, _, err = ParseSavings(workspace, logrus.New())
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Errorf("no profiles present"), err)
}

func TestParseSavings_NoBuildDir(t *testing.T) {
	workspace := getBaseDir()
	_, _, err := ParseSavings(workspace, logrus.New())
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Errorf("no profiles present"), err)
}

func TestParseGradleVerseTimeMs(t *testing.T) {
	timeMap := map[string]int{
		"1d2h4m6.123s": 93846123,
		"2h4m6s":       7446000,
		"4m23.012s":    263012,
		"3.012s":       3012,
		"0.123s":       123,
	}
	for timeStr, expectedTimeMs := range timeMap {
		timeMs := parseGradleVerseTimeMs(timeStr)
		assert.Equal(t, expectedTimeMs, timeMs)
	}
}

func getBaseDir() string {
	wd, _ := os.Getwd()
	return fmt.Sprintf("%s/%s", wd, tempFolder)
}

// createNestedDir will create a nested directory relative to default temp directory
func createNestedDir(path string) error {
	absPath := getBaseDir() + path
	err := os.MkdirAll(absPath, 0777)
	if err != nil {
		return fmt.Errorf("could not create directory structure for testing: %s", err)
	}
	return nil
}

// removeBaseDir will clean up the temp directory
func removeBaseDir() error {
	err := os.RemoveAll(getBaseDir())
	if err != nil {
		return err
	}
	return nil
}

// copy file from src to relative dst in temp directory. Any existing file will be overwritten.
func copyFile(src, relDst string) error {
	dst := getBaseDir() + relDst
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}
