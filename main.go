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
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"golang.org/x/sync/semaphore"
)

// region Variables

const Version = "0.1"

const AllowedExtensions = `\.(gif|jpg|jpeg|png|webp)$`

var AvifExportParams = &vips.AvifExportParams{
	Effort:        5,
	Lossless:      false,
	Quality:       80,
	StripMetadata: false,
}

var Concurrency = runtime.NumCPU()

var Progress = progressbar.NewOptions(0,
	progressbar.OptionEnableColorCodes(true),
	progressbar.OptionSetElapsedTime(true),
	progressbar.OptionSetPredictTime(false),
	progressbar.OptionSetTheme(progressbar.Theme{
		Saucer:        "[cyan]=[reset]",
		SaucerHead:    "[cyan]>[reset]",
		SaucerPadding: " ",
		BarStart:      "[",
		BarEnd:        "]",
	}),
	progressbar.OptionShowBytes(false),
	progressbar.OptionShowCount(),
	progressbar.OptionShowElapsedTimeOnFinish(),
	progressbar.OptionSpinnerType(14),
)

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

	return fmt.Sprintf("%.1f%s", float64(bytes)/float64(div), suffixes[exp])
}

// endregion Helpers

// region Traverse

func FindImagesAt(root string) ([]string, error) {
	r, err := regexp.Compile(AllowedExtensions)

	if err != nil {
		return nil, err
	}

	Progress.ChangeMax(-1)
	Progress.Describe("[cyan]Search images...[reset]")

	defer Progress.Exit()

	var files []string

	var count int

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		if matched := r.MatchString(path); matched {
			count += 1

			if count >= 20 {
				Progress.Add(count)

				count = 0
			}

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

	bytes, _, err := image.ExportAvif(AvifExportParams)

	if err != nil {
		return 0, 0, err
	}

	err = os.WriteFile(ReplaceExt(path), bytes, 0644)

	if err != nil {
		return 0, 0, err
	}

	err = os.Remove(path)

	if err != nil {
		return 0, 0, err
	}

	return uint64(reader.count), uint64(len(bytes)), nil
}

type Stats struct {
	Failed []string

	SizeBefore uint64
	SizeAfter  uint64
}

func ConvertImages(paths []string) *Stats {
	Progress.Reset()
	Progress.ChangeMax(len(paths))
	Progress.Describe("[cyan]Converting images...[reset]")

	defer func() {
		Progress.Exit()

		fmt.Println()
	}()

	stats := &Stats{}

	wg := sync.WaitGroup{}
	mu := sync.Mutex{}
	sm := semaphore.NewWeighted(int64(Concurrency))

	for _, path := range paths {
		wg.Add(1)

		sm.Acquire(context.TODO(), 1)

		go func(path string) {
			defer wg.Done()
			defer sm.Release(1)

			sizeBefore, sizeAfter, err := ConvertImage(path)

			mu.Lock()

			Progress.Add(1)

			if err != nil {
				stats.Failed = append(stats.Failed, path)
			} else {
				stats.SizeBefore += sizeBefore
				stats.SizeAfter += sizeAfter
			}

			mu.Unlock()
		}(path)
	}

	wg.Wait()

	return stats
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

	rootCmd := &cobra.Command{
		Use:   "avify",
		Short: "Avify allows to convert your reference images to AVIF format to save your storage space",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			paths, err := FindImagesAt(args[0])

			if err != nil {
				panic(err)
			}

			if len(paths) == 0 {
				fmt.Println("No images found")

				return
			}

			stats := ConvertImages(paths)

			if len(stats.Failed) < len(paths) {
				savedSize := stats.SizeBefore - stats.SizeAfter
				saved := float64(savedSize) / float64(stats.SizeBefore) * 100

				fmt.Printf("Total size before: %s\n", FormatBytes(stats.SizeBefore))
				fmt.Printf("Total size after: %s\n", FormatBytes(stats.SizeAfter))
				fmt.Printf("Saved size: %s (%.2f%%)\n", FormatBytes(savedSize), saved)
			}

			if len(stats.Failed) > 0 {
				fmt.Println("Following files are failed:")

				for _, path := range stats.Failed {
					fmt.Printf("\t%s\n", path)
				}
			}
		},
	}

	rootCmd.AddCommand(&cobra.Command{
		Use: "version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("avify %s (%s, vips %v)\n", Version, runtime.Version(), vips.Version)
		},
	})

	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
