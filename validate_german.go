package einvoice

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/jxsl13/einvoice/rules"
)

const (
	specXRechnungExtension30 = SpecXRechnung30 + "#conformant#urn:xeinkauf.de:kosit:extension:xrechnung_3.0"
	specXRechnungCVD09       = SpecXRechnung30 + "#compliant#urn:xeinkauf.de:kosit:xrechnung:cvd_0.9"
)

// validateGerman performs German XRechnung-specific business rule validation.
//
// This method validates invoices against BR-DE-* rules defined in the
// XRechnung specification (CIUS XRechnung for Germany).
//
// XRechnung is the German implementation of EN 16931, required for invoices to
// German public authorities and increasingly used in B2B scenarios.
//
// This validation applies when the specification identifier (BT-24) matches
// an XRechnung URN (detected via IsXRechnung()).
//
// BR-DE Rules Implemented (Errors - "muss"/"must"):
//   - BR-DE-1:  Payment instructions (BG-16) must be provided
//   - BR-DE-2:  Seller contact (BG-6) must be provided
//   - BR-DE-3:  Seller city (BT-37) must be provided
//   - BR-DE-4:  Seller post code (BT-38) must be provided
//   - BR-DE-5:  Seller contact point (BT-41) must be provided
//   - BR-DE-6:  Seller contact telephone (BT-42) must be provided
//   - BR-DE-7:  Seller contact email (BT-43) must be provided
//   - BR-DE-8:  Buyer city (BT-52) must be provided
//   - BR-DE-9:  Buyer post code (BT-53) must be provided
//   - BR-DE-10: Deliver to city (BT-77) must be provided if delivery address exists
//   - BR-DE-11: Deliver to post code (BT-78) must be provided if delivery address exists
//   - BR-DE-15: Buyer reference (BT-10) must be provided (Leitweg-ID)
//   - BR-DE-16: Seller identification required when using certain tax codes
//   - BR-DE-23: Payment means requirements (codes 30, 58, 59)
//   - BR-DE-24: Payment card information requirements (codes 48, 54)
//   - BR-DE-25: Direct debit mandate requirements (code 59)
//   - BR-DE-30: Bank assigned creditor identifier (BT-90) for direct debit
//   - BR-DE-31: Debited account identifier (BT-91) for direct debit
//
// BR-DE Rules Implemented (Warnings - "soll"/"should"):
//   - BR-DE-19: IBAN validation for SEPA credit transfer (code 58)
//   - BR-DE-20: IBAN validation for SEPA direct debit (code 59)
//   - BR-DE-26: Corrected invoice should reference preceding invoice
//   - BR-DE-27: Seller contact telephone should contain at least 3 digits
//   - BR-DE-28: Email address format validation
//
// Note: BR-DE-21 (specification identifier) is implicitly satisfied since this
// method only runs for invoices identified as XRechnung via IsXRechnung().
//
// Reference: https://github.com/itplr-kosit/xrechnung-schematron
func (inv *Invoice) validateGerman() {
	if inv.GuidelineSpecifiedDocumentContextParameter != SpecXRechnung30 &&
		inv.GuidelineSpecifiedDocumentContextParameter != specXRechnungExtension30 &&
		inv.GuidelineSpecifiedDocumentContextParameter != specXRechnungCVD09 {
		inv.addWarning(rules.BRDE21, "Specification identifier is not a current XRechnung 3.0 profile")
	}

	// BR-DE-1: Payment instructions (BG-16) must be provided
	if len(inv.PaymentMeans) == 0 {
		inv.addViolation(rules.BRDE1, "An invoice must contain information on PAYMENT INSTRUCTIONS (BG-16)")
	}

	// BR-DE-2: Seller contact (BG-6) must be provided
	if len(inv.Seller.DefinedTradeContact) == 0 {
		inv.addViolation(rules.BRDE2, "The element group SELLER CONTACT (BG-6) must be transmitted")
	}

	// BR-DE-3: Seller city (BT-37) must be provided
	if inv.Seller.PostalAddress == nil || inv.Seller.PostalAddress.City == "" {
		inv.addViolation(rules.BRDE3, "The element 'Seller city' (BT-37) must be transmitted")
	}

	// BR-DE-4: Seller post code (BT-38) must be provided
	if inv.Seller.PostalAddress == nil || inv.Seller.PostalAddress.PostcodeCode == "" {
		inv.addViolation(rules.BRDE4, "The element 'Seller post code' (BT-38) must be transmitted")
	}

	// BR-DE-5, BR-DE-6, BR-DE-7: Seller contact details
	if len(inv.Seller.DefinedTradeContact) > 0 {
		contact := inv.Seller.DefinedTradeContact[0]

		// BR-DE-5: Seller contact point (BT-41)
		if contact.PersonName == "" && contact.DepartmentName == "" {
			inv.addViolation(rules.BRDE5, "The element 'Seller contact point' (BT-41) must be transmitted")
		}

		// BR-DE-6: Seller contact telephone number (BT-42)
		if contact.PhoneNumber == "" {
			inv.addViolation(rules.BRDE6, "The element 'Seller contact telephone number' (BT-42) must be transmitted")
		} else {
			// BR-DE-27: Telephone should contain at least 3 digits (warning per XRechnung schematron)
			digitCount := countDigits(contact.PhoneNumber)
			if digitCount < 3 {
				inv.addWarning(rules.BRDE27, "Seller contact telephone number (BT-42) should contain at least three digits")
			}
		}

		// BR-DE-7: Seller contact email address (BT-43)
		if contact.EMail == "" {
			inv.addViolation(rules.BRDE7, "The element 'Seller contact email address' (BT-43) must be transmitted")
		} else if !isValidEmail(contact.EMail) {
			// BR-DE-28: Email format validation (warning per XRechnung schematron)
			inv.addWarning(rules.BRDE28, "Email address should have valid format (one @, no leading/trailing dots, etc.)")
		}
	}

	// BR-DE-8: Buyer city (BT-52) must be provided
	if inv.Buyer.PostalAddress == nil || inv.Buyer.PostalAddress.City == "" {
		inv.addViolation(rules.BRDE8, "The element 'Buyer city' (BT-52) must be transmitted")
	}

	// BR-DE-9: Buyer post code (BT-53) must be provided
	if inv.Buyer.PostalAddress == nil || inv.Buyer.PostalAddress.PostcodeCode == "" {
		inv.addViolation(rules.BRDE9, "The element 'Buyer post code' (BT-53) must be transmitted")
	}

	// BR-DE-10, BR-DE-11: Deliver to address (if provided)
	if inv.ShipTo != nil && inv.ShipTo.PostalAddress != nil {
		// BR-DE-10: Deliver to city (BT-77)
		if inv.ShipTo.PostalAddress.City == "" {
			inv.addViolation(rules.BRDE10, "The element 'Deliver to city' (BT-77) must be transmitted if delivery address is provided")
		}

		// BR-DE-11: Deliver to post code (BT-78)
		if inv.ShipTo.PostalAddress.PostcodeCode == "" {
			inv.addViolation(rules.BRDE11, "The element 'Deliver to post code' (BT-78) must be transmitted if delivery address is provided")
		}
	}

	// BR-DE-15: Buyer reference (BT-10) must be provided (Leitweg-ID)
	if inv.BuyerReference == "" {
		inv.addViolation(rules.BRDE15, "The element 'Buyer reference' (BT-10) must be transmitted")
	}

	for i := range inv.TradeTaxes {
		if inv.isParsed && !inv.TradeTaxes[i].hasPercentInXML {
			inv.addViolation(rules.BRDE14, "VAT category rate (BT-119) must be present")
		}
	}

	if !strings.Contains(" 326 380 384 389 381 875 876 877 ", " "+inv.InvoiceTypeCode.String()+" ") {
		inv.addWarning(rules.BRDE17, "Invoice type code is outside the XRechnung recommended set")
	}

	for _, terms := range inv.SpecifiedTradePaymentTerms {
		if !validSkontoDescription(terms.Description) {
			inv.addViolation(rules.BRDE18, "Discount payment-terms line does not match the XRechnung syntax")
			break
		}
	}

	filenames := make(map[string]struct{})
	for _, document := range inv.AdditionalReferencedDocument {
		if document.AttachmentFilename != "" {
			if _, duplicate := filenames[document.AttachmentFilename]; duplicate {
				inv.addViolation(rules.BRDE22, "Embedded attachment filenames must be unique")
				break
			}
			filenames[document.AttachmentFilename] = struct{}{}
		}
		if document.URIID != "" && !validXRechnungURL(document.URIID) {
			inv.addWarning(rules.BRTMP2, "External document location must be an absolute URI")
		}
	}

	if inv.SchemaType == CII {
		for i := range inv.InvoiceLines {
			line := &inv.InvoiceLines[i]
			if line.hasNetBasisQuantityInXML && line.hasGrossBasisQuantityInXML &&
				(!line.BasisQuantity.Equal(line.grossBasisQuantity) ||
					(line.BasisQuantityUnit != "" && line.grossBasisQuantityUnit != "" && line.BasisQuantityUnit != line.grossBasisQuantityUnit)) {
				inv.addViolation(rules.BRTMP3, "Gross and net price base quantities must agree")
			}
		}
	}

	hasDeliveryOrPeriod := !inv.OccurrenceDateTime.IsZero() || inv.hasBillingPeriodInXML
	if !hasDeliveryOrPeriod {
		hasDeliveryOrPeriod = true
		for i := range inv.InvoiceLines {
			if !inv.InvoiceLines[i].linePeriodPresent {
				hasDeliveryOrPeriod = false
				break
			}
		}
	}
	if !hasDeliveryOrPeriod {
		inv.addInfo(rules.BRDETMP32, "Delivery date or an invoice/line period is recommended")
	}

	// BR-DE-16: When tax codes S, Z, E, AE, K, G, L or M are used, at least one of
	// Seller VAT identifier (BT-31), Seller tax registration identifier (BT-32)
	// or SELLER TAX REPRESENTATIVE PARTY (BG-11) must be provided
	relevantTaxCodes := map[string]bool{
		"S": true, "Z": true, "E": true, "AE": true,
		"K": true, "G": true, "L": true, "M": true,
	}

	hasRelevantTaxCode := false
	for i := range inv.InvoiceLines {
		inv.checkContext()
		if relevantTaxCodes[inv.InvoiceLines[i].TaxCategoryCode] {
			hasRelevantTaxCode = true
			break
		}
	}
	if !hasRelevantTaxCode {
		for i := range inv.SpecifiedTradeAllowanceCharge {
			inv.checkContext()
			if relevantTaxCodes[inv.SpecifiedTradeAllowanceCharge[i].CategoryTradeTaxCategoryCode] {
				hasRelevantTaxCode = true
				break
			}
		}
	}

	if hasRelevantTaxCode {
		hasSellerVATID := inv.Seller.VATaxRegistration != ""
		hasSellerTaxReg := inv.Seller.FCTaxRegistration != ""
		hasTaxRep := inv.SellerTaxRepresentativeTradeParty != nil

		if !hasSellerVATID && !hasSellerTaxReg && !hasTaxRep {
			inv.addViolation(rules.BRDE16, "When tax codes S, Z, E, AE, K, G, L or M are used, at least one of Seller VAT identifier (BT-31), Seller tax registration identifier (BT-32) or SELLER TAX REPRESENTATIVE PARTY (BG-11) must be provided")
		}
	}

	// Note: VAT identifier format validation (ISO 3166-1 alpha-2 prefix) is handled
	// by BR-CO-09 in validate_core.go, not here.

	// Note: BR-DE-21 validates that BT-24 matches the XRechnung specification identifier.
	// Since this method only runs for XRechnung invoices (determined by IsXRechnung()),
	// and IsXRechnung() already validates the URN format, BR-DE-21 is implicitly satisfied.

	// BR-DE-23, BR-DE-24, BR-DE-25: Payment means requirements
	// These rules ensure mutual exclusivity of payment means groups (BG-17, BG-18, BG-19)
	for i := range inv.PaymentMeans {
		inv.checkContext()
		// Determine which payment information groups are present
		hasBG17CreditTransfer := inv.PaymentMeans[i].hasPayeeAccountInXML || inv.PaymentMeans[i].PayeePartyCreditorFinancialAccountIBAN != "" ||
			inv.PaymentMeans[i].PayeePartyCreditorFinancialAccountProprietaryID != "" ||
			inv.PaymentMeans[i].hasPayeeInstitutionInXML || inv.PaymentMeans[i].hasPayerInstitutionInXML
		hasBG18PaymentCard := inv.PaymentMeans[i].hasPaymentCardInXML || inv.PaymentMeans[i].ApplicableTradeSettlementFinancialCardID != ""
		hasBG19DirectDebit := inv.PaymentMeans[i].hasPaymentMandateInXML || inv.PaymentMeans[i].hasPayerAccountIDInXML || inv.PaymentMeans[i].PayerPartyDebtorFinancialAccountIBAN != ""
		if inv.SchemaType == CII {
			hasBG19DirectDebit = hasBG19DirectDebit || inv.CreditorReferenceID != "" || hasDirectDebitMandate(inv.SpecifiedTradePaymentTerms)
		}

		// BR-DE-23: Credit transfer (codes 30, 58)
		if inv.PaymentMeans[i].TypeCode == 30 || inv.PaymentMeans[i].TypeCode == 58 {
			// BR-DE-23-a: Must have BG-17 (CREDIT TRANSFER)
			if !hasBG17CreditTransfer {
				inv.addViolation(rules.BRDE23A, "Payment means code 30 or 58 (credit transfer) requires BG-17 CREDIT TRANSFER information")
			}

			// BR-DE-23-b: Must NOT have BG-18 (payment card) or BG-19 (direct debit)
			if hasBG18PaymentCard {
				inv.addViolation(rules.BRDE23B, "Payment means code 30 or 58 (credit transfer) must not contain BG-18 PAYMENT CARD INFORMATION")
			}
			if hasBG19DirectDebit {
				inv.addViolation(rules.BRDE23B, "Payment means code 30 or 58 (credit transfer) must not contain BG-19 DIRECT DEBIT")
			}
		}

		// BR-DE-24: Payment card (codes 48, 54, 55)
		if inv.PaymentMeans[i].TypeCode == 48 || inv.PaymentMeans[i].TypeCode == 54 || inv.PaymentMeans[i].TypeCode == 55 {
			// BR-DE-24-a: Must have BG-18 (PAYMENT CARD INFORMATION)
			if !hasBG18PaymentCard {
				inv.addViolation(rules.BRDE24A, "Payment means code 48, 54, or 55 (payment card) requires BG-18 PAYMENT CARD INFORMATION")
			}

			// BR-DE-24-b: Must NOT have BG-17 (credit transfer) or BG-19 (direct debit)
			if hasBG17CreditTransfer {
				inv.addViolation(rules.BRDE24B, "Payment means code 48, 54, or 55 (payment card) must not contain BG-17 CREDIT TRANSFER")
			}
			if hasBG19DirectDebit {
				inv.addViolation(rules.BRDE24B, "Payment means code 48, 54, or 55 (payment card) must not contain BG-19 DIRECT DEBIT")
			}
		}

		// BR-DE-25: Direct debit (code 59)
		if inv.PaymentMeans[i].TypeCode == 59 {
			// BR-DE-25-a: Must have BG-19 (DIRECT DEBIT)
			if !hasBG19DirectDebit {
				inv.addViolation(rules.BRDE25A, "Payment means code 59 (direct debit) requires BG-19 DIRECT DEBIT information")
			}

			// BR-DE-25-b: Must NOT have BG-17 (credit transfer) or BG-18 (payment card)
			if hasBG17CreditTransfer {
				inv.addViolation(rules.BRDE25B, "Payment means code 59 (direct debit) must not contain BG-17 CREDIT TRANSFER")
			}
			if hasBG18PaymentCard {
				inv.addViolation(rules.BRDE25B, "Payment means code 59 (direct debit) must not contain BG-18 PAYMENT CARD INFORMATION")
			}
		}

		// BR-DE-19: IBAN validation for SEPA credit transfer (warning per XRechnung schematron)
		if inv.PaymentMeans[i].TypeCode == 58 {
			if inv.PaymentMeans[i].PayeePartyCreditorFinancialAccountIBAN != "" && !isValidIBAN(inv.PaymentMeans[i].PayeePartyCreditorFinancialAccountIBAN) {
				inv.addWarning(rules.BRDE19, "Payment account identifier (BT-84) should be a valid IBAN when using SEPA credit transfer (code 58)")
			}
		}

		// BR-DE-20: IBAN validation for SEPA direct debit (warning per XRechnung schematron)
		if inv.PaymentMeans[i].TypeCode == 59 {
			if inv.PaymentMeans[i].PayerPartyDebtorFinancialAccountIBAN != "" && !isValidIBAN(inv.PaymentMeans[i].PayerPartyDebtorFinancialAccountIBAN) {
				inv.addWarning(rules.BRDE20, "Debited account identifier (BT-91) should be a valid IBAN when using SEPA direct debit (code 59)")
			}
		}

	}

	inv.validateGermanDirectDebitIdentifiers()

	// BR-DE-26: Corrected invoice should reference preceding invoice (warning per XRechnung schematron)
	if int(inv.InvoiceTypeCode) == 384 {
		if len(inv.InvoiceReferencedDocument) == 0 {
			inv.addWarning(rules.BRDE26, "If invoice type code (BT-3) is 384 (Corrected invoice), PRECEDING INVOICE REFERENCE (BG-3) should be provided")
		}
	}
}

