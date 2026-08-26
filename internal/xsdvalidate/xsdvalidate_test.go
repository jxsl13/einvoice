package xsdvalidate

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jacoelho/xsd/xsderrors"
)

func TestValidateEmbeddedSchemas(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "CII", path: "../../testdata/cii/xrechnung/zugferd-xrechnung-einfach.xml"},
		{name: "UBL invoice", path: "../../testdata/ubl/invoice/UBL-Invoice-2.1-Example.xml"},
		{name: "UBL credit note", path: "../../testdata/ubl/creditnote/UBL-CreditNote-2.1-Example.xml"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(context.Background(), data, 16<<20, 256, 1_000_000, 16<<20); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestValidateFailures(t *testing.T) {
	var nilContext context.Context
	if err := Validate(nilContext, []byte("<x/>"), 1024, 8, 8, 8); err == nil {
		t.Fatal("Validate(nil) succeeded")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Validate(canceled, []byte("<x/>"), 1024, 8, 8, 8); !errors.Is(err, context.Canceled) {
		t.Fatalf("Validate(canceled) = %v", err)
	}
	if err := Validate(context.Background(), nil, 1024, 8, 8, 8); err == nil {
		t.Fatal("Validate(empty) succeeded")
	}
	if err := Validate(context.Background(), []byte("<x/>"), 1024, 8, 8, 8); !isValidationError(err) {
		t.Fatalf("unsupported root error = %v", err)
	}
	invalid := []byte(`<Invoice xmlns="` + ublInvoiceNS + `"><Unexpected/></Invoice>`)
	if err := Validate(context.Background(), invalid, 1024, 8, 8, 1024); !isValidationError(err) {
		t.Fatalf("schema-invalid error = %v", err)
	}
}

func TestSchemaResolver(t *testing.T) {
	resolver := schemaResolver{files: map[string][]byte{
		"schemas/root.xsd":  []byte("root"),
		"schemas/child.xsd": []byte("child"),
	}}
	if _, err := resolver.ResolveSchema(context.Background(), "schemas/root.xsd", "child.xsd"); err != nil {
		t.Fatalf("ResolveSchema() error = %v", err)
	}
	for _, location := range []string{"%zz", "../../outside.xsd", "/absolute.xsd", "missing.xsd"} {
		if _, err := resolver.ResolveSchema(context.Background(), "schemas/root.xsd", location); !errors.Is(err, xsderrors.ErrSchemaNotFound) {
			t.Fatalf("ResolveSchema(%q) = %v", location, err)
		}
	}
}

func TestCompileSchemaFilesRejectsMissingAndInvalidRoots(t *testing.T) {
	if _, err := compileSchemaFiles(nil); err == nil || !strings.Contains(err.Error(), "missing embedded schema") {
		t.Fatalf("compileSchemaFiles(nil) = %v", err)
	}
	files := map[string][]byte{ciiRoot: []byte("<invalid")}
	if _, err := compileSchemaFiles(files); err == nil || !strings.Contains(err.Error(), "compile CII") {
		t.Fatalf("compileSchemaFiles(invalid) = %v", err)
	}
}

func TestReadArchiveDataRejectsUnsafeInputs(t *testing.T) {
	if _, err := readArchiveData([]byte("not a zip")); err == nil {
		t.Fatal("invalid archive accepted")
	}
	for _, test := range []struct {
		name    string
		entries []zipEntry
	}{
		{name: "path escape", entries: []zipEntry{{name: "../x.xsd", data: "x"}}},
		{name: "directory", entries: []zipEntry{{name: "x/", directory: true}}},
		{name: "duplicate", entries: []zipEntry{{name: "x.xsd", data: "a"}, {name: "x.xsd", data: "b"}}},
		{name: "oversized entry", entries: []zipEntry{{name: "x.xsd", data: strings.Repeat("x", maxSchemaFileBytes+1)}}},
		{name: "oversized total", entries: []zipEntry{
			{name: "a.xsd", data: strings.Repeat("a", maxSchemaFileBytes)},
			{name: "b.xsd", data: strings.Repeat("b", maxSchemaFileBytes)},
			{name: "c.xsd", data: "c"},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := readArchiveData(makeZip(t, test.entries)); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	files, err := readArchiveData(makeZip(t, []zipEntry{{name: "safe.xsd", data: "schema"}}))
	if err != nil || string(files["safe.xsd"]) != "schema" {
		t.Fatalf("safe archive = %q, %v", files["safe.xsd"], err)
	}
}

func TestStripLegacySchemaDTD(t *testing.T) {
	plain := []byte("<schema/>")
	if got := stripLegacySchemaDTD(plain); !bytes.Equal(got, plain) {
		t.Fatalf("plain schema changed: %q", got)
	}
	unterminated := []byte("<!DOCTYPE schema [<schema/>")
	if got := stripLegacySchemaDTD(unterminated); !bytes.Equal(got, unterminated) {
		t.Fatalf("unterminated DTD changed: %q", got)
	}
	withDTD := []byte("before<!DOCTYPE schema [ignored]>after")
	if got := string(stripLegacySchemaDTD(withDTD)); got != "beforeafter" {
		t.Fatalf("stripLegacySchemaDTD() = %q", got)
	}
}

func isValidationError(err error) bool {
	var diagnostic *xsderrors.Error
	return errors.As(err, &diagnostic) && diagnostic.Category == xsderrors.CategoryValidation
}

type zipEntry struct {
	name      string
	data      string
	directory bool
}

func makeZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.directory {
			header.SetMode(os.ModeDir | 0o755)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
