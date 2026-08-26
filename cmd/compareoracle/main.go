// Command compareoracle compares native pure-Go validation results with pinned
// KoSIT VARL reports. It is a conformance-development and CI tool only.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jxsl13/einvoice/internal/kositreport"
	"github.com/jxsl13/einvoice/validator"
)

type mismatch struct {
	Document       string   `json:"document"`
	OracleAccepted bool     `json:"oracleAccepted"`
	NativeAccepted bool     `json:"nativeAccepted"`
	OracleRuleIDs  []string `json:"oracleRuleIds,omitempty"`
	NativeRuleIDs  []string `json:"nativeRuleIds,omitempty"`
	NativeError    string   `json:"nativeError,omitempty"`
}

type summary struct {
	Documents                 int        `json:"documents"`
	SkippedUnsupportedProfile int        `json:"skippedUnsupportedProfiles,omitempty"`
	VerdictMatches            int        `json:"verdictMatches"`
	FalseAccepts              int        `json:"falseAccepts"`
	RuleIDsCompared           int        `json:"ruleIdsCompared"`
	RuleIDMatches             int        `json:"ruleIdMatches"`
	VerdictParity             float64    `json:"verdictParity"`
	RejectRuleIDParity        float64    `json:"rejectRuleIdParity"`
	Mismatches                []mismatch `json:"mismatches,omitempty"`
}

func main() {
	reportsDirectory := flag.String("reports", "", "directory containing KoSIT *-report.xml files")
	sourceRoot := flag.String("source-root", "", "local root containing the validated source XML")
	sourcePrefix := flag.String("source-prefix", "", "prefix to strip from report document references")
	minimumParity := flag.Float64("minimum-parity", 0.999, "minimum verdict and rejecting-rule-ID parity")
	skipUnsupported := flag.Bool("skip-unsupported-profiles", false, "exclude explicitly unsupported profile variants")
	flag.Parse()

	if *reportsDirectory == "" || *sourceRoot == "" || *minimumParity < 0 || *minimumParity > 1 {
		fmt.Fprintln(os.Stderr, "compareoracle: valid -reports, -source-root, and -minimum-parity are required")
		os.Exit(2)
	}

	result, err := compare(*reportsDirectory, *sourceRoot, *sourcePrefix, *skipUnsupported)
	if err != nil {
		fmt.Fprintf(os.Stderr, "compareoracle: %v\n", err)
		os.Exit(2)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "compareoracle: encode result: %v\n", err)
		os.Exit(2)
	}
	if result.FalseAccepts > 0 || result.VerdictParity < *minimumParity || result.RejectRuleIDParity < *minimumParity {
		os.Exit(1)
	}
}

func compare(reportsDirectory, sourceRoot, sourcePrefix string, skipUnsupported bool) (summary, error) {
	paths, err := reportPaths(reportsDirectory)
	if err != nil {
		return summary{}, err
	}
	result := summary{Mismatches: make([]mismatch, 0)}
	for _, reportPath := range paths {
		oracle, err := readReport(reportPath)
		if err != nil {
			return summary{}, err
		}
		sourcePath, err := resolveSource(sourceRoot, sourcePrefix, oracle.DocumentReference)
		if err != nil {
			return summary{}, fmt.Errorf("%s: %w", reportPath, err)
		}
		nativeAccepted, nativeIDs, nativeError, err := validateSource(sourcePath)
		if err != nil {
			return summary{}, err
		}
		if skipUnsupported && nativeError == string(validator.ErrorUnsupportedProfile) {
			result.SkippedUnsupportedProfile++
			continue
		}

		result.Documents++
		if oracle.Accepted == nativeAccepted {
			result.VerdictMatches++
		}
		if !oracle.Accepted && nativeAccepted {
			result.FalseAccepts++
		}
		oracleIDs := oracle.RejectingRuleIDs()
		matches, compared := compareRuleIDs(oracleIDs, nativeIDs)
		result.RuleIDMatches += matches
		result.RuleIDsCompared += compared
		if oracle.Accepted != nativeAccepted || matches != compared {
			result.Mismatches = append(result.Mismatches, mismatch{
				Document:       oracle.DocumentReference,
				OracleAccepted: oracle.Accepted,
				NativeAccepted: nativeAccepted,
				OracleRuleIDs:  oracleIDs,
				NativeRuleIDs:  nativeIDs,
				NativeError:    nativeError,
			})
		}
	}
	result.VerdictParity = ratio(result.VerdictMatches, result.Documents)
	result.RejectRuleIDParity = ratio(result.RuleIDMatches, result.RuleIDsCompared)
	if len(result.Mismatches) == 0 {
		result.Mismatches = nil
	}
	return result, nil
}

func reportPaths(directory string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), "-report.xml") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk reports: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no KoSIT reports found in %s", directory)
	}
	sort.Strings(paths)
	return paths, nil
}

func readReport(path string) (kositreport.Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return kositreport.Report{}, fmt.Errorf("open report %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	report, err := kositreport.Parse(file)
	if err != nil {
		return kositreport.Report{}, fmt.Errorf("parse report %s: %w", path, err)
	}
	return report, nil
}

func resolveSource(root, prefix, reference string) (string, error) {
	relative := reference
	if prefix != "" {
		if !strings.HasPrefix(reference, prefix) {
			return "", fmt.Errorf("document reference %q does not have prefix %q", reference, prefix)
		}
		relative = strings.TrimPrefix(reference, prefix)
	}
	relative = filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe document reference %q", reference)
	}
	path := filepath.Join(root, relative)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("source %s: %w", path, err)
	}
	return path, nil
}

func validateSource(path string) (bool, []string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, nil, "", fmt.Errorf("open source %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	result, validationErr := validator.Validate(context.Background(), file, validator.Options{
		RulePack: validator.RulePackXRechnung302,
	})
	if validationErr != nil {
		var typed *validator.Error
		if errors.As(validationErr, &typed) {
			return false, nil, string(typed.Kind), nil
		}
		return false, nil, "", fmt.Errorf("validate source %s: %w", path, validationErr)
	}
	set := make(map[string]struct{})
	for _, finding := range result.Findings {
		if finding.Severity == validator.SeverityError {
			set[finding.RuleID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return result.Accepted, ids, "", nil
}

func compareRuleIDs(oracle, native []string) (int, int) {
	oracleSet := make(map[string]struct{}, len(oracle))
	nativeSet := make(map[string]struct{}, len(native))
	for _, id := range oracle {
		oracleSet[id] = struct{}{}
	}
	for _, id := range native {
		nativeSet[id] = struct{}{}
	}
	matches := 0
	for id := range oracleSet {
		if _, exists := nativeSet[id]; exists {
			matches++
		}
	}
	compared := len(oracleSet)
	if len(nativeSet) > compared {
		compared = len(nativeSet)
	}
	return matches, compared
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}