var skontoLinePattern = regexp.MustCompile(`^#SKONTO#TAGE=[0-9]+#PROZENT=[0-9]+\.[0-9]{2}(#BASISBETRAG=-?[0-9]+\.[0-9]{2})?#$`)

func validSkontoDescription(description string) bool {
	lines := strings.Split(strings.ReplaceAll(description, "\r\n", "\n"), "\n")
	found := false
	lastSkontoLine := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		found = true
		lastSkontoLine = index
		if !skontoLinePattern.MatchString(trimmed) {
			return false
		}
	}
	if !found {
		return true
	}
	return lastSkontoLine < len(lines)-1
}

func validXRechnungURL(value string) bool {
	colon := strings.IndexByte(value, ':')
	if colon < 2 || (value[0] < 'A' || value[0] > 'Z') && (value[0] < 'a' || value[0] > 'z') {
		return false
	}
	for i := 1; i < colon; i++ {
		character := value[i]
		if !isAlphanumericAnyCase(character) && character != '+' && character != '.' && character != '-' {
			return false
		}
	}
	return true
}

func hasDirectDebitMandate(terms []SpecifiedTradePaymentTerms) bool {
	for _, term := range terms {
		if term.DirectDebitMandateID != "" {
			return true
		}
	}
	return false
}

