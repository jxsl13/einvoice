package validator_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

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
