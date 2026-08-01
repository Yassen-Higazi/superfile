package cache

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yorukot/superfile/src/pkg/utils"
)

const (
	indexFileName = ".index.json"
	// evictTargetPercent is the fill level to reach when eviction runs.
	// Eviction starts when total > maxBytes and stops at
	// <= maxBytes*evictTargetPercent/percentBase, leaving headroom so the next
	// few Commits do not immediately re-trigger.
	evictTargetPercent = 80
	percentBase        = 100
)

type indexEntry struct {
	Name       string `json:"name"` // relative path under root
	Size       int64  `json:"size"`
	LastAccess int64  `json:"last_access"` // unix nanoseconds
}

// cacheItem is stored in the LRU list. Front = most recently used, Back = oldest.
type cacheItem struct {
	key   string
	entry indexEntry
}

func itemOf(el *list.Element) *cacheItem {
	item, ok := el.Value.(*cacheItem)
	if !ok || item == nil {
		panic("file cache: invalid LRU list element")
	}
	return item
}

// FileCache is a content-addressed on-disk blob cache with max-size LRU eviction.
type FileCache struct {
	root     string
	maxBytes int64
	mu       sync.Mutex
	// index maps key → list element; list order tracks LRU
	index map[string]*list.Element
	lru   *list.List
	total int64
	// evicting ensures at most one eviction goroutine runs at a time
	evicting atomic.Bool
}

// NewFileCache creates (if needed) root and loads the on-disk index.
// maxBytes <= 0 means no size limit (LRU eviction disabled).
func NewFileCache(root string, maxBytes int64) (*FileCache, error) {
	if root == "" {
		return nil, errors.New("cache root must not be empty")
	}
	if err := os.MkdirAll(root, utils.ConfigDirPerm); err != nil {
		return nil, fmt.Errorf("create cache root: %w", err)
	}

	c := &FileCache{
		root:     root,
		maxBytes: maxBytes,
		index:    make(map[string]*list.Element),
		lru:      list.New(),
	}
	if err := c.loadIndex(); err != nil {
		return nil, err
	}
	return c, nil
}

// Root returns the cache directory.
func (c *FileCache) Root() string {
	return c.root
}

// Path returns the preferred absolute path for a new blob written under key.
// Callers that need a prefix (e.g. pdftoppm) can use Path(key) without extension
// and write Path(key)+".jpg"; then Commit(key, finalPath).
func (c *FileCache) Path(key string) string {
	return filepath.Join(c.root, key)
}

// Get returns the absolute path of a cached blob if present, and updates LRU.
// Stat and list updates are done outside/inside separate lock sections so the
// mutex is not held during disk I/O.
func (c *FileCache) Get(key string) (string, bool) {
	c.mu.Lock()
	el, ok := c.index[key]
	if !ok {
		c.mu.Unlock()
		return "", false
	}
	item := itemOf(el)
	name := item.entry.Name
	path := filepath.Join(c.root, name)
	c.mu.Unlock()

	// File missing on disk (manual delete, crash mid-write) → drop stale entry.
	if _, err := os.Stat(path); err != nil {
		c.mu.Lock()
		// Only remove if this is still the same blob (not replaced by Commit).
		if el2, still := c.index[key]; still {
			if itemOf(el2).entry.Name == name {
				c.removeElementLocked(el2)
			}
		}
		c.mu.Unlock()
		return "", false
	}

	c.mu.Lock()
	if el2, still := c.index[key]; still {
		c.touchLocked(el2) // mark as most recently used
	}
	c.mu.Unlock()
	return path, true
}