func (inv *Invoice) validateGermanDirectDebitIdentifiers() {
	hasBT89 := hasDirectDebitMandate(inv.SpecifiedTradePaymentTerms)
	hasBT90 := inv.CreditorReferenceID != ""
	if inv.SchemaType == UBL {
		hasBT89 = false
		hasBT90 = hasPartyIdentifierScheme(inv.Seller, "SEPA") ||
			(inv.PayeeTradeParty != nil && hasPartyIdentifierScheme(*inv.PayeeTradeParty, "SEPA"))
	}
	hasBT91 := false
	hasBG19 := false
	for i := range inv.PaymentMeans {
		paymentMeans := &inv.PaymentMeans[i]
		if inv.SchemaType == UBL {
			hasBG19 = hasBG19 || paymentMeans.hasPaymentMandateInXML
			hasBT89 = hasBT89 || paymentMeans.mandateIDXML != ""
		} else {
			hasBG19 = hasBG19 || paymentMeans.hasPayerAccountIDInXML || paymentMeans.PayerPartyDebtorFinancialAccountIBAN != ""
		}
		hasBT91 = hasBT91 || paymentMeans.hasPayerAccountIDInXML || paymentMeans.PayerPartyDebtorFinancialAccountIBAN != ""
	}
	if inv.SchemaType == CII {
		hasBG19 = hasBG19 || hasBT89 || hasBT90
	}
	if !hasBG19 {
		return
	}
	if !hasBT90 || !hasBT89 && !hasBT91 {
		inv.addViolation(rules.BRDE30, "Direct debit identifiers do not satisfy BT-90 dependencies")
	}
	if !hasBT91 || !hasBT89 && !hasBT90 {
		inv.addViolation(rules.BRDE31, "Direct debit identifiers do not satisfy BT-91 dependencies")
	}
}

