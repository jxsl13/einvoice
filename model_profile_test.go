package einvoice

import "testing"

func TestCodeProfileTypeStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		profile CodeProfileType
		label   string
		name    string
	}{
		{CProfileUnknown, "unknown profile", "Unknown"},
		{CProfileMinimum, "minimum", SpecFacturXMinimum},
		{CProfileBasicWL, "basic without lines", SpecFacturXBasicWL},
		{CProfileBasic, "basic", SpecFacturXBasic},
		{CProfileEN16931, "EN 16931", SpecEN16931},
		{CProfileExtended, "extended", SpecFacturXExtended},
		{CProfileXRechnung, "XRechnung", SpecXRechnung30},
		{CodeProfileType(99), "unknown", "unknown"},
	}

	for _, test := range tests {
		if got := test.profile.String(); got != test.label {
			t.Errorf("CodeProfileType(%d).String() = %q, want %q", test.profile, got, test.label)
		}
		if got := test.profile.ToProfileName(); got != test.name {
			t.Errorf("CodeProfileType(%d).ToProfileName() = %q, want %q", test.profile, got, test.name)
		}
	}
}

func TestModelStrings(t *testing.T) {
	t.Parallel()

	schemas := []struct {
		schema CodeSchemaType
		want   string
	}{
		{CII, "ZUGFeRD/Factur-X"},
		{UBL, "UBL"},
		{SchemaTypeUnknown, "unknown"},
		{CodeSchemaType(99), "unknown"},
	}
	for _, test := range schemas {
		if got := test.schema.String(); got != test.want {
			t.Errorf("CodeSchemaType(%d).String() = %q, want %q", test.schema, got, test.want)
		}
	}

	if got := CodeDocument(380).String(); got != "380" {
		t.Errorf("CodeDocument(380).String() = %q, want %q", got, "380")
	}
	if got := (Note{SubjectCode: "AAI", Text: "Details"}).String(); got != `Notiz AAI - "Details"` {
		t.Errorf("Note.String() = %q", got)
	}
}

func TestProfileURNsAndNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		urn  string
		name string
	}{
		{SpecFacturXMinimum, "Factur-X Minimum"},
		{SpecFacturXBasicWL, "Factur-X Basic WL"},
		{SpecFacturXBasic, "Factur-X Basic"},
		{SpecFacturXBasicAlt, "Factur-X Basic"},
		{SpecFacturXExtended, "Factur-X Extended"},
		{SpecZUGFeRDMinimum, "ZUGFeRD Minimum"},
		{SpecZUGFeRDBasic, "ZUGFeRD Basic"},
		{SpecZUGFeRDExtended, "ZUGFeRD Extended"},
		{SpecEN16931, "EN 16931"},
		{SpecXRechnung20, "XRechnung 2.0"},
		{SpecXRechnung21, "XRechnung 2.1"},
		{SpecXRechnung22, "XRechnung 2.2"},
		{SpecXRechnung23, "XRechnung 2.3"},
		{SpecXRechnung30, "XRechnung 3.0"},
	}

	for _, test := range tests {
		if !IsProfileURN(test.urn) {
			t.Errorf("IsProfileURN(%q) = false, want true", test.urn)
		}
		if got := GetProfileName(test.urn); got != test.name {
			t.Errorf("GetProfileName(%q) = %q, want %q", test.urn, got, test.name)
		}
	}

	if IsProfileURN("urn:example:unknown") {
		t.Error("IsProfileURN accepted an unknown URN")
	}
	if got := GetProfileName("urn:example:unknown"); got != "Unknown" {
		t.Errorf("GetProfileName(unknown) = %q, want %q", got, "Unknown")
	}
}
