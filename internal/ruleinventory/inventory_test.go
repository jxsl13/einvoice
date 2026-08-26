package ruleinventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateResolvesIncludesAndOrdersSyntaxes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "common.sch"), `<pattern xmlns="http://purl.oclc.org/dsdl/schematron" id="variable-pattern"/>`)
	cii := `<?xml version="1.0"?><schema xmlns="http://purl.oclc.org/dsdl/schematron" queryBinding="xslt2"><phase id="xrechnung-model"><active pattern="variable-pattern"/><active pattern="cii-pattern"/></phase><include href="common.sch"/><pattern id="cii-pattern"><rule context=" /Invoice "><assert id="BR-1" flag="fatal" test=" normalize-space(ID) ">required</assert></rule></pattern></schema>`
	ubl := strings.ReplaceAll(strings.ReplaceAll(cii, "cii-pattern", "ubl-pattern"), "/Invoice", "/ubl:Invoice")
	writeFile(t, filepath.Join(root, "cii.sch"), cii)
	writeFile(t, filepath.Join(root, "ubl.sch"), ubl)

	request := validRequest(root)
	request.Phase = "xrechnung-model"
	request.Sources = []SyntaxSource{
		{Syntax: "ubl", Path: "ubl.sch", SHA256: digestFile(t, filepath.Join(root, "ubl.sch"))},
		{Syntax: "cii", Path: "cii.sch", SHA256: digestFile(t, filepath.Join(root, "cii.sch"))},
	}
	got, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Syntaxes[0].Syntax != "cii" || got.Syntaxes[1].Syntax != "ubl" {
		t.Fatalf("syntaxes are not deterministic: %#v", got.Syntaxes)
	}
	if got.Syntaxes[0].RuleCount != 1 || got.Syntaxes[0].Patterns[1].Rules[0].Context != "/Invoice" {
		t.Fatalf("unexpected inventory: %#v", got.Syntaxes[0])
	}
	if got.Syntaxes[0].Patterns[1].Rules[0].Digest == "" {
		t.Fatal("rule digest is empty")
	}
	first, err := Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || first[len(first)-1] != '\n' {
		t.Fatal("inventory JSON is not canonical")
	}
}