// countDigits counts the number of digit characters in a string.
func countDigits(s string) int {
	count := 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			count++
		}
	}
	return count
}

// isValidEmail validates email format according to BR-DE-28.
// Requirements:
// - Exactly one @ sign
// - Does not start or end with a dot
// - @ sign must not be flanked by whitespace or dot
// - Must be preceded and followed by at least two characters
func isValidEmail(email string) bool {
	// Must have exactly one @
	atCount := strings.Count(email, "@")
	if atCount != 1 {
		return false
	}

	// Must not start or end with dot
	if strings.HasPrefix(email, ".") || strings.HasSuffix(email, ".") {
		return false
	}

	// Split on @
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}

	local := parts[0]
	domain := parts[1]

	// Local and domain parts must have at least 2 characters each
	if len(local) < 2 || len(domain) < 2 {
		return false
	}

	// @ must not be flanked by whitespace or dot
	if strings.HasSuffix(local, " ") || strings.HasPrefix(domain, " ") {
		return false
	}
	if strings.HasSuffix(local, ".") || strings.HasPrefix(domain, ".") {
		return false
	}

	return true
}

// isUppercaseLetter checks if a byte represents an uppercase ASCII letter (A-Z).
func isUppercaseLetter(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

// isValidIBAN performs basic IBAN validation.
// A valid IBAN:
// - Has 15-34 alphanumeric characters
// - Starts with a 2-letter country code
// - Followed by 2 check digits
// - Followed by the Basic Bank Account Number (BBAN)
//
// This is a simplified validation that checks format. Full validation
// would include modulo-97 checksum verification per ISO 13616.
func isValidIBAN(iban string) bool {
	// Mirror the pinned XRechnung XPath: remove whitespace, validate the
	// structural expression, move the first four characters to the end, then
	// evaluate the resulting decimal stream modulo 97 without big integers.
	iban = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, iban)

	if len(iban) < 4 || len(iban) > 34 {
		return false
	}

	// First two characters must be letters (country code)
	if !isUppercaseLetter(iban[0]) || !isUppercaseLetter(iban[1]) {
		return false
	}

	// Next two characters must be digits (check digits)
	if !isDigit(iban[2]) || !isDigit(iban[3]) {
		return false
	}

	// Remaining characters must be ASCII alphanumeric. The official XPath
	// permits either case in the BBAN portion.
	for i := 4; i < len(iban); i++ {
		if !isAlphanumericAnyCase(iban[i]) {
			return false
		}
	}

	rearranged := iban[4:] + strings.ToUpper(iban[:2]) + iban[2:4]
	remainder := 0
	for i := 0; i < len(rearranged); i++ {
		character := rearranged[i]
		switch {
		case isDigit(character):
			remainder = (remainder*10 + int(character-'0')) % 97
		case character >= 'A' && character <= 'Z':
			value := int(character-'A') + 10
			remainder = (remainder*100 + value) % 97
		case character >= 'a' && character <= 'z':
			// XPath maps a codepoint greater than 64 to codepoint-55.
			value := int(character) - 55
			remainder = (remainder*100 + value) % 97
		default:
			return false
		}
	}
	return remainder == 1
}

// isDigit checks if a byte represents a digit (0-9).
func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// isAlphanumeric checks if a byte represents an alphanumeric character (0-9, A-Z).
func isAlphanumeric(b byte) bool {
	return isDigit(b) || isUppercaseLetter(b)
}

func isAlphanumericAnyCase(b byte) bool {
	return isAlphanumeric(b) || (b >= 'a' && b <= 'z')
}

func hasPartyIdentifierScheme(party Party, scheme string) bool {
	for _, identifier := range party.GlobalID {
		if identifier.Scheme == scheme && strings.TrimSpace(identifier.ID) != "" {
			return true
		}
	}
	return false
}