// Commit registers a file already written under the cache root for key.
// filePath must be the absolute path of the produced blob.
func (c *FileCache) Commit(key, filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("commit cache entry: %w", err)
	}
	if info.IsDir() {
		return errors.New("commit cache entry: path is a directory")
	}

	// Require the blob to live under the cache root (no path escape).
	absRoot, err := filepath.Abs(c.root)
	if err != nil {
		return fmt.Errorf("commit cache entry: %w", err)
	}
	absFile, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("commit cache entry: %w", err)
	}
	rel, err := filepath.Rel(absRoot, absFile)
	if err != nil || !filepath.IsLocal(rel) {
		return errors.New("commit cache entry: file must be under cache root")
	}

	size := info.Size()
	now := time.Now().UnixNano()

	c.mu.Lock()
	var stalePath string
	if el, exists := c.index[key]; exists {
		// Update existing key: adjust total, reuse list node.
		old := itemOf(el)
		c.total -= old.entry.Size
		// Different filename for same key → delete the old blob after unlock.
		if old.entry.Name != rel {
			stalePath = filepath.Join(c.root, old.entry.Name)
		}
		old.entry = indexEntry{Name: rel, Size: size, LastAccess: now}
		c.total += size
		c.lru.MoveToFront(el)
	} else {
		// New key: insert as most recently used.
		item := &cacheItem{
			key: key,
			entry: indexEntry{
				Name:       rel,
				Size:       size,
				LastAccess: now,
			},
		}
		c.index[key] = c.lru.PushFront(item)
		c.total += size
	}

	needEviction := c.maxBytes > 0 && c.total > c.maxBytes
	c.mu.Unlock()

	// Index is memory-only until Flush; blobs on disk are the source of truth.
	if stalePath != "" {
		_ = os.Remove(stalePath)
	}
	if needEviction {
		c.scheduleEviction()
	}
	return nil
}

// Put copies or moves srcPath into the cache under key (as basename of key).
func (c *FileCache) Put(key, srcPath string) error {
	dest := c.Path(key)
	if err := copyFile(srcPath, dest); err != nil {
		return err
	}
	return c.Commit(key, dest)
}

// Clear removes all cache blobs and the index file.
func (c *FileCache) Clear() error {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return err
	}

	// Reset memory first so concurrent Gets miss while files are deleted.
	c.mu.Lock()
	c.index = make(map[string]*list.Element)
	c.lru = list.New()
	c.total = 0
	c.mu.Unlock()

	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(c.root, e.Name()))
	}
	return nil
}

// Size returns total tracked blob bytes.
func (c *FileCache) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

// Flush writes the in-memory index (LRU metadata) to disk.
// Blob files are the source of truth; the index only preserves access order.
// Call on clean shutdown. Safe after a crash: next load rebuilds from the directory.
func (c *FileCache) Flush() error {
	c.mu.Lock()
	indexData, err := json.Marshal(c.snapshotIndexLocked())
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return writeIndexFile(c.root, indexData)
}

// touchLocked marks el as most recently used. Caller must hold c.mu.
func (c *FileCache) touchLocked(el *list.Element) {
	c.lru.MoveToFront(el)
	itemOf(el).entry.LastAccess = time.Now().UnixNano()
}

// removeElementLocked drops el from the LRU and index. Caller must hold c.mu.
func (c *FileCache) removeElementLocked(el *list.Element) {
	item := itemOf(el)
	c.total -= item.entry.Size
	if c.total < 0 {
		c.total = 0
	}
	delete(c.index, item.key)
	c.lru.Remove(el)
}

// snapshotIndexLocked builds a plain map for JSON persistence. Caller must hold c.mu.
func (c *FileCache) snapshotIndexLocked() map[string]indexEntry {
	out := make(map[string]indexEntry, len(c.index))
	for k, el := range c.index {
		out[k] = itemOf(el).entry
	}
	return out
}

// setIndexLocked replaces index+lru from a plain map (e.g. after load).
// Caller must hold c.mu (or be single-threaded during New).
func (c *FileCache) setIndexLocked(idx map[string]indexEntry) {
	c.index = make(map[string]*list.Element, len(idx))
	c.lru = list.New()
	c.total = 0

	// Sort oldest → newest so PushFront leaves oldest at Back
	type kv struct {
		key   string
		entry indexEntry
	}
	items := make([]kv, 0, len(idx))
	for k, e := range idx {
		items = append(items, kv{key: k, entry: e})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].entry.LastAccess == items[j].entry.LastAccess {
			return items[i].key < items[j].key
		}
		return items[i].entry.LastAccess < items[j].entry.LastAccess
	})
	for _, it := range items {
		item := &cacheItem{key: it.key, entry: it.entry}
		c.index[it.key] = c.lru.PushFront(item)
		c.total += it.entry.Size
	}
}

