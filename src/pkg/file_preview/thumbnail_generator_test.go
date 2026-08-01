package filepreview

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockThumbGenerator struct {
	calls int
}

func (m *mockThumbGenerator) supportsExt(ext string) bool {
	return ext == ".mock"
}

func (m *mockThumbGenerator) kind() string {
	return "thumb:mock"
}

func (m *mockThumbGenerator) generateThumbnail(_ string, outputPathWithoutExt string) (string, error) {
	m.calls++
	out := outputPathWithoutExt + thumbOutputExt
	return out, os.WriteFile(out, []byte("thumb-bytes"), 0o644)
}

func TestThumbnailGeneratorPersistentCache(t *testing.T) {
	cacheRoot := t.TempDir()

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "clip.mock")
	require.NoError(t, os.WriteFile(src, []byte("source"), 0o644))

	mock := &mockThumbGenerator{}
	gen, err := NewThumbnailGenerator(true, cacheRoot, 1)
	require.NoError(t, err)
	gen.generators = []thumbnailGeneratorInterface{mock}

	// First call generates
	path1, err := gen.GetThumbnailOrGenerate(src)
	require.NoError(t, err)
	require.FileExists(t, path1)
	assert.Equal(t, 1, mock.calls)

	// Second call in same instance hits memory cache
	path2, err := gen.GetThumbnailOrGenerate(src)
	require.NoError(t, err)
	assert.Equal(t, path1, path2)
	assert.Equal(t, 1, mock.calls)

	// New generator instance should hit disk cache without regenerating
	mock2 := &mockThumbGenerator{}
	gen2, err := NewThumbnailGenerator(true, cacheRoot, 1)
	require.NoError(t, err)
	gen2.generators = []thumbnailGeneratorInterface{mock2}

	path3, err := gen2.GetThumbnailOrGenerate(src)
	require.NoError(t, err)
	assert.Equal(t, path1, path3)
	assert.Equal(t, 0, mock2.calls, "should use disk cache")

	// CleanUp must not wipe disk cache
	require.NoError(t, gen.CleanUp())
	require.FileExists(t, path1)

	mock3 := &mockThumbGenerator{}
	gen3, err := NewThumbnailGenerator(true, cacheRoot, 1)
	require.NoError(t, err)
	gen3.generators = []thumbnailGeneratorInterface{mock3}
	path4, err := gen3.GetThumbnailOrGenerate(src)
	require.NoError(t, err)
	assert.Equal(t, path1, path4)
	assert.Equal(t, 0, mock3.calls)
}

func TestThumbnailGeneratorTempFallback(t *testing.T) {
	mock := &mockThumbGenerator{}
	gen, err := NewThumbnailGenerator(false, "", 0)
	require.NoError(t, err)
	gen.generators = []thumbnailGeneratorInterface{mock}

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "clip.mock")
	require.NoError(t, os.WriteFile(src, []byte("source"), 0o644))

	path1, err := gen.GetThumbnailOrGenerate(src)
	require.NoError(t, err)
	require.FileExists(t, path1)
	assert.Equal(t, 1, mock.calls)

	require.NoError(t, gen.CleanUp())
	_, err = os.Stat(path1)
	assert.True(t, os.IsNotExist(err), "temp thumb should be removed on CleanUp")
}
