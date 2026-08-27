package einvoice

import (
	"fmt"
	"strconv"

	"github.com/jxsl13/einvoice/rules"
	"github.com/shopspring/decimal"
)

// validatePEPPOL validates the invoice against PEPPOL BIS Billing 3.0 rules.
//
// PEPPOL (Pan-European Public Procurement On-Line) BIS Billing 3.0 extends
// EN 16931 with additional validation rules required for the PEPPOL network.
//
// This method checks business logic rules that can be validated on the
// Invoice struct. Some PEPPOL rules that require XML structure validation
// (e.g., PEPPOL-EN16931-R008 for empty elements) are checked during parsing.
//
// Common PEPPOL rules implemented:
//   - PEPPOL-EN16931-R001: Business process must be provided (BT-23)
//   - PEPPOL-EN16931-R002: No more than one note on document level
//   - PEPPOL-EN16931-R003: Buyer reference or purchase order reference required (BT-10/BT-13)
//   - PEPPOL-EN16931-R010: Buyer electronic address required (BT-49)
//   - PEPPOL-EN16931-R020: Seller electronic address required (BT-34)
//   - PEPPOL-EN16931-R120: Invoice line net amount calculation validation
//   - PEPPOL-EN16931-R121: Base quantity must be positive above zero
//   - PEPPOL-EN16931-R130: Unit code of price base quantity must match invoiced quantity
//
// Note: Full PEPPOL validation also requires checking the XML structure and
// additional business rules. This is a basic implementation covering the most
// common PEPPOL requirements. Country-specific rules (DK, IT, NL, NO, SE) and
// advanced validations are not yet implemented.
//
// TODO: Implement additional PEPPOL rules:
//   - PEPPOL-EN16931-R005: VAT accounting currency code validation
//   - PEPPOL-EN16931-R006: Only one invoiced object on document level
//   - PEPPOL-EN16931-R110: Start date of line period within invoice period
//   - PEPPOL-EN16931-R111: End date of line period within invoice period
//   - Country-specific rules (DK-R-*, IT-R-*, NL-R-*, NO-R-*, SE-R-*)
//   - Code list validations (PEPPOL-EN16931-CL*)
//   - Format validations (PEPPOL-EN16931-F*)
//   - Profile-specific rules (PEPPOL-EN16931-P*)
//   - Common identifier format rules (PEPPOL-COMMON-R*)
func (inv *Invoice) validatePEPPOL() {
	if inv.isParsed && inv.peppolEmptyElementCount > 0 {
		inv.addViolation(rules.PEPPOLEN16931R8, "Document contains an empty XML element")
	}

	// PEPPOL-EN16931-R001: Business process MUST be provided (BT-23)
	if inv.BPSpecifiedDocumentContextParameter == "" {
		inv.addViolation(rules.PEPPOLEN16931R1, "Business process MUST be provided")
	}

	if inv.isPEPPOL() {
		// These BIS-only predicates are not active in the XRechnung 3.0.2
		// Schematron phase even though both profiles reuse other PEPPOL rules.
		if inv.BPSpecifiedDocumentContextParameter != "" {
			if err := ValidateBusinessProcessID(inv.BPSpecifiedDocumentContextParameter); err != nil {
				inv.addViolation(rules.PEPPOLEN16931R7, err.Error())
			}
		}

		// PEPPOL-EN16931-R002: No more than one note is allowed on document level
		if len(inv.Notes) > 1 {
			inv.addViolation(rules.PEPPOLEN16931R2, "No more than one note is allowed on document level")
		}

		// PEPPOL-EN16931-R003: A buyer reference or purchase order reference MUST be provided
		if inv.BuyerReference == "" && inv.BuyerOrderReferencedDocument == "" {
			inv.addViolation(rules.PEPPOLEN16931R3, "A buyer reference or purchase order reference MUST be provided")
		}
	}

	// PEPPOL-EN16931-R010: Buyer electronic address MUST be provided (BT-49)
	if inv.Buyer.URIUniversalCommunication == "" {
		inv.addViolation(rules.PEPPOLEN16931R10, "Buyer electronic address MUST be provided")
	}

	// PEPPOL-EN16931-R020: Seller electronic address MUST be provided (BT-34)
	if inv.Seller.URIUniversalCommunication == "" {
		inv.addViolation(rules.PEPPOLEN16931R20, "Seller electronic address MUST be provided")
	}

	inv.validatePEPPOLAllowanceCharges()
	inv.validatePEPPOLPaymentAndPeriods()

	// Validate invoice line calculations (R120, R121, R130)
	inv.validatePEPPOLLineCalculations()
}

