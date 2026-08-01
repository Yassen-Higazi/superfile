package filepreview

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/yorukot/superfile/src/internal/common"
	"github.com/yorukot/superfile/src/pkg/cache"
)

const thumbCacheVersion = "v1"

type thumbnailGeneratorInterface interface {
	supportsExt(ext string) bool
	kind() string
	generateThumbnail(inputPath string, outputPathWithoutExt string) (string, error)
}

type VideoGenerator struct{}

func newVideoGenerator() (*VideoGenerator, error) {
	if !isFFmpegInstalled() {
		return nil, errors.New("ffmpeg is not installed")
	}

	return &VideoGenerator{}, nil
}

func (g *VideoGenerator) supportsExt(ext string) bool {
	return common.VideoExtensions[strings.ToLower(ext)]
}

func (g *VideoGenerator) kind() string {
	return "thumb:video"
}

func (g *VideoGenerator) generateThumbnail(inputPath string, outputPathWithoutExt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), thumbGenerationTimeout)
	defer cancel()
	outputPath := outputPathWithoutExt + thumbOutputExt

	// ffmpeg -v warning -t 60 -hwaccel auto -an -sn -dn -skip_frame nokey -i input.mkv -vf scale='min(1024,iw)':'min(720,ih)':force_original_aspect_ratio=decrease:flags=fast_bilinear -vf "thumbnail" -frames:v 1 -y thumb.jpg
	ffmpeg := exec.CommandContext(
		ctx, "ffmpeg",
		"-v", "warning", // set log level to warning
		"-an",       // disable Audio stream
		"-sn",       // disable Subtitle stream
		"-dn",       // disable data stream
		"-t", "180", // process maximum 180s of the video (the first 3 min)
		"-hwaccel", "auto", // Use Hardware Acceleration if available
		"-skip_frame", "nokey", // skip non-key frames
		"-i", inputPath, // set input file
		"-vf", "thumbnail", // use ffmpeg default thumbnail filter
		"-frames:v", "1", // output only one frame (one image)
		"-f", "image2", // set format to image2
		"-fs", maxVideoFileSizeForThumb, // limit the max file size to match image previewer limit
		"-y", outputPath, // set the outputFile and overwrite it without confirmation if already exists
	)

	err := ffmpeg.Run()
	if err != nil {
		return "", fmt.Errorf("error generating video thumbnail, outputPath: %s : %w", outputPath, err)
	}

	return outputPath, nil
}

type pdfGenerator struct{}

func newPdfGenerator() (*pdfGenerator, error) {
	if !isPopplerInstalled() {
		return nil, errors.New("poppler is not installed")
	}

	return &pdfGenerator{}, nil
}

func (g *pdfGenerator) supportsExt(ext string) bool {
	return strings.ToLower(ext) == ".pdf"
}

func (g *pdfGenerator) kind() string {
	return "thumb:pdf"
}

func (g *pdfGenerator) generateThumbnail(inputPath string, outputPathWithoutExt string) (string, error) {
	outputPath := outputPathWithoutExt + thumbOutputExt
	ctx, cancel := context.WithTimeout(context.Background(), thumbGenerationTimeout)
	defer cancel()

	// pdftoppm -singlefile -png prefixFilename
	pdftoppm := exec.CommandContext(
		ctx, "pdftoppm",
		"-singlefile",        // output only the first page as image
		"-jpeg",              // Image extension
		inputPath,            // Set input file
		outputPathWithoutExt, // The output prefix. (pdftoppm will add the .jpg ext)
	)

	err := pdftoppm.Run()
	if err != nil {
		return "", fmt.Errorf("error generating pdf thumbnail, outputPath: %s : %w",
			outputPath, err)
	}

	return outputPath, nil
}

type psGenerator struct{}

func newPsGenerator() (*psGenerator, error) {
	if !isGhostscriptInstalled() {
		return nil, errors.New("ghostscript is not installed")
	}

	return &psGenerator{}, nil
}

