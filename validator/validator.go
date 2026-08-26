package validator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"

	einvoice "github.com/jxsl13/einvoice"
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
		result.Findings = appendSemanticFindings(nil, invoice.Warnings())
		result.Accepted = true
	case errors.As(validationErr, &semanticErr):
		result.Findings = appendSemanticFindings(semanticErr.Violations(), semanticErr.Warnings())
		result.Accepted = semanticErr.Count() == 0
	default:
		return Result{}, failure(ErrorInternal, "rule_validation", nil)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return Result{}, failure(ErrorCanceled, "", contextErr)
	}
	result.Findings = boundFindings(result.Findings, bounded.maxFindings)
	return result, nil
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

func appendSemanticFindings(violations, warnings []einvoice.SemanticError) []Finding {
	findings := make([]Finding, 0, len(violations)+len(warnings))
	for _, violation := range violations {
		findings = append(findings, semanticFinding(violation, SeverityError))
	}
	for _, warning := range warnings {
		findings = append(findings, semanticFinding(warning, SeverityWarning))
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
