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

func (m *mockThumbGenerator) generateThumbnail(_ string, outputPathWithoutExt string) (string, error) {
	m.calls.Add(1)
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	out := outputPathWithoutExt + thumbOutputExt
	return out, os.WriteFile(out, []byte("thumb-bytes"), 0o644)
}

func TestGetThumbnailOrGenerateSingleFlight(t *testing.T) {
	gen, err := NewThumbnailGenerator()
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
	gen, err := NewThumbnailGenerator()
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