func TestGenerateRejectsUnsafeOrInvalidSources(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "remote include", source: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase id="p"><active pattern="x"/></phase><include href="https://example.invalid/x.sch"/></schema>`, want: "non-local include"},
		{name: "unknown severity", source: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase id="p"><active pattern="x"/></phase><pattern id="x"><rule context="/"><assert id="R" flag="maybe" test="true()"/></rule></pattern></schema>`, want: "unknown severity"},
		{name: "duplicate rule", source: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase id="p"><active pattern="x"/></phase><pattern id="x"><rule context="/"><assert id="R" flag="fatal" test="true()"/><assert id="R" flag="fatal" test="false()"/></rule></pattern></schema>`, want: "duplicate rule ID"},
		{name: "unresolved pattern", source: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase id="p"><active pattern="missing"/></phase></schema>`, want: "unresolved"},
		{name: "directive", source: `<!DOCTYPE schema><schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase id="p"><active pattern="x"/></phase></schema>`, want: "directives are not allowed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "source.sch")
			writeFile(t, path, test.source)
			request := validRequest(root)
			request.Sources = []SyntaxSource{{Syntax: "cii", Path: "source.sch", SHA256: digestFile(t, path)}}
			_, err := Generate(request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestGenerateRejectsDigestMismatchAndPathEscape(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.sch")
	writeFile(t, path, `<schema xmlns="http://purl.oclc.org/dsdl/schematron"/>`)
	request := validRequest(root)
	request.Sources = []SyntaxSource{{Syntax: "cii", Path: "source.sch", SHA256: strings.Repeat("0", 64)}}
	_, err := Generate(request)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.sch")
	writeFile(t, outside, `<schema xmlns="http://purl.oclc.org/dsdl/schematron"/>`)
	request.Sources = []SyntaxSource{{Syntax: "cii", Path: outside, SHA256: digestFile(t, outside)}}
	_, err = Generate(request)
	if err == nil || !strings.Contains(err.Error(), "escapes source root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateValidatesRequestAndDocumentStructure(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.sch")
	validSource := `<schema xmlns="http://purl.oclc.org/dsdl/schematron" queryBinding="xslt2"><phase id="p"><active pattern="x"/></phase><pattern id="x"><rule context="/"><report id="R" flag="information" test="false()"/></rule></pattern></schema>`
	writeFile(t, sourcePath, validSource)
	valid := validRequest(root)
	valid.Sources = []SyntaxSource{{Syntax: "cii", Path: "source.sch", SHA256: digestFile(t, sourcePath)}}

	tests := []struct {
		name   string
		mutate func(*Request)
		want   string
	}{
		{name: "root", mutate: func(r *Request) { r.Root = "" }, want: "root is required"},
		{name: "phase", mutate: func(r *Request) { r.Phase = "" }, want: "phase is required"},
		{name: "sources", mutate: func(r *Request) { r.Sources = nil }, want: "syntax source is required"},
		{name: "profile", mutate: func(r *Request) { r.Profile = "" }, want: "profile is required"},
		{name: "archive digest", mutate: func(r *Request) { r.Archive.SHA256 = "bad" }, want: "archive SHA-256 is invalid"},
		{name: "archive URL", mutate: func(r *Request) { r.Archive.URL = "http://example.test/archive.zip" }, want: "archive URL"},
		{name: "syntax", mutate: func(r *Request) { r.Sources[0].Syntax = "pdf" }, want: "unsupported syntax"},
		{name: "duplicate syntax", mutate: func(r *Request) { r.Sources = append(r.Sources, r.Sources[0]) }, want: "duplicate syntax"},
		{name: "missing phase", mutate: func(r *Request) { r.Phase = "missing" }, want: "not found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			request.Sources = append([]SyntaxSource(nil), valid.Sources...)
			test.mutate(&request)
			_, err := Generate(request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}

	got, err := Generate(valid)
	if err != nil {
		t.Fatal(err)
	}
	rule := got.Syntaxes[0].Patterns[0].Rules[0]
	if rule.Kind != "report" || rule.Severity != "information" || classifyPattern("ubl-extension-pattern") != "extension" || classifyPattern("ubl-cvd-pattern") != "cvd" || classifyPattern("peppol-ubl-pattern") != "peppol" {
		t.Fatalf("unexpected classification or report: %#v", rule)
	}
}

func TestGenerateRejectsCyclesDuplicatesAndEmptyPredicates(t *testing.T) {
	tests := []struct {
		name  string
		main  string
		extra map[string]string
		want  string
	}{
		{name: "cycle", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><include href="source.sch"/></schema>`, want: "include cycle"},
		{name: "duplicate phase", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase id="p"/><phase id="p"/></schema>`, want: "duplicate phase"},
		{name: "phase without ID", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase/></schema>`, want: "phase without ID"},
		{name: "active without pattern", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase id="p"><active/></phase></schema>`, want: "without pattern"},
		{name: "duplicate pattern", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><pattern id="x"/><pattern id="x"/></schema>`, want: "duplicate pattern"},
		{name: "pattern without ID", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><pattern/></schema>`, want: "pattern without ID"},
		{name: "empty context", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase id="p"><active pattern="x"/></phase><pattern id="x"><rule><assert id="R" flag="fatal" test="true()"/></rule></pattern></schema>`, want: "without context"},
		{name: "empty test", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase id="p"><active pattern="x"/></phase><pattern id="x"><rule context="/"><assert id="R" flag="fatal"/></rule></pattern></schema>`, want: "empty test"},
		{name: "check without ID", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><pattern id="x"><rule context="/"><assert flag="fatal" test="true()"/></rule></pattern></schema>`, want: "without ID"},
		{name: "conflicting binding", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron" queryBinding="xslt2"><include href="other.sch"/></schema>`, extra: map[string]string{"other.sch": `<schema xmlns="http://purl.oclc.org/dsdl/schematron" queryBinding="xslt3"/>`}, want: "conflicting query bindings"},
		{name: "duplicate activation", main: `<schema xmlns="http://purl.oclc.org/dsdl/schematron"><phase id="p"><active pattern="x"/><active pattern="x"/></phase><pattern id="x"/></schema>`, want: "activated more than once"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "source.sch")
			writeFile(t, path, test.main)
			for name, content := range test.extra {
				writeFile(t, filepath.Join(root, name), content)
			}
			request := validRequest(root)
			request.Sources = []SyntaxSource{{Syntax: "cii", Path: "source.sch", SHA256: digestFile(t, path)}}
			_, err := Generate(request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestFileDigestRejectsNonRegularAndOversizedSource(t *testing.T) {
	if _, err := fileSHA256(t.TempDir()); err == nil || !strings.Contains(err.Error(), "regular") {
		t.Fatalf("unexpected directory error: %v", err)
	}
	path := filepath.Join(t.TempDir(), "large.sch")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxSourceBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fileSHA256(path); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected size error: %v", err)
	}
}

func validRequest(root string) Request {
	return Request{
		Root: root, Phase: "p", Profile: "XRechnung", ProfileVersion: "3.0.2", RuleVersion: "v2.5.0",
		Archive: Archive{Repository: "owner/repo", URL: "https://example.test/archive.zip", Version: "v2.5.0", SHA256: strings.Repeat("a", 64), License: "Apache-2.0"},
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func digestFile(t *testing.T, path string) string {
	t.Helper()
	digest, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