func (g *psGenerator) supportsExt(ext string) bool {
	extension := strings.ToLower(ext)
	return extension == ".ps" || extension == ".eps"
}

func (g *psGenerator) kind() string {
	return "thumb:ps"
}

func (g *psGenerator) generateThumbnail(inputPath string, outputPathWithoutExt string) (string, error) {
	outputPath := outputPathWithoutExt + thumbOutputExt
	ctx, cancel := context.WithTimeout(context.Background(), thumbGenerationTimeout)
	defer cancel()

	// gs -dSAFER -dBATCH -dNOPAUSE -sPageList=1 -sDEVICE=jpeg -r150 -sOutputFile=output.jpg input.ps
	outputParam := "-sOutputFile=" + outputPath
	gs := exec.CommandContext(
		ctx, "gs",
		"-dSAFER", "-dBATCH", "-dNOPAUSE", // Standard GS operators
		"-sPageList=1",  // Output only the first page
		"-sDEVICE=jpeg", // Output format
		"-r150",         // Resolution (the same as for pdf)
		outputParam,     // Result (variable because of golangci-lint)
		inputPath,       // Input file
	)

	err := gs.Run()
	if err != nil {
		return "", fmt.Errorf("error generating ps thumbnail, outputPath: %s : %w",
			outputPath, err)
	}

	return outputPath, nil
}

type ThumbnailGenerator struct {
	// memoryCache is session-only L1: source path → thumbnail file path.
	// Empty on each new process; filled after a disk hit or fresh generation.
	memoryCache map[string]string
	// fileCache is persistent L2 on disk (nil when preview cache is disabled).
	// Keyed by MakeKey(absPath, mtime, size, kind) so thumbs survive restarts
	// when the source file has not changed.
	fileCache *cache.FileCache
	// Session temp dir used only when fileCache is nil
	tempDirectory string
	mu            sync.Mutex
	// sf ensures only one generation runs per source path at a time;
	// concurrent callers wait and share the result.
	sf         singleflight.Group
	generators []thumbnailGeneratorInterface
}

// NewThumbnailGenerator creates a thumbnail generator.
// When cacheEnabled is true, thumbnails are stored under cacheDir with a
// maxSizeMB disk limit. Otherwise a session-only temp directory is used.
func NewThumbnailGenerator(cacheEnabled bool, cacheDir string, maxSizeMB int) (*ThumbnailGenerator, error) {
	var fileCache *cache.FileCache
	var tempDir string

	if cacheEnabled {
		const bytesPerMB = 1024 * 1024
		maxBytes := int64(maxSizeMB) * bytesPerMB
		fc, err := cache.NewFileCache(cacheDir, maxBytes)
		if err != nil {
			slog.Debug("Could not create preview file cache, falling back to temp dir",
				"error", err)
		} else {
			fileCache = fc
		}
	}

	if fileCache == nil {
		tmp, err := os.MkdirTemp("", "superfiles-*")
		if err != nil {
			return nil, err
		}
		tempDir = tmp
	}

	generators := []thumbnailGeneratorInterface{}

	pdf, err := newPdfGenerator()
	if err != nil {
		slog.Debug("Error while trying to create pdfGenerator", "error", err)
	} else {
		generators = append(generators, pdf)
	}

	ps, err := newPsGenerator()
	if err != nil {
		slog.Debug("Error while trying to create psGenerator", "error", err)
	} else {
		generators = append(generators, ps)
	}

	video, err := newVideoGenerator()
	if err != nil {
		slog.Debug("Error while trying to create videoGenerator", "error", err)
	} else {
		generators = append(generators, video)
	}

	return &ThumbnailGenerator{
		memoryCache:   make(map[string]string),
		fileCache:     fileCache,
		tempDirectory: tempDir,
		generators:    generators,
	}, nil
}

func (g *ThumbnailGenerator) SupportsExt(ext string) bool {
	for i := range g.generators {
		if g.generators[i].supportsExt(ext) {
			return true
		}
	}

	return false
}

