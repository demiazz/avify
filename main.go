package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/davidbyttow/govips/v2/vips"
	"golang.org/x/sync/semaphore"
)

// region Variables

const AllowedExtensions = `\.(jpg|jpeg|png)$`

var AvifExportParams = &vips.AvifExportParams{
	Effort:        5,
	Lossless:      false,
	Quality:       80,
	StripMetadata: false,
}

var Concurrency = runtime.NumCPU()

// endregion Variables

// region Helpers

type Reader struct {
	r     io.Reader
	count int64
}

func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

func (r *Reader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)

	r.count += int64(n)

	return n, err
}

func ReplaceExt(path string) string {
	old := filepath.Ext(path)

	return strings.TrimSuffix(path, old) + ".avif"
}

func FormatBytes(bytes uint64) string {
	const unit = 1024

	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	suffixes := []string{"KB", "MB", "GB", "TB", "PB", "EB"}

	div, exp := uint64(unit), 0

	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), suffixes[exp])
}

// endregion Helpers

// region Traverse

func FindImagesAt(root string) ([]string, error) {
	r, err := regexp.Compile(AllowedExtensions)

	if err != nil {
		return nil, err
	}

	var files []string

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		if matched := r.MatchString(path); matched {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

// endregion Traverse

// region Convert

func ConvertImage(path string) (uint64, uint64, error) {
	file, err := os.Open(path)

	if err != nil {
		return 0, 0, err
	}

	defer file.Close()

	reader := NewReader(file)

	image, err := vips.NewImageFromReader(reader)

	if err != nil {
		return 0, 0, err
	}

	oldSize := reader.count

	bytes, _, err := image.ExportAvif(AvifExportParams)

	if err != nil {
		return 0, 0, err
	}

	err = os.WriteFile(ReplaceExt(path), bytes, 0644)

	if err != nil {
		return 0, 0, err
	}

	return uint64(oldSize), uint64(len(bytes)), nil
}

func ConvertImages(paths []string) (uint64, uint64, error) {
	var oldTotalSize uint64
	var newTotalSize uint64

	var wg sync.WaitGroup
	var mu sync.Mutex

	maxWorkers := Concurrency
	sem := semaphore.NewWeighted(int64(maxWorkers))

	errChan := make(chan error, len(paths))

	for _, path := range paths {
		wg.Add(1)

		sem.Acquire(context.TODO(), 1)

		go func(path string) {
			defer wg.Done()
			defer sem.Release(1)

			oldSize, newSize, err := ConvertImage(path)

			if err != nil {
				errChan <- err

				return
			}

			mu.Lock()

			oldTotalSize += oldSize
			newTotalSize += newSize

			mu.Unlock()
		}(path)
	}

	wg.Wait()

	close(errChan)

	if len(errChan) > 0 {
		return 0, 0, <-errChan
	}

	return oldTotalSize, newTotalSize, nil
}

// endregion Convert

func main() {
	vips.LoggingSettings(nil, vips.LogLevelError)

	vips.Startup(&vips.Config{
		ConcurrencyLevel: Concurrency,
		MaxCacheMem:      0,
		MaxCacheSize:     0,
		MaxCacheFiles:    0,
		CacheTrace:       false,
	})

	defer vips.Shutdown()

	target := os.Args[1]

	paths, err := FindImagesAt(target)

	if err != nil {
		panic(err)
	}

	oldSize, newSize, err := ConvertImages(paths)

	if err != nil {
		panic(err)
	}

	fmt.Printf("Before: %s, After: %s\n", FormatBytes(oldSize), FormatBytes(newSize))
}
