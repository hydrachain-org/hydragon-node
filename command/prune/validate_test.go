package prune

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDataDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "prune_validate_test")
	require.NoError(t, err)

	t.Cleanup(func() { os.RemoveAll(dir) })

	// Create trie/ and blockchain/ subdirs
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "trie"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "blockchain"), 0755))

	return dir
}

func TestValidateParams_MissingDataDir(t *testing.T) {
	params = pruneParams{
		DataDir:    "",
		TargetPath: "/tmp/target",
	}

	err := validateParams()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--data-dir")
}

func TestValidateParams_MissingTargetPath(t *testing.T) {
	params = pruneParams{
		DataDir:    "/tmp/some-dir",
		TargetPath: "",
	}

	err := validateParams()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--target-path")
}

func TestValidateParams_NonexistentDataDir(t *testing.T) {
	params = pruneParams{
		DataDir:    "/nonexistent/path/xyz123",
		TargetPath: "/tmp/target",
	}

	err := validateParams()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trie directory not found")
}

func TestValidateParams_MissingTrieSubdir(t *testing.T) {
	dir, err := os.MkdirTemp("", "prune_no_trie")
	require.NoError(t, err)

	defer os.RemoveAll(dir)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "blockchain"), 0755))

	params = pruneParams{
		DataDir:    dir,
		TargetPath: "/tmp/target",
	}

	err = validateParams()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trie directory not found")
}

func TestValidateParams_MissingBlockchainSubdir(t *testing.T) {
	dir, err := os.MkdirTemp("", "prune_no_blockchain")
	require.NoError(t, err)

	defer os.RemoveAll(dir)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "trie"), 0755))

	params = pruneParams{
		DataDir:    dir,
		TargetPath: "/tmp/target",
	}

	err = validateParams()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blockchain directory not found")
}

func TestValidateParams_TargetEqualsSource(t *testing.T) {
	dir := setupTestDataDir(t)

	params = pruneParams{
		DataDir:    dir,
		TargetPath: filepath.Join(dir, "trie"),
	}

	err := validateParams()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be the same")
}

func TestValidateParams_TargetNonEmpty(t *testing.T) {
	dir := setupTestDataDir(t)

	targetDir, err := os.MkdirTemp("", "prune_nonempty_target")
	require.NoError(t, err)

	defer os.RemoveAll(targetDir)

	// Put a file in target to make it non-empty
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "dummy"), []byte("data"), 0644))

	params = pruneParams{
		DataDir:    dir,
		TargetPath: targetDir,
	}

	err = validateParams()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not empty")
}

func TestValidateParams_ValidParams(t *testing.T) {
	dir := setupTestDataDir(t)

	params = pruneParams{
		DataDir:    dir,
		TargetPath: filepath.Join(dir, "trie_new"),
	}

	err := validateParams()
	assert.NoError(t, err)
}
