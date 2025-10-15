package backup

import (
	"archive/tar"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/klauspost/pgzip"
)

type Data struct {
	Locations []Location `yaml:"location"`
}

// A Location represents a directory that should be included in a backup.
type Location struct {
	Path   string   `yaml:"path"`
	Ignore []string `yaml:"ignore"`

	size  int64        // The logical size of the location in bytes
	index []IndexEntry // A list of all paths that should be included in the backup
}

type IndexEntry struct {
	Path string      `yaml:"path"`
	Info os.FileInfo `yaml:"info"`
}

func BackupData(config *Config) error {

	log.SetPrefix("[DATA] ")

	var err error
	for _, location := range config.Data.Locations {

		// TODO: Warn if backup output in inside location.Path

		// Generate a unique filename based on the location path
		h := sha256.New()
		h.Write([]byte(location.Path))
		filename := fmt.Sprintf("%s-%x.tar.gz", filepath.Base(location.Path), h.Sum(nil))

		// If path starts with "~", expand it
		if len(location.Path) > 1 && location.Path[:2] == "~/" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home dir: %w", err)
			}
			location.Path = filepath.Join(home, location.Path[2:]) // replace "~/" with home path
		}

		// Then convert to absolute path
		location.Path, err = filepath.Abs(location.Path)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}

		err = location.scan()
		if err != nil {
			return fmt.Errorf("failed to scan location: %w", err)
		}

		tw, err := openArchiveWriter(filepath.Join(config.Output, filename))
		if err != nil {
			return fmt.Errorf("failed to open tar.gz writer: %w", err)
		}
		defer tw.Close()

		err = location.write(tw)
		if err != nil {
			return fmt.Errorf("failed to filter location: %w", err)
		}

	}

	log.Println("Backup of files created successfully!")
	log.SetPrefix("")

	return nil

}

// TODO: implement proper directory scanning for calculating the logical size
// of only the files that should be inlcuded in the backup
func (l *Location) scan() error {

	log.Printf("Scanning location %s", l.Path)

	l.index = make([]IndexEntry, 0)

	// Walk the directory
	err := filepath.WalkDir(l.Path, func(path string, d os.DirEntry, werr error) error {

		if werr != nil {
			return werr
		}

		// Skip the root directory
		if path == l.Path {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		// Skip directory or file if matching ignore patterns
		if slices.Contains(l.Ignore, info.Name()) {
			fmt.Println("Skipping:", info.Name())
			if info.IsDir() {
				return filepath.SkipDir // Skip the entire directory tree
			}
			return nil // Skip just this file
		}

		l.size += info.Size()
		l.index = append(l.index, IndexEntry{
			Path: path,
			Info: info,
		})

		return nil

	})
	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	return nil

}

// TODO: Fix error `failed to filter location: archive/tar: write too long` e.g.
// /Users/nhinke/playground/esp32/distance-sensor/build/esp-idf/mbedtls/mbedtls/library/error.c
func (l *Location) write(tw *ArchiveWriter) error {

	for _, e := range l.index {

		relPath, err := filepath.Rel(l.Path, e.Path)
		if err != nil {
			return err
		}

		// Create tar header
		hdr, err := tar.FileInfoHeader(e.Info, "")
		if err != nil {
			return err
		}
		hdr.Name = relPath
		// hdr.Format = tar.FormatPAX

		log.Printf("Writing %s\n", e.Path)

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		// TODO: Consider implementing a parallel file buffer for small files
		if !e.Info.IsDir() {

			file, err := os.Open(e.Path)
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(tw, file); err != nil {
				fmt.Println("error on file ", e.Path)
				return err
			}

		}

	}

	return nil

}

// TarGzWriter wraps tar.Writer and handles proper cleanup of all underlying writers
type ArchiveWriter struct {
	tar  *tar.Writer
	gzip *pgzip.Writer
	file *os.File
}

// Close closes all writers in the correct order
func (aw *ArchiveWriter) Close() error {
	var output error
	if err := aw.tar.Close(); err != nil { // Close tar writer first
		output = err
	}
	if err := aw.gzip.Close(); err != nil { // Close gzip writer second
		output = err
	}
	if err := aw.file.Close(); err != nil { // Close file last
		output = err
	}
	return output
}

func (aw *ArchiveWriter) WriteHeader(hdr *tar.Header) error {
	return aw.tar.WriteHeader(hdr)
}

func (aw *ArchiveWriter) Write(p []byte) (int, error) {
	return aw.tar.Write(p)
}

func openArchiveWriter(path string) (*ArchiveWriter, error) {

	outFile, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	// Parallel gzip writer
	gw, err := pgzip.NewWriterLevel(outFile, pgzip.DefaultCompression)
	if err != nil {
		outFile.Close() // Clean up file on error
		return nil, err
	}

	// Tune concurrency: (block size, threads). Defaults are good, but you can tune.
	gw.SetConcurrency(1<<20, runtime.NumCPU()) // 1MB blocks, use all CPU cores

	return &ArchiveWriter{
		tar:  tar.NewWriter(gw),
		gzip: gw,
		file: outFile,
	}, nil

}
