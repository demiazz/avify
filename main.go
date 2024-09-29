package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/davidbyttow/govips/v2/vips"
	bar "github.com/schollz/progressbar/v3"
)

// region Variables

const AllowedExtensions = `\.(jpg|jpeg|png)$`

var AvifExportParams = &vips.AvifExportParams{
	Effort:        5,
	Lossless:      false,
	Quality:       80,
	StripMetadata: false,
}

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
	progress := bar.NewOptions64(-1, bar.OptionShowCount(), bar.OptionSetDescription("searching images..."))

	defer progress.Close()

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
			progress.Add64(1)

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
	var oldSizeTotal uint64
	var newSizeTotal uint64

	for _, path := range paths {
		oldSize, newSize, err := ConvertImage(path)

		if err != nil {
			return 0, 0, err
		}

		oldSizeTotal += oldSize
		newSizeTotal += newSize
	}

	return oldSizeTotal, newSizeTotal, nil
}

// endregion Convert

func main() {
	vips.LoggingSettings(nil, vips.LogLevelError)

	vips.Startup(nil)

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