// validatePEPPOLLineCalculations validates line-level calculation rules using PEPPOL rule codes.
func (inv *Invoice) validatePEPPOLLineCalculations() {
	inv.validateLineCalculations(
		rules.PEPPOLEN16931R120,
		rules.PEPPOLEN16931R121,
		rules.PEPPOLEN16931R130,
		inv.addWarning,
		inv.addViolation,
		inv.addViolation,
	)
}

// validateUserLineCalculations applies the same line calculation checks for non-PEPPOL invoices
// but reports violations under the custom rule BR-USER-05.
func (inv *Invoice) validateUserLineCalculations() {
	inv.validateLineCalculations(
		rules.BRUSER05,
		rules.BRUSER05,
		rules.BRUSER05,
		inv.addWarning,
		inv.addWarning,
		inv.addWarning,
	)
}

// validateLineCalculations validates line-level calculation rules.
//
// These checks ensure that line-level calculations are mathematically correct,
// catching errors before they cascade to document-level totals.
// The provided rule codes control how violations are reported for:
//   - calcRule: invoice line net amount calculation
//   - baseQtyRule: base quantity positivity
//   - unitRule: unit of measure alignment
func (inv *Invoice) validateLineCalculations(
	calcRule rules.Rule,
	baseQtyRule rules.Rule,
	unitRule rules.Rule,
	reportCalculation func(rule rules.Rule, text string),
	reportBaseQuantity func(rule rules.Rule, text string),
	reportUnit func(rule rules.Rule, text string),
) {
	for i := range inv.InvoiceLines {
		inv.checkContext()
		// Create line reference for error messages
		lineRef := inv.InvoiceLines[i].LineID
		if lineRef == "" {
			lineRef = strconv.Itoa(i + 1)
		}

		// PEPPOL-EN16931-R121: Base quantity MUST be a positive number above zero
		// Only validate if BasisQuantity was explicitly set (non-zero in parsed XML)
		// When element is missing, parser returns zero and we default to 1 for calculation
		if !inv.InvoiceLines[i].BasisQuantity.IsZero() && !inv.InvoiceLines[i].BasisQuantity.GreaterThan(decimal.Zero) {
			reportBaseQuantity(baseQtyRule,
				fmt.Sprintf("Line %s: Base quantity MUST be a positive number above zero (got %s)",
					lineRef, inv.InvoiceLines[i].BasisQuantity))
		}

		// PEPPOL-EN16931-R130: Unit code of price base quantity MUST be same as invoiced quantity
		// Only validate if BasisQuantityUnit is specified (element present in XML)
		if inv.InvoiceLines[i].BasisQuantityUnit != "" && inv.InvoiceLines[i].BasisQuantityUnit != inv.InvoiceLines[i].BilledQuantityUnit {
			reportUnit(unitRule,
				fmt.Sprintf("Line %s: Unit code of price base quantity (%s) MUST be same as invoiced quantity (%s)",
					lineRef, inv.InvoiceLines[i].BasisQuantityUnit, inv.InvoiceLines[i].BilledQuantityUnit))
		}

		// PEPPOL-EN16931-R120: Invoice line net amount calculation
		// Formula: (quantity × price / baseQty) + charges - allowances
		baseQty := inv.InvoiceLines[i].BasisQuantity
		if baseQty.IsZero() {
			// Default to 1 when not specified (per EN 16931)
			baseQty = decimal.NewFromInt(1)
		}

		// Calculate: BilledQuantity × NetPrice / BasisQuantity
		calculated := inv.InvoiceLines[i].BilledQuantity.Mul(inv.InvoiceLines[i].NetPrice).Div(baseQty)

		// Add line-level charges (BG-28)
		chargeTotal := decimal.Zero
		for j := range inv.InvoiceLines[i].InvoiceLineCharges {
			inv.checkContext()
			calculated = calculated.Add(inv.InvoiceLines[i].InvoiceLineCharges[j].ActualAmount)
			chargeTotal = chargeTotal.Add(inv.InvoiceLines[i].InvoiceLineCharges[j].ActualAmount)
		}

		// Subtract line-level allowances (BG-27)
		allowanceTotal := decimal.Zero
		for j := range inv.InvoiceLines[i].InvoiceLineAllowances {
			inv.checkContext()
			calculated = calculated.Sub(inv.InvoiceLines[i].InvoiceLineAllowances[j].ActualAmount)
			allowanceTotal = allowanceTotal.Add(inv.InvoiceLines[i].InvoiceLineAllowances[j].ActualAmount)
		}

		// Round to 2 decimal places (per PEPPOL schematron)
		expected := roundHalfUp(calculated, 2)

		if !inv.InvoiceLines[i].Total.Equal(expected) {
			reportCalculation(calcRule,
				fmt.Sprintf("Line %s: Invoice line net amount %s does not match calculated %s "+
					"(qty %s × price %s / baseQty %s + charges %s - allowances %s)",
					lineRef, inv.InvoiceLines[i].Total, expected,
					inv.InvoiceLines[i].BilledQuantity, inv.InvoiceLines[i].NetPrice, baseQty,
					chargeTotal, allowanceTotal))
		}
	}
}

