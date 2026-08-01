package filepreview

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockThumbGenerator struct {
	calls atomic.Int32
	delay time.Duration
}

func (m *mockThumbGenerator) supportsExt(ext string) bool {
	return ext == ".mock"
}

func (m *mockThumbGenerator) kind() string {
	return "thumb:mock"
}

func (m *mockThumbGenerator) generateThumbnail(_ string, outputPathWithoutExt string) (string, error) {
	m.calls.Add(1)
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
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
	assert.Equal(t, int32(1), mock.calls.Load())

	// Second call in same instance hits memory cache
	path2, err := gen.GetThumbnailOrGenerate(src)
	require.NoError(t, err)
	assert.Equal(t, path1, path2)
	assert.Equal(t, int32(1), mock.calls.Load())

	// New generator instance should hit disk cache without regenerating
	mock2 := &mockThumbGenerator{}
	gen2, err := NewThumbnailGenerator(true, cacheRoot, 1)
	require.NoError(t, err)
	gen2.generators = []thumbnailGeneratorInterface{mock2}

	path3, err := gen2.GetThumbnailOrGenerate(src)
	require.NoError(t, err)
	assert.Equal(t, path1, path3)
	assert.Equal(t, int32(0), mock2.calls.Load(), "should use disk cache")

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
	assert.Equal(t, int32(0), mock3.calls.Load())
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
	assert.Equal(t, int32(1), mock.calls.Load())

	require.NoError(t, gen.CleanUp())
	_, err = os.Stat(path1)
	assert.True(t, os.IsNotExist(err), "temp thumb should be removed on CleanUp")
}

func TestGetThumbnailOrGenerateSingleFlight(t *testing.T) {
	gen, err := NewThumbnailGenerator(false, "", 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.CleanUp() })

	mock := &mockThumbGenerator{delay: 50 * time.Millisecond}
	gen.generators = []thumbnailGeneratorInterface{mock}

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "clip.mock")
	require.NoError(t, os.WriteFile(src, []byte("source"), 0o644))

	const n = 8
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)

	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = gen.GetThumbnailOrGenerate(src)
		}(i)
	}
	wg.Wait()

	for i := range n {
		require.NoError(t, errs[i], "goroutine %d", i)
		require.FileExists(t, results[i])
		assert.Equal(t, results[0], results[i], "all callers should share the same path")
	}
	assert.Equal(t, int32(1), mock.calls.Load(), "only one generation should run")

	// After completion, cache serves without regenerating
	path, err := gen.GetThumbnailOrGenerate(src)
	require.NoError(t, err)
	assert.Equal(t, results[0], path)
	assert.Equal(t, int32(1), mock.calls.Load())
}

func TestGetThumbnailOrGenerateCacheHit(t *testing.T) {
	gen, err := NewThumbnailGenerator(false, "", 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = gen.CleanUp() })

	mock := &mockThumbGenerator{}
	gen.generators = []thumbnailGeneratorInterface{mock}

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "clip.mock")
	require.NoError(t, os.WriteFile(src, []byte("source"), 0o644))

	path1, err := gen.GetThumbnailOrGenerate(src)
	require.NoError(t, err)
	path2, err := gen.GetThumbnailOrGenerate(src)
	require.NoError(t, err)

	assert.Equal(t, path1, path2)
	assert.Equal(t, int32(1), mock.calls.Load())
}
