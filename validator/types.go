package validator

import "fmt"

const (
	// RulePackBootstrap identifies the current pre-conformance rule set. It is
	// immutable once published but is not approved for authoritative issuance.
	RulePackBootstrap = "bootstrap-0"

	// HardMaxBytes is the largest XML input accepted by the bounded API.
	HardMaxBytes int64 = 5 << 20
	// HardMaxDepth is the largest XML element nesting depth accepted by the bounded API.
	HardMaxDepth = 64
	// HardMaxElements is the largest number of XML elements accepted by the bounded API.
	HardMaxElements = 100_000
	// HardMaxAttributes is the largest number of attributes accepted on one element.
	HardMaxAttributes = 128
	// HardMaxTextBytes is the largest contiguous XML text node accepted by the bounded API.
	HardMaxTextBytes = 1 << 20
	// HardMaxFindings is the largest number of findings returned by the bounded API.
	HardMaxFindings = 256
)

// Syntax identifies the parsed invoice XML syntax.
type Syntax string

const (
	// SyntaxCII identifies UN/CEFACT Cross Industry Invoice XML.
	SyntaxCII Syntax = "cii"
	// SyntaxUBL identifies UBL Invoice or CreditNote XML.
	SyntaxUBL Syntax = "ubl"
)

// Profile identifies an admitted invoice conformance profile.
type Profile string

const (
	// ProfileEN16931 identifies the base EN 16931 profile.
	ProfileEN16931 Profile = "en16931"
	// ProfileXRechnung30 identifies standard XRechnung 3.0.x documents.
	ProfileXRechnung30 Profile = "xrechnung-3.0"
)

// Severity is the stable severity of one finding.
type Severity string

const (
	// SeverityError rejects the invoice.
	SeverityError Severity = "error"
	// SeverityWarning does not reject the invoice.
	SeverityWarning Severity = "warning"
	// SeverityInfo is reserved for bounded API metadata such as truncation.
	SeverityInfo Severity = "info"
)

// Options controls validation within hard public ceilings. A zero value uses
// secure defaults equal to the hard ceilings. Callers may only reduce limits.
type Options struct {
	RulePack    string
	MaxBytes    int64
	MaxDepth    int
	MaxFindings int
}

// Finding is one deterministic, localization-neutral validation result.
// MessageCode is intended for consumer-owned localization catalogs.
type Finding struct {
	RuleID      string   `json:"ruleId"`
	Severity    Severity `json:"severity"`
	Location    string   `json:"location,omitempty"`
	MessageCode string   `json:"messageCode"`
}

// Result is the complete bounded validation result. Operational failures are
// returned as errors and never represented as business-rule findings.
type Result struct {
	Syntax   Syntax    `json:"syntax"`
	Profile  Profile   `json:"profile"`
	RulePack string    `json:"rulePack"`
	Accepted bool      `json:"accepted"`
	Findings []Finding `json:"findings"`
}

// ErrorKind identifies one typed operational validation failure.
type ErrorKind string

const (
	ErrorInvalidOptions      ErrorKind = "invalid_options"
	ErrorMalformedInput      ErrorKind = "malformed_input"
	ErrorUnsupportedSyntax   ErrorKind = "unsupported_syntax"
	ErrorUnsupportedProfile  ErrorKind = "unsupported_profile"
	ErrorUnsupportedRulePack ErrorKind = "unsupported_rule_pack"
	ErrorResourceLimit       ErrorKind = "resource_limit"
	ErrorCanceled            ErrorKind = "canceled"
	ErrorInternal            ErrorKind = "internal"
)

// Error is a sanitized operational failure. Limit is a stable limit name and
// never contains input data. The underlying cause is retained only for context
// cancellation compatibility.
type Error struct {
	Kind  ErrorKind
	Limit string
	cause error
}

// Error implements error without exposing XML, invoice values, or parser details.
func (e *Error) Error() string {
	if e == nil {
		return "validator: unknown failure"
	}
	if e.Limit != "" {
		return fmt.Sprintf("validator: %s (%s)", e.Kind, e.Limit)
	}
	return fmt.Sprintf("validator: %s", e.Kind)
}

// Unwrap exposes only safe cancellation causes.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type limits struct {
	rulePack    string
	maxBytes    int64
	maxDepth    int
	maxFindings int
}

func normalizeOptions(options Options) (limits, error) {
	value := limits{
		rulePack:    options.RulePack,
		maxBytes:    options.MaxBytes,
		maxDepth:    options.MaxDepth,
		maxFindings: options.MaxFindings,
	}
	if value.rulePack == "" {
		value.rulePack = RulePackBootstrap
	}
	if value.rulePack != RulePackBootstrap {
		return limits{}, failure(ErrorUnsupportedRulePack, "rule_pack", nil)
	}
	if value.maxBytes == 0 {
		value.maxBytes = HardMaxBytes
	}
	if value.maxDepth == 0 {
		value.maxDepth = HardMaxDepth
	}
	if value.maxFindings == 0 {
		value.maxFindings = HardMaxFindings
	}
	if value.maxBytes < 1 || value.maxBytes > HardMaxBytes {
		return limits{}, failure(ErrorInvalidOptions, "max_bytes", nil)
	}
	if value.maxDepth < 1 || value.maxDepth > HardMaxDepth {
		return limits{}, failure(ErrorInvalidOptions, "max_depth", nil)
	}
	if value.maxFindings < 1 || value.maxFindings > HardMaxFindings {
		return limits{}, failure(ErrorInvalidOptions, "max_findings", nil)
	}
	return value, nil
}

func failure(kind ErrorKind, limit string, cause error) *Error {
	return &Error{Kind: kind, Limit: limit, cause: cause}
}
