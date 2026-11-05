package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
)

func extractBinary(source io.Reader, destination io.Writer) error {
	gz, err := gzip.NewReader(source)
	if err != nil {
		return fmt.Errorf("open release archive: %w", err)
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: 2*maxArchiveSize + 1}
	reader := tar.NewReader(limited)
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read release archive: %w", err)
		}
		if strings.TrimPrefix(header.Name, "./") != "rehome" {
			continue
		}
		if found || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size <= 0 || header.Size > maxArchiveSize {
			return errors.New("release archive has an invalid rehome executable")
		}
		if _, err := io.Copy(destination, reader); err != nil {
			return fmt.Errorf("extract executable: %w", err)
		}
		found = true
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return fmt.Errorf("read release archive footer: %w", err)
	}
	if limited.N == 0 {
		return errors.New("release archive exceeds unpacked size limit")
	}
	if !found {
		return errors.New("release archive has no rehome executable")
	}
	return nil
}
