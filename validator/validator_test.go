package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	einvoice "github.com/jxsl13/einvoice"
)

func TestValidateSupportedFixturesDeterministically(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		syntax  Syntax
		profile Profile
	}{
		{
			name:    "UBL EN 16931",
			path:    "../testdata/ubl/invoice/ubl-tc434-example1.xml",
			syntax:  SyntaxUBL,
			profile: ProfileEN16931,
		},
		{
			name:    "CII EN 16931",
			path:    "../testdata/cii/en16931/zugferd-en16931-einfach.xml",
			syntax:  SyntaxCII,
			profile: ProfileEN16931,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			first, err := Validate(context.Background(), bytes.NewReader(data), Options{})
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			second, err := Validate(context.Background(), bytes.NewReader(data), Options{})
			if err != nil {
				t.Fatalf("second Validate() error = %v", err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("results are not deterministic:\nfirst=%#v\nsecond=%#v", first, second)
			}
			if first.Syntax != test.syntax || first.Profile != test.profile || first.RulePack != RulePackBootstrap {
				t.Fatalf("unexpected identity: %#v", first)
			}
			if len(first.Findings) > HardMaxFindings {
				t.Fatalf("findings = %d, want <= %d", len(first.Findings), HardMaxFindings)
			}
			assertSorted(t, first.Findings)
		})
	}
}

func TestValidateRejectsUnsupportedProfilesBeforeRuleWeakening(t *testing.T) {
	t.Parallel()
	result, err := Validate(context.Background(), strings.NewReader(ubl("urn:example:unsupported", "")), Options{})
	assertErrorKind(t, err, ErrorUnsupportedProfile, "")
	if !reflect.DeepEqual(result, Result{}) {
		t.Fatalf("result = %#v, want zero result", result)
	}

	xrechnung, err := Validate(context.Background(), strings.NewReader(ubl(
		"urn:cen.eu:en16931:2017#compliant#urn:xeinkauf.de:kosit:xrechnung_3.0",
		"<cbc:ID>SECRET-INVOICE-VALUE</cbc:ID>",
	)), Options{})
	if err != nil {
		t.Fatalf("XRechnung Validate() error = %v", err)
	}
	if xrechnung.Profile != ProfileXRechnung30 || xrechnung.Accepted || len(xrechnung.Findings) == 0 {
		t.Fatalf("unexpected XRechnung result: %#v", xrechnung)
	}
	encoded, err := json.Marshal(xrechnung)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "SECRET-INVOICE-VALUE") {
		t.Fatalf("result exposed an invoice value: %s", encoded)
	}
}

func TestValidateEnforcesXMLLimits(t *testing.T) {
	t.Parallel()
	attributes := make([]string, HardMaxAttributes+1)
	for index := range attributes {
		attributes[index] = fmt.Sprintf(" a%d=\"x\"", index)
	}
	manyElements := strings.Repeat("<x/>", HardMaxElements)

	tests := []struct {
		name    string
		xml     string
		options Options
		limit   string
	}{
		{name: "bytes", xml: ubl("urn:cen.eu:en16931:2017", ""), options: Options{MaxBytes: 8}, limit: "max_bytes"},
		{name: "depth", xml: ubl("urn:cen.eu:en16931:2017", "<a><b/></a>"), options: Options{MaxDepth: 2}, limit: "max_depth"},
		{
			name:  "attributes",
			xml:   `<Invoice xmlns="` + ublInvoiceNS + `"` + strings.Join(attributes, "") + `/>`,
			limit: "max_attributes",
		},
		{name: "elements", xml: `<Invoice xmlns="` + ublInvoiceNS + `">` + manyElements + `</Invoice>`, limit: "max_elements"},
		{name: "text", xml: `<Invoice xmlns="` + ublInvoiceNS + `">` + strings.Repeat("x", HardMaxTextBytes+1) + `</Invoice>`, limit: "max_text_bytes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Validate(context.Background(), strings.NewReader(test.xml), test.options)
			assertErrorKind(t, err, ErrorResourceLimit, test.limit)
		})
	}
}

