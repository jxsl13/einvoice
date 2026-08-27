package validator_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	einvoice "github.com/jxsl13/einvoice"
	"github.com/jxsl13/einvoice/validator"
)

func TestPublicAPIExposesTypedSanitizedErrors(t *testing.T) {
	t.Parallel()
	input := `<Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2" ` +
		`xmlns:cbc="urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2">` +
		`<cbc:CustomizationID>PRIVATE-UNSUPPORTED-PROFILE</cbc:CustomizationID></Invoice>`
	result, err := validator.Validate(context.Background(), strings.NewReader(input), validator.Options{})
	var validationErr *validator.Error
	if !errors.As(err, &validationErr) || validationErr.Kind != validator.ErrorUnsupportedProfile {
		t.Fatalf("Validate() error = %v, want unsupported-profile *validator.Error", err)
	}
	if !reflect.DeepEqual(result, validator.Result{}) {
		t.Fatalf("Validate() result = %#v, want zero result", result)
	}
	if strings.Contains(err.Error(), "PRIVATE-UNSUPPORTED-PROFILE") {
		t.Fatalf("error exposed an invoice value: %v", err)
	}
}

func TestWrittenCIIReferencePassesEmbeddedSchema(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../testdata/cii/en16931/zugferd-en16931-einfach.xml")
	if err != nil {
		t.Fatal(err)
	}
	invoice, err := einvoice.ParseReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	invoice.AdditionalReferencedDocument = append(invoice.AdditionalReferencedDocument, einvoice.Document{
		IssuerAssignedID: "ATTACHMENT-1",
		URIID:            "https://example.test/invoice.pdf",
		TypeCode:         "916",
	})

	var written bytes.Buffer
	if err := invoice.Write(&written); err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(context.Background(), &written, validator.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range result.Findings {
		if finding.RuleID == "XSD" {
			t.Fatalf("writer produced schema-invalid CII: %#v", result.Findings)
		}
	}
}
