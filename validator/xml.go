package validator

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strings"
)

const (
	ciiNamespace      = "urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
	ublInvoiceNS      = "urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"
	ublCreditNoteNS   = "urn:oasis:names:specification:ubl:schema:xsd:CreditNote-2"
	ublBasicNS        = "urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2"
	ciiReusableNS     = "urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100"
	xincludeNamespace = "http://www.w3.org/2001/XInclude"
	truncationRuleID  = "VALIDATOR-FINDINGS-TRUNCATED"
	truncationMsgCode = "validator.findings_truncated"
	schemaRuleID      = "XSD"
	schemaMsgCode     = "schema.invalid"
)

type contextReader struct {
	ctx context.Context
	src io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.src.Read(buffer)
}

func readBounded(ctx context.Context, src io.Reader, maxBytes int64) ([]byte, error) {
	limited := &io.LimitedReader{R: contextReader{ctx: ctx, src: src}, N: maxBytes + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, failure(ErrorCanceled, "", contextErr)
		}
		return nil, failure(ErrorInternal, "input_read", nil)
	}
	if int64(len(data)) > maxBytes {
		return nil, failure(ErrorResourceLimit, "max_bytes", nil)
	}
	if len(data) == 0 {
		return nil, failure(ErrorMalformedInput, "", nil)
	}
	return data, nil
}

func inspectXML(ctx context.Context, data []byte, maxDepth int) (Syntax, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true

	var syntax Syntax
	depth := 0
	elements := 0
	textBytes := 0
	seenRoot := false
	closedRoot := false
	seenDeclaration := false
	profileElements := 0
	stack := make([]xml.Name, 0, maxDepth)

	for {
		if err := ctx.Err(); err != nil {
			return "", failure(ErrorCanceled, "", err)
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", failure(ErrorMalformedInput, "", nil)
		}

		switch value := token.(type) {
		case xml.StartElement:
			textBytes = 0
			if closedRoot {
				return "", failure(ErrorMalformedInput, "", nil)
			}
			if len(value.Attr) > HardMaxAttributes {
				return "", failure(ErrorResourceLimit, "max_attributes", nil)
			}
			if value.Name.Space == xincludeNamespace {
				return "", failure(ErrorMalformedInput, "", nil)
			}
			if depth == 0 {
				if seenRoot {
					return "", failure(ErrorMalformedInput, "", nil)
				}
				seenRoot = true
				var rootErr error
				syntax, rootErr = rootSyntax(value.Name)
				if rootErr != nil {
					return "", rootErr
				}
			}
			if isProfileElement(syntax, stack, value.Name) {
				profileElements++
				if profileElements > 1 {
					return "", failure(ErrorMalformedInput, "", nil)
				}
			}
			stack = append(stack, value.Name)
			depth++
			elements++
			if depth > maxDepth {
				return "", failure(ErrorResourceLimit, "max_depth", nil)
			}
			if elements > HardMaxElements {
				return "", failure(ErrorResourceLimit, "max_elements", nil)
			}
		case xml.EndElement:
			textBytes = 0
			if len(stack) == 0 {
				return "", failure(ErrorMalformedInput, "", nil)
			}
			stack = stack[:len(stack)-1]
			depth--
			if depth < 0 {
				return "", failure(ErrorMalformedInput, "", nil)
			}
			if depth == 0 {
				closedRoot = true
			}
		case xml.CharData:
			if depth == 0 {
				if strings.TrimSpace(string(value)) != "" {
					return "", failure(ErrorMalformedInput, "", nil)
				}
				continue
			}
			textBytes += len(value)
			if textBytes > HardMaxTextBytes {
				return "", failure(ErrorResourceLimit, "max_text_bytes", nil)
			}
		case xml.Directive:
			return "", failure(ErrorMalformedInput, "", nil)
		case xml.ProcInst:
			// Processing instructions are part of well-formed XML and carry no
			// entity-expansion behavior in encoding/xml. Keep rejecting malformed
			// or duplicate XML declarations, but safely ignore other targets.
			if value.Target == "xml" {
				if seenDeclaration || seenRoot {
					return "", failure(ErrorMalformedInput, "", nil)
				}
				seenDeclaration = true
			}
			textBytes = 0
		default:
			textBytes = 0
		}
	}

	if !seenRoot || !closedRoot || depth != 0 {
		return "", failure(ErrorMalformedInput, "", nil)
	}
	return syntax, nil
}

func hasForbiddenEmptyElement(ctx context.Context, data []byte, syntax Syntax) (bool, error) {
	type state struct {
		name       xml.Name
		hasElement bool
		hasText    bool
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	stack := make([]state, 0, HardMaxDepth)
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if len(stack) > 0 {
				stack[len(stack)-1].hasElement = true
			}
			stack = append(stack, state{name: value.Name})
		case xml.CharData:
			if len(stack) > 0 && strings.TrimSpace(string(value)) != "" {
				stack[len(stack)-1].hasText = true
			}
		case xml.EndElement:
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			excludedCIIContainer := syntax == SyntaxCII && current.name.Space == ciiReusableNS &&
				current.name.Local == "ApplicableHeaderTradeDelivery"
			if !current.hasElement && !current.hasText && !excludedCIIContainer {
				return true, nil
			}
		}
	}
}

func rootSyntax(name xml.Name) (Syntax, error) {
	switch {
	case name.Space == ciiNamespace && name.Local == "CrossIndustryInvoice":
		return SyntaxCII, nil
	case name.Space == ublInvoiceNS && name.Local == "Invoice":
		return SyntaxUBL, nil
	case name.Space == ublCreditNoteNS && name.Local == "CreditNote":
		return SyntaxUBL, nil
	default:
		return "", failure(ErrorUnsupportedSyntax, "", nil)
	}
}

func isProfileElement(syntax Syntax, parents []xml.Name, name xml.Name) bool {
	switch syntax {
	case SyntaxUBL:
		return len(parents) == 1 && name.Space == ublBasicNS && name.Local == "CustomizationID"
	case SyntaxCII:
		return len(parents) == 3 &&
			parents[1].Space == ciiNamespace && parents[1].Local == "ExchangedDocumentContext" &&
			parents[2].Space == ciiReusableNS &&
			parents[2].Local == "GuidelineSpecifiedDocumentContextParameter" &&
			name.Space == ciiReusableNS && name.Local == "ID"
	default:
		return false
	}
}