func (inv *Invoice) validatePEPPOLAllowanceCharges() {
	documentAndLine := make([]*AllowanceCharge, 0, len(inv.SpecifiedTradeAllowanceCharge))
	for i := range inv.SpecifiedTradeAllowanceCharge {
		documentAndLine = append(documentAndLine, &inv.SpecifiedTradeAllowanceCharge[i])
	}
	for i := range inv.InvoiceLines {
		for j := range inv.InvoiceLines[i].InvoiceLineAllowances {
			documentAndLine = append(documentAndLine, &inv.InvoiceLines[i].InvoiceLineAllowances[j])
		}
		for j := range inv.InvoiceLines[i].InvoiceLineCharges {
			documentAndLine = append(documentAndLine, &inv.InvoiceLines[i].InvoiceLineCharges[j])
		}
	}

	slack := decimal.New(2, -2)
	if inv.InvoiceCurrencyCode == "HUF" {
		slack = decimal.NewFromFloat(0.5)
	}
	for _, allowanceCharge := range documentAndLine {
		inv.checkContext()
		if allowanceCharge.hasPercentInXML && !allowanceCharge.hasBasisAmountInXML {
			inv.addViolation(rules.PEPPOLEN16931R41, "Allowance/charge percentage requires a base amount")
		}
		if !allowanceCharge.hasPercentInXML && allowanceCharge.hasBasisAmountInXML {
			inv.addViolation(rules.PEPPOLEN16931R42, "Allowance/charge base amount requires a percentage")
		}
		if allowanceCharge.hasPercentInXML && allowanceCharge.hasBasisAmountInXML {
			expected := allowanceCharge.BasisAmount.Mul(allowanceCharge.CalculationPercent).Div(decimal100)
			if allowanceCharge.ActualAmount.Sub(expected).Abs().GreaterThan(slack) {
				inv.addViolation(rules.PEPPOLEN16931R40, "Allowance/charge amount differs from base amount times percentage")
			}
		}
		if inv.isParsed && (!allowanceCharge.hasIndicatorInXML || !allowanceCharge.indicatorValidXML) {
			rule := rules.PEPPOLEN16931R43
			if inv.SchemaType == CII {
				rule = rules.PEPPOLEN16931R431
			}
			inv.addViolation(rule, "Allowance/charge indicator is not an XML boolean")
		}
	}

	for i := range inv.InvoiceLines {
		line := &inv.InvoiceLines[i]
		for j := range line.AppliedTradeAllowanceCharge {
			allowanceCharge := &line.AppliedTradeAllowanceCharge[j]
			if inv.SchemaType == CII && inv.isParsed && (!allowanceCharge.hasIndicatorInXML || !allowanceCharge.indicatorValidXML) {
				inv.addViolation(rules.PEPPOLEN16931R432, "Price allowance indicator is not an XML boolean")
			}
			if allowanceCharge.ChargeIndicator {
				inv.addViolation(rules.PEPPOLEN16931R44, "Price-level allowance indicator must be false")
			}
			if inv.SchemaType == UBL && allowanceCharge.hasBasisAmountInXML && !line.NetPrice.Equal(allowanceCharge.BasisAmount.Sub(allowanceCharge.ActualAmount)) {
				inv.addViolation(rules.PEPPOLEN16931R46, "Item net price does not equal gross price minus price discount")
			}
		}
		if inv.SchemaType == CII && line.hasGrossPriceInXML {
			discount := decimal.Zero
			if len(line.AppliedTradeAllowanceCharge) > 0 {
				discount = line.AppliedTradeAllowanceCharge[0].ActualAmount
			}
			if !line.NetPrice.Equal(line.GrossPrice.Sub(discount)) {
				inv.addViolation(rules.PEPPOLEN16931R46, "Item net price does not equal gross price minus price discount")
			}
		}
	}
}

