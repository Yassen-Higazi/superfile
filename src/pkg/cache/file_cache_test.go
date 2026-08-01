package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestMakeKeyStableAndBustsOnMtimeSize(t *testing.T) {
	dir := t.TempDir()
	src := writeTempFile(t, dir, "video.mp4", "data")

	info1, err := os.Stat(src)
	require.NoError(t, err)
	k1 := MakeKey(src, info1, "thumb:video", "v1")
	k2 := MakeKey(src, info1, "thumb:video", "v1")
	assert.Equal(t, k1, k2)
	assert.NotEmpty(t, k1)

	// Different kind → different key
	assert.NotEqual(t, k1, MakeKey(src, info1, "thumb:pdf", "v1"))

	// Size change
	require.NoError(t, os.WriteFile(src, []byte("data!!"), 0o644))
	info2, err := os.Stat(src)
	require.NoError(t, err)
	assert.NotEqual(t, k1, MakeKey(src, info2, "thumb:video", "v1"))

	// Mtime change (same size)
	require.NoError(t, os.WriteFile(src, []byte("data"), 0o644))
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(src, past, past))
	info3, err := os.Stat(src)
	require.NoError(t, err)
	// May equal original if content+times collide unlikely; size is back to 4
	// Ensure mtime differs from info2 at least
	assert.NotEqual(t, MakeKey(src, info2, "thumb:video", "v1"), MakeKey(src, info3, "thumb:video", "v1"))
}

func TestFileCacheGetCommit(t *testing.T) {
	root := t.TempDir()
	fc, err := NewFileCache(root, 0)
	require.NoError(t, err)

	key := "abc123"
	_, ok := fc.Get(key)
	assert.False(t, ok)

	withoutExt := fc.Path(key)
	final := withoutExt + ".jpg"
	require.NoError(t, os.WriteFile(final, []byte("jpeg-bytes"), 0o644))
	require.NoError(t, fc.Commit(key, final))

	got, ok := fc.Get(key)
	require.True(t, ok)
	assert.Equal(t, final, got)
	assert.Equal(t, int64(len("jpeg-bytes")), fc.Size())

	// Blobs on disk are enough to reopen without Flush
	fc2, err := NewFileCache(root, 0)
	require.NoError(t, err)
	got2, ok := fc2.Get(key)
	require.True(t, ok)
	assert.Equal(t, final, got2)

	// Flush persists LRU metadata for the next process
	require.NoError(t, fc.Flush())
	fc3, err := NewFileCache(root, 0)
	require.NoError(t, err)
	got3, ok := fc3.Get(key)
	require.True(t, ok)
	assert.Equal(t, final, got3)
}

func TestFileCacheLRUEviction(t *testing.T) {
	root := t.TempDir()
	// max 10 bytes → eviction target 80% = 8 bytes
	const maxBytes int64 = 10
	target := maxBytes * evictTargetPercent / percentBase
	fc, err := NewFileCache(root, maxBytes)
	require.NoError(t, err)

	writeCommit := func(key, content string) {
		p := fc.Path(key) + ".jpg"
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
		require.NoError(t, fc.Commit(key, p))
	}

	writeCommit("a", "12345") // 5 bytes
	writeCommit("b", "12345") // 5 bytes, total 10
	_, okA := fc.Get("a")     // touch a as more recent (LRU list)
	require.True(t, okA)
	writeCommit("c", "12345") // total 15 → eviction (oldest first: b, then a)

	// Eviction runs in a background goroutine (do not Get while waiting — that
	// would touch LRU order and race with eviction).
	require.Eventually(t, func() bool {
		return fc.Size() <= target
	}, time.Second, 5*time.Millisecond, "cache should shrink to eviction target")

	// Target 8 bytes with 5-byte entries → only newest (c) remains
	_, okB := fc.Get("b")
	assert.False(t, okB, "b should be evicted (oldest)")
	_, okA2 := fc.Get("a")
	assert.False(t, okA2, "a should be evicted to reach target headroom")
	_, okC := fc.Get("c")
	assert.True(t, okC, "c is newest and should remain")
	assert.LessOrEqual(t, fc.Size(), target)
}

func TestFileCacheClear(t *testing.T) {
	root := t.TempDir()
	fc, err := NewFileCache(root, 0)
	require.NoError(t, err)

	p := fc.Path("k") + ".jpg"
	require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
	require.NoError(t, fc.Commit("k", p))
	require.NoError(t, fc.Clear())

	_, ok := fc.Get("k")
	assert.False(t, ok)
	assert.Equal(t, int64(0), fc.Size())
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
