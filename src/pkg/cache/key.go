package cache

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"io"
	"os"
)

// MakeKey builds a stable content-addressed key from source file identity and
// cache kind/options. When the source mtime or size changes, the key changes
// and the previous blob is left as an orphan until LRU eviction or Clear.
func MakeKey(absPath string, info os.FileInfo, kind string, opts ...string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, absPath)
	_, _ = h.Write([]byte{0})

	_ = binary.Write(h, binary.LittleEndian, info.ModTime().UnixNano())
	_ = binary.Write(h, binary.LittleEndian, info.Size())

	_, _ = h.Write([]byte{0})
	_, _ = io.WriteString(h, kind)
	for _, opt := range opts {
		_, _ = h.Write([]byte{0})
		_, _ = io.WriteString(h, opt)
	}

	sum := h.Sum(nil)
	// 16 bytes → 26-char base32 (no padding), same length style as yazi
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:16])
}