func (g *ThumbnailGenerator) GetThumbnailOrGenerate(path string) (string, error) {
	// L1: same session, already resolved this source path.
	if thumb, ok := g.cachedThumbnail(path); ok {
		return thumb, nil
	}

	// Single-flight: concurrent requests for the same path share one generation.
	v, err, _ := g.sf.Do(path, func() (any, error) {
		// Re-check L1: another flight may have finished between miss and Do.
		if thumb, ok := g.cachedThumbnail(path); ok {
			return thumb, nil
		}

		// May hit L2 disk cache or run ffmpeg/pdftoppm/gs.
		generatedThumbnailPath, err := g.generateThumbnail(path)
		if err != nil {
			return "", err
		}

		// Warm L1 for the rest of this session.
		g.mu.Lock()
		g.memoryCache[path] = generatedThumbnailPath
		g.mu.Unlock()

		return generatedThumbnailPath, nil
	})
	if err != nil {
		return "", err
	}

	thumb, ok := v.(string)
	if !ok {
		return "", errors.New("unexpected thumbnail result type")
	}
	return thumb, nil
}

// cachedThumbnail checks L1 only (memoryCache). Cross-session hits go through
func (g *ThumbnailGenerator) cachedThumbnail(path string) (string, bool) {
	g.mu.Lock()
	file, ok := g.memoryCache[path]
	g.mu.Unlock()

	if !ok {
		return "", false
	}

	if _, err := os.Stat(file); err != nil {
		g.mu.Lock()
		delete(g.memoryCache, path)
		g.mu.Unlock()
		return "", false
	}
	return file, true
}

func (g *ThumbnailGenerator) generateThumbnail(path string) (string, error) {
	fileExt := filepath.Ext(path)
	for index := range g.generators {
		generator := g.generators[index]

		if !generator.supportsExt(fileExt) {
			continue
		}

		if g.fileCache != nil {
			return g.generateWithFileCache(path, generator)
		}
		return g.generateWithTempDir(path, generator)
	}

	return "", errors.New("unsupported file format")
}

// generateWithFileCache uses persistent L2: look up by content key, else generate
// into the cache dir and Commit. Caller stores the returned path in memoryCache.
func (g *ThumbnailGenerator) generateWithFileCache(path string, generator thumbnailGeneratorInterface) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}

	key := cache.MakeKey(absPath, info, generator.kind(), thumbCacheVersion)
	if cached, ok := g.fileCache.Get(key); ok {
		return cached, nil
	}

	withoutExt := g.fileCache.Path(key)
	outputPath, err := generator.generateThumbnail(absPath, withoutExt)
	if err != nil {
		return "", err
	}

	if err := g.fileCache.Commit(key, outputPath); err != nil {
		// Still return the generated file even if index update fails
		slog.Debug("Failed to commit thumbnail to cache", "error", err, "path", absPath)
	}
	return outputPath, nil
}

func (g *ThumbnailGenerator) generateWithTempDir(path string, generator thumbnailGeneratorInterface) (string, error) {
	fileExt := filepath.Ext(path)
	filename := filepath.Base(path)
	baseName := filename[:len(filename)-len(fileExt)]

	outputPathWithoutExt := filepath.Join(g.tempDirectory,
		fmt.Sprintf("%s-%d", baseName, time.Now().UnixNano()))

	return generator.generateThumbnail(path, outputPathWithoutExt)
}

// CleanUp releases session resources and flushes persistent cache metadata.
// Blob files are kept on disk.
func (g *ThumbnailGenerator) CleanUp() error {
	g.mu.Lock()
	g.memoryCache = make(map[string]string)
	g.mu.Unlock()

	if g.fileCache != nil {
		if err := g.fileCache.Flush(); err != nil {
			return err
		}
	}

	if g.tempDirectory != "" {
		return os.RemoveAll(g.tempDirectory)
	}
	return nil
}

func isPopplerInstalled() bool {
	_, err := exec.LookPath("pdftoppm")
	return err == nil
}

func isGhostscriptInstalled() bool {
	_, err := exec.LookPath("gs")
	return err == nil
}

func isFFmpegInstalled() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}
