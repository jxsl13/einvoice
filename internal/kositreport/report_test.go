package kositreport

import (
	"strings"
	"testing"
)

func TestParseNormalizesAssessmentAndMessages(t *testing.T) {
	t.Parallel()

	report, err := Parse(strings.NewReader(`<rep:report xmlns:rep="urn:report">
 <rep:documentReference> /work/invoice.xml </rep:documentReference>
 <rep:validationResults>
	  <rep:message level="warning">[BR-W-1] warning that contributes to rejection</rep:message>
  <rep:message code="BR-2" level="error"/>
  <rep:message level="fatal" code="BR-1"/>
  <rep:message level="error" code="BR-2"/>
 </rep:validationResults>
 <rep:assessment><rep:reject/></rep:assessment>
</rep:report>`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if report.DocumentReference != "/work/invoice.xml" || report.Accepted {
		t.Fatalf("unexpected report: %#v", report)
	}
	if got := strings.Join(report.RejectingRuleIDs(), ","); got != "BR-1,BR-2" {
		t.Fatalf("rejecting IDs = %q", got)
	}
}

func TestParseAcceptsNamespacelessFixture(t *testing.T) {
	t.Parallel()

	report, err := Parse(strings.NewReader(`<report><documentReference>x.xml</documentReference><assessment><accept/></assessment></report>`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !report.Accepted {
		t.Fatal("accepted assessment was not recognized")
	}
}

func TestParseRejectsInvalidReports(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "nil", input: ""},
		{name: "malformed", input: `<report>`},
		{name: "reference", input: `<report><assessment><accept/></assessment></report>`},
		{name: "assessment", input: `<report><documentReference>x</documentReference></report>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(test.input)); err == nil {
				t.Fatal("expected parsing error")
			}
		})
	}
}

func TestParseRejectsNilReader(t *testing.T) {
	t.Parallel()
	if _, err := Parse(nil); err == nil {
		t.Fatal("expected nil-reader error")
	}
}
