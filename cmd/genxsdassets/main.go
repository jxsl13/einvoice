// Command genxsdassets builds the deterministic, unmodified XSD bundle used by
// the production validator. It is a maintainer tool and is not shipped.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type manifest struct {
	Files         []manifestFile `json:"files"`
	SchemaVersion int            `json:"schemaVersion"`
}

type manifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func main() {
	source := flag.String("source", "", "KoSIT configuration resources directory")
	output := flag.String("output", "internal/xsdvalidate/schemas.zip", "output ZIP path")
	manifestPath := flag.String("manifest", "internal/xsdvalidate/schemas.json", "output manifest path")
	flag.Parse()
	if strings.TrimSpace(*source) == "" {
		fatalf("-source is required")
	}

	paths, err := selectedSchemas(*source)
	if err != nil {
		fatalf("select schemas: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}
	entries, err := writeArchive(*source, *output, paths)
	if err != nil {
		fatalf("write archive: %v", err)
	}
	if err := writeManifest(*manifestPath, entries); err != nil {
		fatalf("write manifest: %v", err)
	}
}

func selectedSchemas(root string) ([]string, error) {
	paths := []string{
		"ubl/2.1/xsd/maindoc/UBL-Invoice-2.1.xsd",
		"ubl/2.1/xsd/maindoc/UBL-CreditNote-2.1.xsd",
	}
	for _, directory := range []string{"cii/16b/xsd", "ubl/2.1/xsd/common"} {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(directory), "*.xsd"))
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no schemas in %s", directory)
		}
		for _, match := range matches {
			relative, err := filepath.Rel(root, match)
			if err != nil {
				return nil, err
			}
			paths = append(paths, filepath.ToSlash(relative))
		}
	}
	slices.Sort(paths)
	return paths, nil
}

func writeArchive(root, output string, paths []string) ([]manifestFile, error) {
	file, err := os.Create(output) //nolint:gosec // Maintainer-selected output path.
	if err != nil {
		return nil, err
	}
	archive := zip.NewWriter(file)
	entries := make([]manifestFile, 0, len(paths))
	for _, name := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name))) //nolint:gosec // Paths are fixed or Glob-selected XSD files below root.
		if err != nil {
			return nil, closeArchive(archive, file, err)
		}
		digest := sha256.Sum256(data)
		entries = append(entries, manifestFile{Path: name, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data))})

		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o644)
		header.Modified = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return nil, closeArchive(archive, file, err)
		}
		if _, err := writer.Write(data); err != nil {
			return nil, closeArchive(archive, file, err)
		}
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return entries, nil
}

func closeArchive(archive *zip.Writer, file io.Closer, cause error) error {
	_ = archive.Close()
	_ = file.Close()
	return cause
}

func writeManifest(path string, files []manifestFile) error {
	data, err := json.MarshalIndent(manifest{Files: files, SchemaVersion: 1}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644) //nolint:gosec // Maintainer-selected output path.
}

func fatalf(format string, values ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