func TestValidateRejectsUnsafeAndMalformedXML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		xml  string
		kind ErrorKind
	}{
		{name: "empty", xml: "", kind: ErrorMalformedInput},
		{name: "unknown root", xml: `<invoice/>`, kind: ErrorUnsupportedSyntax},
		{name: "namespace confusion", xml: `<Other xmlns="` + ublInvoiceNS + `"/>`, kind: ErrorUnsupportedSyntax},
		{name: "unclosed", xml: `<Invoice xmlns="` + ublInvoiceNS + `">`, kind: ErrorMalformedInput},
		{name: "multiple roots", xml: `<Invoice xmlns="` + ublInvoiceNS + `"/><Invoice xmlns="` + ublInvoiceNS + `"/>`, kind: ErrorMalformedInput},
		{
			name: "duplicate profile",
			xml:  ubl("urn:cen.eu:en16931:2017", `<cbc:CustomizationID>urn:example:second</cbc:CustomizationID>`),
			kind: ErrorMalformedInput,
		},
		{name: "DTD", xml: `<!DOCTYPE Invoice><Invoice xmlns="` + ublInvoiceNS + `"/>`, kind: ErrorMalformedInput},
		{name: "processing instruction", xml: `<?unsafe value?><Invoice xmlns="` + ublInvoiceNS + `"/>`, kind: ErrorMalformedInput},
		{
			name: "XInclude",
			xml:  `<Invoice xmlns="` + ublInvoiceNS + `"><xi:include xmlns:xi="` + xincludeNamespace + `" href="file:///etc/passwd"/></Invoice>`,
			kind: ErrorMalformedInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Validate(context.Background(), strings.NewReader(test.xml), Options{})
			assertErrorKind(t, err, test.kind, "")
			if (test.xml != "" && strings.Contains(fmt.Sprint(err), test.xml)) ||
				strings.Contains(fmt.Sprint(err), "passwd") {
				t.Fatalf("error exposed input: %v", err)
			}
		})
	}
}

func TestValidateRejectsInvalidOptionsAndNilArguments(t *testing.T) {
	t.Parallel()
	validXML := ubl("urn:cen.eu:en16931:2017", "")
	tests := []struct {
		name    string
		ctx     context.Context
		src     io.Reader
		options Options
		kind    ErrorKind
		limit   string
	}{
		{name: "nil context", src: strings.NewReader(validXML), kind: ErrorInvalidOptions, limit: "nil_argument"},
		{name: "nil reader", ctx: context.Background(), kind: ErrorInvalidOptions, limit: "nil_argument"},
		{name: "rule pack", ctx: context.Background(), src: strings.NewReader(validXML), options: Options{RulePack: "future"}, kind: ErrorUnsupportedRulePack, limit: "rule_pack"},
		{name: "negative bytes", ctx: context.Background(), src: strings.NewReader(validXML), options: Options{MaxBytes: -1}, kind: ErrorInvalidOptions, limit: "max_bytes"},
		{name: "large bytes", ctx: context.Background(), src: strings.NewReader(validXML), options: Options{MaxBytes: HardMaxBytes + 1}, kind: ErrorInvalidOptions, limit: "max_bytes"},
		{name: "large depth", ctx: context.Background(), src: strings.NewReader(validXML), options: Options{MaxDepth: HardMaxDepth + 1}, kind: ErrorInvalidOptions, limit: "max_depth"},
		{name: "large findings", ctx: context.Background(), src: strings.NewReader(validXML), options: Options{MaxFindings: HardMaxFindings + 1}, kind: ErrorInvalidOptions, limit: "max_findings"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Validate(test.ctx, test.src, test.options)
			assertErrorKind(t, err, test.kind, test.limit)
		})
	}
}

func TestValidateHonorsCancellationAndContainsPanics(t *testing.T) {
	t.Parallel()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Validate(canceled, panicReader{}, Options{})
	assertErrorKind(t, err, ErrorCanceled, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(%v, context.Canceled) = false", err)
	}

	_, err = Validate(context.Background(), panicReader{}, Options{})
	assertErrorKind(t, err, ErrorInternal, "panic")

	ctx, cancelDuringRead := context.WithCancel(context.Background())
	_, err = Validate(ctx, &cancelingReader{cancel: cancelDuringRead}, Options{})
	assertErrorKind(t, err, ErrorCanceled, "")
}