func (inv *Invoice) validatePEPPOLPaymentAndPeriods() {
	for i := range inv.PaymentMeans {
		paymentMeans := &inv.PaymentMeans[i]
		if paymentMeans.TypeCode == 49 || paymentMeans.TypeCode == 59 {
			hasMandate := paymentMeans.mandateIDXML != ""
			if inv.SchemaType == CII {
				hasMandate = false
				for _, terms := range inv.SpecifiedTradePaymentTerms {
					if terms.DirectDebitMandateID != "" {
						hasMandate = true
						break
					}
				}
			}
			if !hasMandate {
				inv.addViolation(rules.PEPPOLEN16931R61, "Mandate reference is required for direct debit")
			}
		}
	}
	for i := range inv.InvoiceLines {
		line := &inv.InvoiceLines[i]
		if inv.lineHasInvalidObjectReference(line) {
			inv.addViolation(rules.PEPPOLEN16931R101, "Line document reference is not an invoiced object reference")
		}
		if !inv.BillingSpecifiedPeriodStart.IsZero() && !line.BillingSpecifiedPeriodStart.IsZero() && line.BillingSpecifiedPeriodStart.Before(inv.BillingSpecifiedPeriodStart) {
			inv.addViolation(rules.PEPPOLEN16931R110, "Line period starts before the invoice period")
		}
		if !inv.BillingSpecifiedPeriodEnd.IsZero() && !line.BillingSpecifiedPeriodEnd.IsZero() && line.BillingSpecifiedPeriodEnd.After(inv.BillingSpecifiedPeriodEnd) {
			inv.addViolation(rules.PEPPOLEN16931R111, "Line period ends after the invoice period")
		}
		if inv.SchemaType == CII && line.hasGrossBasisQuantityInXML {
			if !line.grossBasisQuantity.IsPositive() {
				inv.addViolation(rules.PEPPOLEN16931R121, "Gross-price base quantity must be positive")
			}
			if line.grossBasisQuantityUnit != "" && line.grossBasisQuantityUnit != line.BilledQuantityUnit {
				inv.addViolation(rules.PEPPOLEN16931R130, "Gross-price base quantity unit differs from invoiced quantity unit")
			}
		}
	}
}

func (inv *Invoice) lineHasInvalidObjectReference(line *InvoiceLine) bool {
	return line.lineDocumentReferenceCount > 0 && line.AdditionalReferencedDocumentTypeCode != "130"
}
