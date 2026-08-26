// Package kositreport parses the stable, machine-readable subset of KoSIT
// validator assessment reports used by the conformance parity gate.
package kositreport

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Report is the normalized oracle result for one XML document.
type Report struct {
	DocumentReference string
	Accepted          bool
	Messages          []Message
}

// Message is one normalized oracle finding.
type Message struct {
	Code  string
	Level string
}

// Parse extracts only the assessment and findings from a KoSIT VARL report.
func Parse(reader io.Reader) (Report, error) {
	if reader == nil {
		return Report{}, fmt.Errorf("kosit report: nil reader")
	}

	decoder := xml.NewDecoder(reader)
	var result Report
	var seenAssessment, seenDecision bool
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Report{}, fmt.Errorf("kosit report: decode: %w", err)
		}

		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "documentReference":
				var reference string
				if err := decoder.DecodeElement(&reference, &value); err != nil {
					return Report{}, fmt.Errorf("kosit report: document reference: %w", err)
				}
				if result.DocumentReference == "" {
					result.DocumentReference = strings.TrimSpace(reference)
				}
			case "assessment":
				seenAssessment = true
			case "accept", "reject":
				if seenAssessment && !seenDecision {
					result.Accepted = value.Name.Local == "accept"
					seenDecision = true
				}
			case "message":
				message := Message{}
				for _, attribute := range value.Attr {
					switch attribute.Name.Local {
					case "code":
						message.Code = strings.TrimSpace(attribute.Value)
					case "level":
						message.Level = strings.ToLower(strings.TrimSpace(attribute.Value))
					}
				}
				var body string
				if err := decoder.DecodeElement(&body, &value); err != nil {
					return Report{}, fmt.Errorf("kosit report: message: %w", err)
				}
				if message.Code == "" {
					message.Code = bracketedRuleID(body)
				}
				if message.Code != "" || message.Level != "" {
					result.Messages = append(result.Messages, message)
				}
			}
		}
	}

	if result.DocumentReference == "" {
		return Report{}, fmt.Errorf("kosit report: missing document reference")
	}
	if !seenAssessment || !seenDecision {
		return Report{}, fmt.Errorf("kosit report: missing assessment decision")
	}
	sort.Slice(result.Messages, func(left, right int) bool {
		if result.Messages[left].Level != result.Messages[right].Level {
			return result.Messages[left].Level < result.Messages[right].Level
		}
		return result.Messages[left].Code < result.Messages[right].Code
	})
	return result, nil
}

// RejectingRuleIDs returns the unique, sorted error/fatal oracle rule IDs.
func (report Report) RejectingRuleIDs() []string {
	set := make(map[string]struct{})
	for _, message := range report.Messages {
		if message.Code != "" && (message.Level == "error" || message.Level == "fatal") {
			code := message.Code
			if strings.HasPrefix(code, "cvc-") {
				code = "XSD"
			}
			set[code] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for code := range set {
		result = append(result, code)
	}
	sort.Strings(result)
	return result
}

func bracketedRuleID(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") {
		return ""
	}
	end := strings.IndexByte(value, ']')
	if end < 2 {
		return ""
	}
	return strings.TrimSpace(value[1:end])
}