func TestBoundFindingsSortsAndTruncates(t *testing.T) {
	t.Parallel()
	findings := []Finding{
		{RuleID: "B", Severity: SeverityWarning, MessageCode: "rule.b"},
		{RuleID: "C", Severity: SeverityError, MessageCode: "rule.c"},
		{RuleID: "A", Severity: SeverityError, MessageCode: "rule.a"},
		{RuleID: "A", Severity: SeverityWarning, MessageCode: "rule.a"},
	}
	got := boundFindings(findings, 3)
	want := []Finding{
		{RuleID: "A", Severity: SeverityError, MessageCode: "rule.a"},
		{RuleID: "C", Severity: SeverityError, MessageCode: "rule.c"},
		{RuleID: truncationRuleID, Severity: SeverityInfo, MessageCode: truncationMsgCode},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("boundFindings() = %#v, want %#v", got, want)
	}
}

func TestTypedErrorAndHelperEdges(t *testing.T) {
	t.Parallel()
	var nilError *Error
	if nilError.Error() != "validator: unknown failure" || nilError.Unwrap() != nil {
		t.Fatalf("unexpected nil error behavior")
	}
	internal := failure(ErrorInternal, "", nil)
	if internal.Error() != "validator: internal" || internal.Unwrap() != nil {
		t.Fatalf("unexpected internal error behavior: %v", internal)
	}

	empty := semanticFinding(structuredError(), SeverityError)
	if empty.RuleID != "UNKNOWN-RULE" || empty.MessageCode != "rule.unknown_rule" {
		t.Fatalf("unexpected empty-rule finding: %#v", empty)
	}
	for severity, rank := range map[Severity]int{
		SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2, Severity("future"): 3,
	} {
		if got := severityRank(severity); got != rank {
			t.Fatalf("severityRank(%q) = %d, want %d", severity, got, rank)
		}
	}
	if syntax, err := rootSyntax(xmlName(ublCreditNoteNS, "CreditNote")); err != nil || syntax != SyntaxUBL {
		t.Fatalf("credit-note root = %q, %v", syntax, err)
	}

	_, err := Validate(context.Background(), errorReader{}, Options{})
	assertErrorKind(t, err, ErrorInternal, "input_read")
}

func FuzzValidate(f *testing.F) {
	f.Add([]byte(ubl("urn:cen.eu:en16931:2017", "")))
	f.Add([]byte(`<unsafe/>`))
	f.Add([]byte(`<!DOCTYPE x><x/>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		result, _ := Validate(context.Background(), bytes.NewReader(data), Options{MaxBytes: 64 << 10})
		if len(result.Findings) > HardMaxFindings {
			t.Fatalf("findings = %d, want <= %d", len(result.Findings), HardMaxFindings)
		}
	})
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) {
	panic("reader secret must not escape")
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("reader secret must not escape")
}

type cancelingReader struct {
	cancel context.CancelFunc
	read   bool
}

func (r *cancelingReader) Read(buffer []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	copy(buffer, "<Invoice")
	r.cancel()
	return len("<Invoice"), nil
}

func ubl(profile, body string) string {
	return `<Invoice xmlns="` + ublInvoiceNS + `" xmlns:cbc="urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2">` +
		`<cbc:CustomizationID>` + profile + `</cbc:CustomizationID>` + body + `</Invoice>`
}

func structuredError() einvoice.SemanticError {
	return einvoice.SemanticError{}
}

func xmlName(space, local string) xml.Name {
	return xml.Name{Space: space, Local: local}
}

func assertErrorKind(t *testing.T, err error, kind ErrorKind, limit string) {
	t.Helper()
	var validatorErr *Error
	if !errors.As(err, &validatorErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if validatorErr.Kind != kind || validatorErr.Limit != limit {
		t.Fatalf("error = %#v, want kind=%q limit=%q", validatorErr, kind, limit)
	}
}

func assertSorted(t *testing.T, findings []Finding) {
	t.Helper()
	want := append([]Finding(nil), findings...)
	want = boundFindings(want, HardMaxFindings)
	if !slices.Equal(findings, want) {
		t.Fatalf("findings are not sorted: %#v", findings)
	}
}
