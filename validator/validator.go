package validator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"

	"github.com/jacoelho/xsd/xsderrors"
	einvoice "github.com/jxsl13/einvoice"
	"github.com/jxsl13/einvoice/internal/xsdvalidate"
)

// Validate parses and validates one invoice through hard resource ceilings.
// Business-rule rejection is returned in Result with a nil operational error.
func Validate(ctx context.Context, src io.Reader, options Options) (result Result, err error) {
	defer func() {
		if recover() != nil {
			result = Result{}
			err = failure(ErrorInternal, "panic", nil)
		}
	}()

	if ctx == nil || src == nil {
		return Result{}, failure(ErrorInvalidOptions, "nil_argument", nil)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return Result{}, failure(ErrorCanceled, "", contextErr)
	}
	bounded, err := normalizeOptions(options)
	if err != nil {
		return Result{}, err
	}
	result.RulePack = bounded.rulePack

	data, err := readBounded(ctx, src, bounded.maxBytes)
	if err != nil {
		return Result{}, err
	}
	result.Syntax, err = inspectXML(ctx, data, bounded.maxDepth)
	if err != nil {
		return Result{}, err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return Result{}, failure(ErrorCanceled, "", contextErr)
	}
	if bounded.rulePack == RulePackXRechnung302 {
		if schemaErr := xsdvalidate.Validate(
			ctx,
			data,
			bounded.maxBytes,
			bounded.maxDepth,
			HardMaxAttributes,
			HardMaxTextBytes,
		); schemaErr != nil {
			return schemaFailure(result, schemaErr, ctx.Err())
		}
	}

	invoice, parseErr := einvoice.ParseReaderContext(ctx, bytes.NewReader(data))
	if parseErr != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return Result{}, failure(ErrorCanceled, "", contextErr)
		}
		return Result{}, failure(ErrorMalformedInput, "", nil)
	}
	if !syntaxMatches(result.Syntax, invoice.SchemaType) {
		return Result{}, failure(ErrorInternal, "syntax_mismatch", nil)
	}
	result.Profile, err = admittedProfile(invoice.GuidelineSpecifiedDocumentContextParameter)
	if err != nil {
		return Result{}, err
	}
	if bounded.rulePack == RulePackXRechnung302 && result.Profile != ProfileXRechnung30 {
		return Result{}, failure(ErrorUnsupportedProfile, "", nil)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return Result{}, failure(ErrorCanceled, "", contextErr)
	}

	validationErr := invoice.ValidateContext(ctx)
	if contextErr := ctx.Err(); contextErr != nil {
		return Result{}, failure(ErrorCanceled, "", contextErr)
	}
	var semanticErr *einvoice.ValidationError
	switch {
	case validationErr == nil:
		result.Findings = appendSemanticFindings(nil, invoice.Warnings(), invoice.Information())
		result.Accepted = !hasRejectingXRechnungWarning(result.Findings)
	case errors.As(validationErr, &semanticErr):
		result.Findings = appendSemanticFindings(semanticErr.Violations(), semanticErr.Warnings(), semanticErr.Information())
		result.Accepted = semanticErr.Count() == 0 && !hasRejectingXRechnungWarning(result.Findings)
	default:
		return Result{}, failure(ErrorInternal, "rule_validation", nil)
	}
	if empty, emptyErr := hasForbiddenEmptyElement(ctx, data, result.Syntax); emptyErr != nil {
		return Result{}, failure(ErrorInternal, "empty_element_scan", nil)
	} else if empty && !hasRule(result.Findings, "PEPPOL-EN16931-R008") {
		result.Findings = append(result.Findings, Finding{
			RuleID:      "PEPPOL-EN16931-R008",
			Severity:    SeverityError,
			MessageCode: "rule.peppol_en16931_r008",
		})
		result.Accepted = false
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return Result{}, failure(ErrorCanceled, "", contextErr)
	}
	result.Findings = boundFindings(result.Findings, bounded.maxFindings)
	return result, nil
}

