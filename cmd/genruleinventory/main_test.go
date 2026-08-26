package main

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeSchematronVerifiesAndExtractsOnlyRequiredMembers(t *testing.T) {
	archive := createArchive(t, schematronMembers, false)
	digest := digestArchive(t, archive)
	root, cleanup, err := materializeSchematron(archive, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, path := range []string{"common.sch", "cii/XRechnung-CII-validation.sch", "ubl/XRechnung-UBL-validation.sch"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != path {
			t.Fatalf("unexpected content for %s: %q", path, data)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "ignored.txt")); !os.IsNotExist(err) {
		t.Fatalf("unexpected extraction of ignored member: %v", err)
	}
}

func TestMaterializeSchematronRejectsUntrustedArchives(t *testing.T) {
	valid := createArchive(t, schematronMembers, false)
	tests := []struct {
		name    string
		archive string
		digest  string
		want    string
	}{
		{name: "digest", archive: valid, digest: strings.Repeat("0", 64), want: "digest mismatch"},
		{name: "missing member", archive: createArchive(t, schematronMembers[:2], false), want: "is missing"},
		{name: "duplicate member", archive: createArchive(t, append(schematronMembers, schematronMembers[0]), false), want: "duplicate"},
		{name: "symlink member", archive: createArchive(t, schematronMembers, true), want: "bounded regular file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			digest := test.digest
			if digest == "" {
				digest = digestArchive(t, test.archive)
			}
			_, cleanup, err := materializeSchematron(test.archive, digest)
			cleanup()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func createArchive(t *testing.T, members []string, symlinkFirst bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for index, name := range members {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if symlinkFirst && index == 0 {
			header.SetMode(os.ModeSymlink | 0o777)
		} else {
			header.SetMode(0o600)
		}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(strings.TrimPrefix(name, "schematron/"))); err != nil {
			t.Fatal(err)
		}
	}
	ignored, err := writer.Create("ignored.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ignored.Write([]byte("ignored")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func digestArchive(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
