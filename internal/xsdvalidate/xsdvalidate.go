// Package xsdvalidate validates invoice XML against the pinned, embedded XML
// schemas without filesystem or network access.
package xsdvalidate

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"sync"

	xsd "github.com/jacoelho/xsd"
	"github.com/jacoelho/xsd/xsderrors"
)

const (
	ciiRoot        = "cii/16b/xsd/CrossIndustryInvoice_100pD16B.xsd"
	ublInvoiceRoot = "ubl/2.1/xsd/maindoc/UBL-Invoice-2.1.xsd"
	ublCreditRoot  = "ubl/2.1/xsd/maindoc/UBL-CreditNote-2.1.xsd"

	ciiNamespace    = "urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
	ublInvoiceNS    = "urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"
	ublCreditNoteNS = "urn:oasis:names:specification:ubl:schema:xsd:CreditNote-2"

	maxSchemaFileBytes = 2 << 20
	maxSchemaTotal     = 4 << 20
)

//go:embed schemas.zip
var archiveData []byte

type schemaSet struct {
	cii        *xsd.Engine
	invoice    *xsd.Engine
	creditNote *xsd.Engine
}

var loadSchemaSet = sync.OnceValues(compileSchemas)

// Validate checks data against the schema selected by its exact root QName.
// The embedded resolver cannot read from disk or perform network requests.
func Validate(ctx context.Context, data []byte, maxBytes int64, maxDepth, maxAttributes, maxTextBytes int) error {
	if ctx == nil {
		return errors.New("xsdvalidate: nil context")
	}
	root, err := rootName(ctx, data)
	if err != nil {
		return err
	}
	schemas, err := loadSchemaSet()
	if err != nil {
		return fmt.Errorf("xsdvalidate: compile embedded schemas: %w", err)
	}
	var engine *xsd.Engine
	switch root {
	case (xml.Name{Space: ciiNamespace, Local: "CrossIndustryInvoice"}):
		engine = schemas.cii
	case (xml.Name{Space: ublInvoiceNS, Local: "Invoice"}):
		engine = schemas.invoice
	case (xml.Name{Space: ublCreditNoteNS, Local: "CreditNote"}):
		engine = schemas.creditNote
	default:
		return xsderrors.Validation(xsderrors.CodeValidationRoot, 0, 0, "", "unsupported invoice root")
	}
	return engine.ValidateWithOptions(ctx, bytes.NewReader(data), xsd.ValidateOptions{
		MaxErrors:                       1,
		MaxIdentityScopes:               10_000,
		MaxIdentityEntries:              100_000,
		MaxIdentityTupleBytes:           4 << 10,
		MaxInstanceDepth:                maxDepth,
		MaxInstanceAttributes:           maxAttributes,
		MaxInstanceTextBytes:            int64(maxTextBytes),
		MaxInstanceTokenBytes:           maxBytes,
		MaxInstanceBytes:                maxBytes,
		MaxSchemaLocationNamespaces:     256,
		MaxSchemaLocationNamespaceBytes: 64 << 10,
	})
}

func rootName(ctx context.Context, data []byte) (xml.Name, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	for {
		if err := ctx.Err(); err != nil {
			return xml.Name{}, err
		}
		token, err := decoder.Token()
		if err != nil {
			return xml.Name{}, err
		}
		if start, ok := token.(xml.StartElement); ok {
			return start.Name, nil
		}
	}
}

func compileSchemas() (*schemaSet, error) {
	files, err := readArchive()
	if err != nil {
		return nil, err
	}
	return compileSchemaFiles(files)
}

func compileSchemaFiles(files map[string][]byte) (*schemaSet, error) {
	resolver := schemaResolver{files: files}
	compile := func(name string) (*xsd.Engine, error) {
		data, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("missing embedded schema %q", name)
		}
		return xsd.CompileWithOptions(context.Background(), xsd.CompileOptions{
			MaxSchemaDepth:             128,
			MaxSchemaAttributes:        128,
			MaxSchemaTokenBytes:        maxSchemaFileBytes,
			MaxSchemaSourceBytes:       maxSchemaFileBytes,
			MaxSchemaSources:           32,
			MaxSchemaTotalBytes:        maxSchemaTotal,
			MaxSchemaReferences:        128,
			MaxSchemaTargetContexts:    64,
			MaxSchemaInstantiatedNodes: 200_000,
			MaxSchemaNames:             100_000,
			MaxContentModelStates:      100_000,
		}, xsd.Bytes(name, stripLegacySchemaDTD(data)).WithResolver(resolver))
	}
	cii, err := compile(ciiRoot)
	if err != nil {
		return nil, fmt.Errorf("compile CII: %w", err)
	}
	invoice, err := compile(ublInvoiceRoot)
	if err != nil {
		return nil, fmt.Errorf("compile UBL Invoice: %w", err)
	}
	creditNote, err := compile(ublCreditRoot)
	if err != nil {
		return nil, fmt.Errorf("compile UBL CreditNote: %w", err)
	}
	return &schemaSet{cii: cii, invoice: invoice, creditNote: creditNote}, nil
}

type schemaResolver struct {
	files map[string][]byte
}

func (r schemaResolver) ResolveSchema(_ context.Context, base, location string) (xsd.SchemaSource, error) {
	decoded, err := url.PathUnescape(location)
	if err != nil {
		return xsd.SchemaSource{}, xsderrors.ErrSchemaNotFound
	}
	name := path.Clean(path.Join(path.Dir(base), decoded))
	if strings.HasPrefix(name, "../") || path.IsAbs(name) {
		return xsd.SchemaSource{}, xsderrors.ErrSchemaNotFound
	}
	data, ok := r.files[name]
	if !ok {
		return xsd.SchemaSource{}, xsderrors.ErrSchemaNotFound
	}
	return xsd.Bytes(name, stripLegacySchemaDTD(data)).WithResolver(r), nil
}

func readArchive() (map[string][]byte, error) {
	return readArchiveData(archiveData)
}

func readArchiveData(data []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(reader.File))
	total := int64(0)
	for _, entry := range reader.File {
		name := path.Clean(entry.Name)
		if name != entry.Name || path.IsAbs(name) || strings.HasPrefix(name, "../") || entry.FileInfo().IsDir() {
			return nil, fmt.Errorf("unsafe schema archive entry %q", entry.Name)
		}
		if entry.UncompressedSize64 > maxSchemaFileBytes {
			return nil, fmt.Errorf("schema archive entry too large %q", name)
		}
		total += int64(entry.UncompressedSize64)
		if total > maxSchemaTotal {
			return nil, errors.New("schema archive exceeds size limit")
		}
		stream, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, maxSchemaFileBytes+1))
		closeErr := stream.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if int64(len(data)) != int64(entry.UncompressedSize64) {
			return nil, fmt.Errorf("schema archive size mismatch %q", name)
		}
		if _, duplicate := files[name]; duplicate {
			return nil, fmt.Errorf("duplicate schema archive entry %q", name)
		}
		files[name] = data
	}
	return files, nil
}

func stripLegacySchemaDTD(data []byte) []byte {
	start := bytes.Index(data, []byte("<!DOCTYPE schema"))
	if start < 0 {
		return data
	}
	end := bytes.Index(data[start:], []byte("]>"))
	if end < 0 {
		return data
	}
	end += start + len("]>")
	clean := make([]byte, 0, len(data)-(end-start))
	clean = append(clean, data[:start]...)
	return append(clean, data[end:]...)
}