func hasRule(findings []Finding, ruleID string) bool {
	for _, finding := range findings {
		if finding.RuleID == ruleID {
			return true
		}
	}
	return false
}

func schemaFailure(result Result, schemaErr, contextErr error) (Result, error) {
	if contextErr != nil || errors.Is(schemaErr, context.Canceled) || errors.Is(schemaErr, context.DeadlineExceeded) {
		if contextErr == nil {
			contextErr = schemaErr
		}
		return Result{}, failure(ErrorCanceled, "", contextErr)
	}
	var diagnostic *xsderrors.Error
	if !errors.As(schemaErr, &diagnostic) {
		return Result{}, failure(ErrorInternal, "schema_validation", nil)
	}
	if diagnostic.Code == xsderrors.CodeValidationLimit {
		return Result{}, failure(ErrorResourceLimit, "schema_validation", nil)
	}
	if diagnostic.Category != xsderrors.CategoryValidation {
		return Result{}, failure(ErrorInternal, "schema_validation", nil)
	}
	result.Accepted = false
	result.Findings = []Finding{{RuleID: schemaRuleID, Severity: SeverityError, MessageCode: schemaMsgCode}}
	return result, nil
}

func hasRejectingXRechnungWarning(findings []Finding) bool {
	for _, finding := range findings {
		if finding.Severity != SeverityWarning {
			continue
		}
		switch finding.RuleID {
		case "CII-SR-452", "CII-SR-453", "CII-SR-454":
			return true
		}
	}
	return false
}

func admittedProfile(profileURN string) (Profile, error) {
	switch profileURN {
	case einvoice.SpecEN16931:
		return ProfileEN16931, nil
	case einvoice.SpecXRechnung30:
		return ProfileXRechnung30, nil
	default:
		return "", failure(ErrorUnsupportedProfile, "", nil)
	}
}

func syntaxMatches(syntax Syntax, legacy einvoice.CodeSchemaType) bool {
	return syntax == SyntaxCII && legacy == einvoice.CII || syntax == SyntaxUBL && legacy == einvoice.UBL
}

func appendSemanticFindings(violations, warnings, information []einvoice.SemanticError) []Finding {
	findings := make([]Finding, 0, len(violations)+len(warnings)+len(information))
	for _, violation := range violations {
		findings = append(findings, semanticFinding(violation, SeverityError))
	}
	for _, warning := range warnings {
		findings = append(findings, semanticFinding(warning, SeverityWarning))
	}
	for _, info := range information {
		findings = append(findings, semanticFinding(info, SeverityInfo))
	}
	return findings
}

func semanticFinding(value einvoice.SemanticError, severity Severity) Finding {
	ruleID := strings.TrimSpace(value.Rule.Code)
	if ruleID == "" {
		ruleID = "UNKNOWN-RULE"
	}
	messageCode := "rule." + strings.ToLower(strings.ReplaceAll(ruleID, "-", "_"))
	return Finding{RuleID: ruleID, Severity: severity, MessageCode: messageCode}
}

func boundFindings(findings []Finding, maximum int) []Finding {
	sort.SliceStable(findings, func(left, right int) bool {
		leftRank, rightRank := severityRank(findings[left].Severity), severityRank(findings[right].Severity)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if findings[left].RuleID != findings[right].RuleID {
			return findings[left].RuleID < findings[right].RuleID
		}
		if findings[left].Location != findings[right].Location {
			return findings[left].Location < findings[right].Location
		}
		return findings[left].MessageCode < findings[right].MessageCode
	})
	if len(findings) <= maximum {
		return findings
	}
	bounded := make([]Finding, 0, maximum)
	bounded = append(bounded, findings[:maximum-1]...)
	bounded = append(bounded, Finding{
		RuleID:      truncationRuleID,
		Severity:    SeverityInfo,
		MessageCode: truncationMsgCode,
	})
	return bounded
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	default:
		return 3
	}
}