// scheduleEviction runs LRU eviction in the background.
// CompareAndSwap keeps only one eviction goroutine at a time (single-flight).
func (c *FileCache) scheduleEviction() {
	if c.maxBytes <= 0 {
		return
	}
	if !c.evicting.CompareAndSwap(false, true) {
		return // already running
	}
	go func() {
		c.evict()
		c.evicting.Store(false)
	}()
}

// evict enforces maxBytes using LRU. Disk I/O runs outside the mutex.
// Starts when total > maxBytes; continues until total <= evict target
// (evictTargetPercent of maxBytes). Does not write the index; Flush on exit
// persists LRU order.
func (c *FileCache) evict() {
	c.mu.Lock()
	if c.total <= c.maxBytes {
		c.mu.Unlock()
		return
	}

	target := c.maxBytes * evictTargetPercent / percentBase
	var toRemove []string
	for c.total > target && c.lru.Len() > 0 {
		el := c.lru.Back() // oldest
		if el == nil {
			break
		}
		item := itemOf(el)
		toRemove = append(toRemove, filepath.Join(c.root, item.entry.Name))
		c.removeElementLocked(el)
	}
	c.mu.Unlock()

	for _, p := range toRemove {
		_ = os.Remove(p)
	}
}

// loadIndex builds the in-memory index from blob files on disk, optionally
// overlaying LastAccess times from .index.json when present.
func (c *FileCache) loadIndex() error {
	meta, err := readIndexMeta(c.root)
	if err != nil && !os.IsNotExist(err) {
		// Corrupt index: ignore and rebuild from directory only
		meta = nil
	}

	idx, err := scanBlobDir(c.root, meta)
	if err != nil {
		return err
	}
	c.setIndexLocked(idx)
	return nil
}

func readIndexMeta(root string) (map[string]indexEntry, error) {
	data, err := os.ReadFile(filepath.Join(root, indexFileName))
	if err != nil {
		return nil, err
	}
	var idx map[string]indexEntry
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	if idx == nil {
		idx = make(map[string]indexEntry)
	}
	return idx, nil
}

// scanBlobDir lists blob files under root. Disk is source of truth for presence.
// meta (from .index.json) only supplies LastAccess so LRU order survives restarts.
func scanBlobDir(root string, meta map[string]indexEntry) (map[string]indexEntry, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	idx := make(map[string]indexEntry)
	now := time.Now().UnixNano()
	for _, e := range entries {
		if e.Name() == indexFileName || e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		// Blobs are stored as <key> or <key>.ext (e.g. hash.jpg).
		key := name
		if ext := filepath.Ext(name); ext != "" {
			key = name[:len(name)-len(ext)]
		}
		if _, exists := idx[key]; exists {
			key = name // collision: keep full filename as key
		}

		lastAccess := now // unknown access time after crash without index
		if meta != nil {
			if m, ok := meta[key]; ok {
				lastAccess = m.LastAccess
			}
		}

		idx[key] = indexEntry{
			Name:       name,
			Size:       info.Size(),
			LastAccess: lastAccess,
		}
	}
	return idx, nil
}

// writeIndexFile writes via temp + rename for atomic replace.
func writeIndexFile(root string, data []byte) error {
	tmp := filepath.Join(root, indexFileName+".tmp")
	if err := os.WriteFile(tmp, data, utils.ConfigFilePerm); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(root, indexFileName))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if mkdirErr := os.MkdirAll(filepath.Dir(dst), utils.ConfigDirPerm); mkdirErr != nil {
		return mkdirErr
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, utils.ConfigFilePerm)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, copyErr := io.Copy(out, in); copyErr != nil {
		return copyErr
	}
	return out.Close()
}
