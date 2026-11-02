package secrets

import (
	"io/fs"
	"os"
	"path/filepath"
)

// ScanDir reports secret-looking values in every text file under root (excluding .git);
// push uses it as the last gate before anything leaves the machine.
func ScanDir(root string) ([]Finding, error) {
	var findings []Finding

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}

			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		data, err := os.ReadFile(path)

		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		_, found := Redact(rel, data)
		findings = append(findings, found...)

		return nil
	})

	return findings, err
}
