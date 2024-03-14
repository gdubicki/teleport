package main

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

const (
	testDataDir = "./testdata"
	snapshotDir = testDataDir + "/snapshots"
)

func TestSnapshot(t *testing.T) {
	destDir := t.TempDir()

	err := run(testDataDir, destDir)
	require.NoError(t, err)

	expectedFiles, err := os.ReadDir(snapshotDir)
	require.NoError(t, err)

	actualFiles, err := os.ReadDir(destDir)
	require.NoError(t, err)

	require.Equal(t, len(expectedFiles), len(actualFiles))

	for _, expectedFile := range expectedFiles {
		expectedContent, err := os.ReadFile(filepath.Join(snapshotDir, expectedFile.Name()))
		require.NoError(t, err)
		actualContent, err := os.ReadFile(filepath.Join(destDir, expectedFile.Name()))
		require.NoError(t, err)
		require.Equal(t, string(expectedContent), string(actualContent), "content does not match for file %s", expectedFile)
	}
}
