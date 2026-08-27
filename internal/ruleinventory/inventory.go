// Package ruleinventory extracts a deterministic rule inventory from pinned
// Schematron sources. It is build tooling; it is not a runtime XPath engine.
package ruleinventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const schematronNamespace = "http://purl.oclc.org/dsdl/schematron"

const maxSourceBytes = 8 << 20

// Inventory is the deterministic, machine-readable description of a pinned
// Schematron phase.
type Inventory struct {
	SchemaVersion  int               `json:"schemaVersion"`
	Profile        string            `json:"profile"`
	ProfileVersion string            `json:"profileVersion"`
	RuleVersion    string            `json:"ruleVersion"`
	Phase          string            `json:"phase"`
	Archive        Archive           `json:"archive"`
	Capabilities   []Capability      `json:"capabilities"`
	Syntaxes       []SyntaxInventory `json:"syntaxes"`
}

// Archive identifies the immutable upstream archive from which member files
// were extracted.
type Archive struct {
	Repository string `json:"repository"`
	URL        string `json:"url"`
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	License    string `json:"license"`
}

// Capability separates predicates that apply to different profile variants.
// Supported is deliberately false until runtime mappings and witnesses pass.
type Capability struct {
	Group     string `json:"group"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// SyntaxInventory contains one syntax-specific Schematron inventory.
type SyntaxInventory struct {
	Syntax       string    `json:"syntax"`
	Source       Source    `json:"source"`
	QueryBinding string    `json:"queryBinding"`
	Patterns     []Pattern `json:"patterns"`
	RuleCount    int       `json:"ruleCount"`
}

// Source identifies one member inside the pinned archive.
type Source struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Pattern is an active Schematron pattern and its executable predicates.
type Pattern struct {
	ID    string `json:"id"`
	Group string `json:"group"`
	Rules []Rule `json:"rules,omitempty"`
}

// Rule is a Schematron assert or report. Digest binds all executable inputs.
type Rule struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Context  string `json:"context"`
	Test     string `json:"test"`
	Digest   string `json:"digest"`
}

// Request describes the pinned sources to inventory.
type Request struct {
	Root           string
	Phase          string
	Profile        string
	ProfileVersion string
	RuleVersion    string
	Archive        Archive
	Sources        []SyntaxSource
}

// SyntaxSource maps a syntax to a member of the Schematron archive.
type SyntaxSource struct {
	Syntax string
	Path   string
	SHA256 string
}

type document struct {
	queryBinding string
	phases       map[string][]string
	patterns     map[string]rawPattern
}

type rawPattern struct {
	id    string
	rules []rawRule
}

type rawRule struct {
	context string
	checks  []rawCheck
}

type rawCheck struct {
	id       string
	kind     string
	severity string
	test     string
}

// Generate loads the requested Schematron members and returns a deterministic
// inventory. Includes are restricted to Root and resolved without networking.
func Generate(request Request) (Inventory, error) {
	if strings.TrimSpace(request.Root) == "" {
		return Inventory{}, errors.New("rule inventory root is required")
	}
	if strings.TrimSpace(request.Phase) == "" {
		return Inventory{}, errors.New("schematron phase is required")
	}
	if len(request.Sources) == 0 {
		return Inventory{}, errors.New("at least one syntax source is required")
	}
	if err := validateProvenance(request); err != nil {
		return Inventory{}, err
	}

	root, err := filepath.Abs(request.Root)
	if err != nil {
		return Inventory{}, fmt.Errorf("resolve source root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Inventory{}, fmt.Errorf("resolve source root symlinks: %w", err)
	}

	result := Inventory{
		SchemaVersion:  1,
		Profile:        request.Profile,
		ProfileVersion: request.ProfileVersion,
		RuleVersion:    request.RuleVersion,
		Phase:          request.Phase,
		Archive:        request.Archive,
		Capabilities: []Capability{
			{Group: "infrastructure", Supported: false, Reason: "inventory metadata only"},
			{Group: "standard", Supported: false, Reason: "runtime mapping and witness gates pending"},
			{Group: "peppol", Supported: false, Reason: "runtime mapping and witness gates pending"},
			{Group: "extension", Supported: false, Reason: "profile is outside the initial release scope"},
			{Group: "cvd", Supported: false, Reason: "profile is outside the initial release scope"},
		},
	}

	seenSyntax := make(map[string]struct{}, len(request.Sources))
	for _, source := range request.Sources {
		syntax := strings.ToLower(strings.TrimSpace(source.Syntax))
		if syntax != "cii" && syntax != "ubl" {
			return Inventory{}, fmt.Errorf("unsupported syntax %q", source.Syntax)
		}
		if _, duplicate := seenSyntax[syntax]; duplicate {
			return Inventory{}, fmt.Errorf("duplicate syntax %q", syntax)
		}
		seenSyntax[syntax] = struct{}{}

		memberPath, err := resolveMember(root, source.Path)
		if err != nil {
			return Inventory{}, fmt.Errorf("resolve %s source: %w", syntax, err)
		}
		digest, err := fileSHA256(memberPath)
		if err != nil {
			return Inventory{}, fmt.Errorf("digest %s source: %w", syntax, err)
		}
		if !equalDigest(digest, source.SHA256) {
			return Inventory{}, fmt.Errorf("%s source digest mismatch: got %s", syntax, digest)
		}

		doc := document{phases: map[string][]string{}, patterns: map[string]rawPattern{}}
		if err := loadDocument(root, memberPath, map[string]bool{}, &doc); err != nil {
			return Inventory{}, fmt.Errorf("parse %s source: %w", syntax, err)
		}
		active, ok := doc.phases[request.Phase]
		if !ok {
			return Inventory{}, fmt.Errorf("phase %q not found in %s source", request.Phase, syntax)
		}
		if len(active) == 0 {
			return Inventory{}, fmt.Errorf("phase %q has no active patterns in %s source", request.Phase, syntax)
		}

		syntaxResult := SyntaxInventory{
			Syntax:       syntax,
			Source:       Source{Path: filepath.ToSlash(source.Path), SHA256: digest},
			QueryBinding: doc.queryBinding,
		}
		seenPattern := make(map[string]struct{}, len(active))
		seenRule := make(map[string]struct{})
		for _, patternID := range active {
			if _, duplicate := seenPattern[patternID]; duplicate {
				return Inventory{}, fmt.Errorf("pattern %q activated more than once for %s", patternID, syntax)
			}
			seenPattern[patternID] = struct{}{}
			pattern, found := doc.patterns[patternID]
			if !found {
				return Inventory{}, fmt.Errorf("active pattern %q unresolved for %s", patternID, syntax)
			}
			outputPattern := Pattern{ID: pattern.id, Group: classifyPattern(pattern.id)}
			for _, rawRule := range pattern.rules {
				context := normalizeExpression(rawRule.context)
				if context == "" {
					return Inventory{}, fmt.Errorf("pattern %q contains a rule without context", pattern.id)
				}
				for _, check := range rawRule.checks {
					if _, duplicate := seenRule[check.id]; duplicate {
						return Inventory{}, fmt.Errorf("duplicate rule ID %q for %s", check.id, syntax)
					}
					seenRule[check.id] = struct{}{}
					if !validSeverity(check.severity) {
						return Inventory{}, fmt.Errorf("unknown severity %q for %s rule %s", check.severity, syntax, check.id)
					}
					test := normalizeExpression(check.test)
					if test == "" {
						return Inventory{}, fmt.Errorf("rule %s has an empty test", check.id)
					}
					rule := Rule{
						ID: check.id, Kind: check.kind, Severity: check.severity,
						Context: context, Test: test,
					}
					rule.Digest = ruleDigest(pattern.id, rule)
					outputPattern.Rules = append(outputPattern.Rules, rule)
					syntaxResult.RuleCount++
				}
			}
			syntaxResult.Patterns = append(syntaxResult.Patterns, outputPattern)
		}
		result.Syntaxes = append(result.Syntaxes, syntaxResult)
	}

	sort.Slice(result.Syntaxes, func(i, j int) bool { return result.Syntaxes[i].Syntax < result.Syntaxes[j].Syntax })
	return result, nil
}

// Marshal returns canonical indented JSON with a terminating newline.
func Marshal(inventory Inventory) ([]byte, error) {
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal inventory: %w", err)
	}
	return append(data, '\n'), nil
}

func loadDocument(root, path string, loading map[string]bool, result *document) error {
	if loading[path] {
		return fmt.Errorf("include cycle at %s", filepath.Base(path))
	}
	loading[path] = true
	defer delete(loading, path)

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	decoder := xml.NewDecoder(io.LimitReader(file, 8<<20))
	decoder.Strict = true
	var currentPattern *rawPattern
	var currentRule *rawRule
	var currentPhase string

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.Directive:
			return errors.New("XML directives are not allowed in Schematron sources")
		case xml.ProcInst:
			if strings.ToLower(value.Target) != "xml" {
				return fmt.Errorf("processing instruction %q is not allowed", value.Target)
			}
		case xml.StartElement:
			if value.Name.Space != "" && value.Name.Space != schematronNamespace {
				continue
			}
			switch value.Name.Local {
			case "schema":
				if binding := attr(value.Attr, "queryBinding"); binding != "" {
					if result.queryBinding != "" && result.queryBinding != binding {
						return fmt.Errorf("conflicting query bindings %q and %q", result.queryBinding, binding)
					}
					result.queryBinding = binding
				}
			case "include":
				href := attr(value.Attr, "href")
				included, err := resolveInclude(root, filepath.Dir(path), href)
				if err != nil {
					return err
				}
				if err := loadDocument(root, included, loading, result); err != nil {
					return fmt.Errorf("include %q: %w", href, err)
				}
			case "phase":
				currentPhase = strings.TrimSpace(attr(value.Attr, "id"))
				if currentPhase == "" {
					return errors.New("phase without ID")
				}
				if _, duplicate := result.phases[currentPhase]; duplicate {
					return fmt.Errorf("duplicate phase %q", currentPhase)
				}
				result.phases[currentPhase] = nil
			case "active":
				if currentPhase != "" {
					patternID := strings.TrimSpace(attr(value.Attr, "pattern"))
					if patternID == "" {
						return fmt.Errorf("phase %q has active entry without pattern", currentPhase)
					}
					result.phases[currentPhase] = append(result.phases[currentPhase], patternID)
				}
			case "pattern":
				id := strings.TrimSpace(attr(value.Attr, "id"))
				if id == "" {
					return errors.New("pattern without ID")
				}
				if _, duplicate := result.patterns[id]; duplicate {
					return fmt.Errorf("duplicate pattern %q", id)
				}
				pattern := rawPattern{id: id}
				currentPattern = &pattern
			case "rule":
				if currentPattern != nil {
					rule := rawRule{context: attr(value.Attr, "context")}
					currentRule = &rule
				}
			case "assert", "report":
				if currentRule != nil {
					check := rawCheck{
						id: strings.TrimSpace(attr(value.Attr, "id")), kind: value.Name.Local,
						severity: strings.ToLower(strings.TrimSpace(attr(value.Attr, "flag"))),
						test:     attr(value.Attr, "test"),
					}
					if check.id == "" {
						return errors.New("assert/report without ID")
					}
					currentRule.checks = append(currentRule.checks, check)
				}
			}
		case xml.EndElement:
			if value.Name.Space != "" && value.Name.Space != schematronNamespace {
				continue
			}
			switch value.Name.Local {
			case "phase":
				currentPhase = ""
			case "rule":
				if currentPattern != nil && currentRule != nil {
					currentPattern.rules = append(currentPattern.rules, *currentRule)
				}
				currentRule = nil
			case "pattern":
				if currentPattern != nil {
					result.patterns[currentPattern.id] = *currentPattern
				}
				currentPattern = nil
			}
		}
	}
	return nil
}

func resolveInclude(root, base, href string) (string, error) {
	if strings.TrimSpace(href) == "" {
		return "", errors.New("include without href")
	}
	if strings.Contains(href, "://") || filepath.IsAbs(href) {
		return "", fmt.Errorf("non-local include %q is not allowed", href)
	}
	return resolveMember(root, filepath.Join(base, href))
}

func resolveMember(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(path))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes source root", path)
	}
	return resolved, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("source is not a regular file")
	}
	if info.Size() > maxSourceBytes {
		return "", fmt.Errorf("source exceeds %d bytes", maxSourceBytes)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validateProvenance(request Request) error {
	for name, value := range map[string]string{
		"profile": request.Profile, "profile version": request.ProfileVersion,
		"rule version": request.RuleVersion, "archive repository": request.Archive.Repository,
		"archive version": request.Archive.Version, "archive license": request.Archive.License,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if !validSHA256(request.Archive.SHA256) {
		return errors.New("archive SHA-256 is invalid")
	}
	parsed, err := url.Parse(request.Archive.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("archive URL must be an unauthenticated HTTPS URL")
	}
	return nil
}

func validSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func equalDigest(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right)) && len(strings.TrimSpace(right)) == sha256.Size*2
}

func ruleDigest(pattern string, rule Rule) string {
	payload := strings.Join([]string{pattern, rule.ID, rule.Kind, rule.Severity, rule.Context, rule.Test}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func normalizeExpression(value string) string { return strings.Join(strings.Fields(value), " ") }

func validSeverity(value string) bool {
	switch value {
	case "fatal", "error", "warning", "information":
		return true
	default:
		return false
	}
}

func classifyPattern(id string) string {
	lower := strings.ToLower(id)
	switch {
	case strings.Contains(lower, "extension"):
		return "extension"
	case strings.Contains(lower, "cvd"):
		return "cvd"
	case strings.Contains(lower, "peppol"):
		return "peppol"
	case lower == "variable-pattern":
		return "infrastructure"
	default:
		return "standard"
	}
}

func attr(attributes []xml.Attr, name string) string {
	for _, attribute := range attributes {
		if attribute.Name.Local == name {
			return attribute.Value
		}
	}
	return ""
}
