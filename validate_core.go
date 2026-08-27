package einvoice

import (
	"fmt"
	"strings"

	"github.com/jxsl13/einvoice/rules"
	"github.com/shopspring/decimal"
)

const en16931VATCountryPrefixes = " 1A AD AE AF AG AI AL AM AN AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH EL ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS XI YE YT ZA ZM ZW "

func (inv *Invoice) validateCalculations() {
	var sum decimal.Decimal

	// BR-CO-9 VAT identifier country prefix validation
	// VAT identifiers shall use the exact code set pinned by EN 16931 1.3.15.
	validateVATIDPrefix := func(vatID string, fieldName string) {
		if vatID == "" {
			return // Empty VAT IDs are handled by other rules
		}
		if len(vatID) < 2 {
			inv.addViolation(rules.BRCO9, fieldName+" must have at least 2-character country prefix")
			return
		}
		prefix := vatID[:2]
		if !strings.Contains(en16931VATCountryPrefixes, " "+prefix+" ") {
			inv.addViolation(rules.BRCO9, fmt.Sprintf("%s has an unsupported EN 16931 country prefix (got: %s)", fieldName, prefix))
		}
	}

	validateVATIDPrefix(inv.Seller.VATaxRegistration, "Seller VAT identifier (BT-31)")
	validateVATIDPrefix(inv.Buyer.VATaxRegistration, "Buyer VAT identifier (BT-48)")
	if inv.SellerTaxRepresentativeTradeParty != nil {
		validateVATIDPrefix(inv.SellerTaxRepresentativeTradeParty.VATaxRegistration, "Seller tax representative VAT identifier (BT-63)")
	}

	// BR-CO-3 Rechnung
	// Umsatzsteuerdatum "Value added tax point date" (BT-7) und Code für das Umsatzsteuerdatum "Value added tax point date code" (BT-8)
	// schließen sich gegenseitig aus.
	for i := range inv.TradeTaxes {
		inv.checkContext()
		if !inv.TradeTaxes[i].TaxPointDate.IsZero() && inv.TradeTaxes[i].DueDateTypeCode != "" {
			inv.addViolation(rules.BRCO3, "TaxPointDate and DueDateTypeCode are mutually exclusive")
			break
		}
	}

	// BR-CO-4 Rechnungsposition
	// Jede Rechnungsposition "INVOICE LINE" (BG-25) muss anhand der Umsatzsteuerkategorie des in Rechnung gestellten Postens "Invoiced item VAT
	// category code" (BT-151) kategorisiert werden.
	// Sub invoice line aggregation lines (GROUP / INFORMATION) may omit the VAT
	// category (BR-FXEXT) and must be excluded from document-level sums.
	for i := range inv.InvoiceLines {
		inv.checkContext()
		if !inv.InvoiceLines[i].isDetailLine() {
			continue
		}
		if inv.InvoiceLines[i].TaxCategoryCode == "" {
			inv.addViolation(rules.BRCO4, fmt.Sprintf("Invoice line %s missing VAT category code", inv.InvoiceLines[i].LineID))
		}
	}

	// BR-CO-10 Gesamtsummen auf Dokumentenebene
	// Der Inhalt des Elementes "Sum of Invoice line net amount" (BT-106) entspricht der Summe aller Inhalte der Elemente "Invoice line net amount"
	// (BT-131).
	// Note: Only validate when invoice lines exist (Minimum profile may not have lines)
	if len(inv.InvoiceLines) > 0 {
		sum = decimal.Zero
		detailCount := 0
		for i := range inv.InvoiceLines {
			inv.checkContext()
			if !inv.InvoiceLines[i].isDetailLine() {
				continue
			}
			sum = sum.Add(inv.InvoiceLines[i].Total)
			detailCount++
		}
		// In the EXTENDED profile BR-CO-10 is replaced by BR-FXEXT-CO-10, which
		// allows a tolerance of 0.01 per line net amount (Factur-X 1.09).
		if inv.IsExtended() {
			tolerance := decimal.New(1, -2).Mul(decimal.NewFromInt(int64(detailCount)))
			if inv.LineTotal.Sub(sum).Abs().GreaterThan(tolerance) {
				inv.addViolation(rules.BRFXEXTCO10, fmt.Sprintf("Line total %s does not match sum of invoice lines %s (tolerance %s)", inv.LineTotal.String(), sum.String(), tolerance.String()))
			}
		} else {
			roundedSum := roundHalfUp(sum, 2)
			if !inv.LineTotal.Equal(roundedSum) {
				inv.addViolation(rules.BRCO10, fmt.Sprintf("Line total %s does not match rounded sum of invoice lines %s", inv.LineTotal.String(), roundedSum.String()))
			}
		}
	}

	// BR-CO-11 Gesamtsummen auf Dokumentenebene
	// Der Inhalt des Elementes "Sum of allowances on document level" (BT-107) entspricht der Summe aller Inhalte
	// der Elemente "Document level allowance amount" (BT-92).
	calculatedAllowanceTotal := decimal.Zero
	for i := range inv.SpecifiedTradeAllowanceCharge {
		inv.checkContext()
		if !inv.SpecifiedTradeAllowanceCharge[i].ChargeIndicator {
			calculatedAllowanceTotal = calculatedAllowanceTotal.Add(inv.SpecifiedTradeAllowanceCharge[i].ActualAmount)
		}
	}
	if !inv.AllowanceTotal.Equal(calculatedAllowanceTotal) {
		inv.addViolation(rules.BRCO11, fmt.Sprintf("Allowance total %s does not match sum of document level allowances %s", inv.AllowanceTotal.String(), calculatedAllowanceTotal.String()))
	}

	// BR-CO-12 Gesamtsummen auf Dokumentenebene
	// Der Inhalt des Elementes "Sum of charges on document level" (BT-108) entspricht der Summe aller Inhalte
	// der Elemente "Document level charge amount" (BT-99).
	calculatedChargeTotal := decimal.Zero
	for i := range inv.SpecifiedTradeAllowanceCharge {
		inv.checkContext()
		if inv.SpecifiedTradeAllowanceCharge[i].ChargeIndicator {
			calculatedChargeTotal = calculatedChargeTotal.Add(inv.SpecifiedTradeAllowanceCharge[i].ActualAmount)
		}
	}
	if !inv.ChargeTotal.Equal(calculatedChargeTotal) {
		inv.addViolation(rules.BRCO12, fmt.Sprintf("Charge total %s does not match sum of document level charges %s", inv.ChargeTotal.String(), calculatedChargeTotal.String()))
	}

	// BR-CO-13 Gesamtsummen auf Dokumentenebene
	// Der Inhalt des Elementes "Invoice total amount without VAT" (BT-109) entspricht der Summe aus "Sum of Invoice line net amount"
	// (BT-106) abzüglich "Sum of allowances on document level" (BT-107) zuzüglich "Sum of charges on document level" (BT-108).
	// Note: Minimum profile may not have LineTotal populated, so only validate when invoice lines exist
	if len(inv.InvoiceLines) > 0 || !inv.LineTotal.IsZero() {
		expectedTaxBasisTotal := inv.LineTotal.Sub(inv.AllowanceTotal).Add(inv.ChargeTotal)
		if !inv.TaxBasisTotal.Equal(expectedTaxBasisTotal) {
			inv.addViolation(rules.BRCO13, fmt.Sprintf("Tax basis total %s does not match LineTotal - AllowanceTotal + ChargeTotal = %s", inv.TaxBasisTotal.String(), expectedTaxBasisTotal.String()))
		}
	}

	// BR-CO-14 Gesamtsummen auf Dokumentenebene
	// Der Inhalt des Elementes "Invoice total VAT amount" (BT-110) entspricht der
	// Summe aller Inhalte der Elemente "VAT category tax amount" (BT-117).
	// Note: Minimum profile may not have VAT breakdown, so only validate when TradeTaxes exist
	if len(inv.TradeTaxes) > 0 {
		calculatedTaxTotal := decimal.Zero
		for i := range inv.TradeTaxes {
			inv.checkContext()
			calculatedTaxTotal = calculatedTaxTotal.Add(inv.TradeTaxes[i].CalculatedAmount)
		}
		if inv.isParsed && inv.SchemaType == CII && inv.GuidelineSpecifiedDocumentContextParameter == SpecXRechnung30 {
			for _, total := range inv.taxTotalsXML {
				if total.currency != inv.InvoiceCurrencyCode {
					continue
				}
				if !total.amount.Equal(roundHalfUp(calculatedTaxTotal, 2)) {
					inv.addViolation(rules.BRCO14, fmt.Sprintf("Invoice total VAT amount %s does not match sum of VAT category amounts %s", total.amount.String(), calculatedTaxTotal.String()))
				}
			}
		} else if inv.isParsed && inv.SchemaType == UBL {
			for _, total := range inv.taxTotalsXML {
				if total.hasTaxSubtotal && !total.amount.Equal(roundHalfUp(total.subtotalSum, 2)) {
					inv.addViolation(rules.BRCO14, fmt.Sprintf("Invoice total VAT amount %s does not match its VAT subtotal sum %s", total.amount.String(), total.subtotalSum.String()))
				}
			}
		} else if !inv.TaxTotal.Equal(calculatedTaxTotal) {
			inv.addViolation(rules.BRCO14, fmt.Sprintf("Invoice total VAT amount %s does not match sum of VAT category amounts %s", inv.TaxTotal.String(), calculatedTaxTotal.String()))
		}
	}

	// BR-CO-15 Gesamtsummen auf Dokumentenebene
	// Der Inhalt des Elementes "Invoice total amount with VAT" (BT-112) entspricht der Summe aus "Invoice total amount without VAT"
	// (BT-109) und "Invoice total VAT amount" (BT-110).
	// Per EN 16931 schematron: TaxInclusiveAmount = round((TaxExclusiveAmount + TaxAmount) * 100) / 100
	// This applies 2-decimal rounding to the calculated side to account for rounding during invoice generation
	expectedGrandTotal := roundHalfUp(inv.TaxBasisTotal.Add(inv.TaxTotal), 2)
	validGrandTotal := inv.GrandTotal.Equal(expectedGrandTotal)
	if inv.isParsed && inv.SchemaType == UBL {
		invoiceCurrencyTotals := 0
		invoiceCurrencyTaxTotal := decimal.Zero
		for _, total := range inv.taxTotalsXML {
			if total.currency == inv.InvoiceCurrencyCode {
				invoiceCurrencyTotals++
				invoiceCurrencyTaxTotal = total.amount
			}
		}
		expectedGrandTotal = roundHalfUp(inv.TaxBasisTotal.Add(invoiceCurrencyTaxTotal), 2)
		validGrandTotal = invoiceCurrencyTotals == 1 && inv.GrandTotal.Equal(expectedGrandTotal)
	} else if inv.isParsed && inv.SchemaType == CII {
		invoiceCurrencyTotals := 0
		invoiceCurrencyTaxTotal := decimal.Zero
		for _, total := range inv.taxTotalsXML {
			if total.currency == inv.InvoiceCurrencyCode {
				invoiceCurrencyTotals++
				invoiceCurrencyTaxTotal = total.amount
			}
		}
		expectedGrandTotal = roundHalfUp(inv.TaxBasisTotal.Add(invoiceCurrencyTaxTotal), 2)
		validGrandTotal = inv.GrandTotal.Equal(inv.TaxBasisTotal) ||
			(invoiceCurrencyTotals == 1 && inv.GrandTotal.Equal(expectedGrandTotal))
	}
	if !validGrandTotal {
		inv.addViolation(rules.BRCO15, fmt.Sprintf("Grand total %s does not match TaxBasisTotal + TaxTotal = %s", inv.GrandTotal.String(), expectedGrandTotal.String()))
	}

	// BR-CO-16 Gesamtsummen auf Dokumentenebene
	// Der Inhalt des Elementes "Amount due for payment" (BT-115) entspricht der Summe aus "Invoice total amount with VAT" (BT-112)
	// abzüglich "Paid amount" (BT-113) zuzüglich "Rounding amount" (BT-114).
	// Per EN 16931 schematron, this rule has different cases based on which fields are present:
	// 1. If PrepaidAmount exists but not RoundingAmount: round((TaxInclusiveAmount - PrepaidAmount) * 100) / 100 = PayableAmount
	// 2. If neither exists: PayableAmount = TaxInclusiveAmount
	// 3. If both exist: round((PayableAmount - RoundingAmount) * 100) / 100 = round((TaxInclusiveAmount - PrepaidAmount) * 100) / 100
	// 4. If only RoundingAmount exists: round((PayableAmount - RoundingAmount) * 100) / 100 = TaxInclusiveAmount
	hasPrepaid := !inv.TotalPrepaid.IsZero()
	hasRounding := !inv.RoundingAmount.IsZero()

	var valid bool
	switch {
	case hasPrepaid && !hasRounding:
		// Case 1: round((GrandTotal - PrepaidAmount) * 100) / 100 = DuePayableAmount
		expected := roundHalfUp(inv.GrandTotal.Sub(inv.TotalPrepaid), 2)
		valid = inv.DuePayableAmount.Equal(expected)
	case !hasPrepaid && !hasRounding:
		// Case 2: DuePayableAmount = GrandTotal
		valid = inv.DuePayableAmount.Equal(inv.GrandTotal)
	case hasPrepaid && hasRounding:
		// Case 3: round((DuePayableAmount - RoundingAmount) * 100) / 100 = round((GrandTotal - PrepaidAmount) * 100) / 100
		leftSide := roundHalfUp(inv.DuePayableAmount.Sub(inv.RoundingAmount), 2)
		rightSide := roundHalfUp(inv.GrandTotal.Sub(inv.TotalPrepaid), 2)
		valid = leftSide.Equal(rightSide)
	default:
		// Case 4: !hasPrepaid && hasRounding: round((DuePayableAmount - RoundingAmount) * 100) / 100 = GrandTotal
		leftSide := roundHalfUp(inv.DuePayableAmount.Sub(inv.RoundingAmount), 2)
		valid = leftSide.Equal(inv.GrandTotal)
	}

	if !valid {
		expectedDuePayableAmount := inv.GrandTotal.Sub(inv.TotalPrepaid).Add(inv.RoundingAmount)
		inv.addViolation(rules.BRCO16, fmt.Sprintf("Due payable amount %s does not match GrandTotal - TotalPrepaid + RoundingAmount = %s (with rounding)", inv.DuePayableAmount.String(), expectedDuePayableAmount.String()))
	}

	// BR-CO-17 Umsatzsteueraufschlüsselung
	// Der Inhalt des Elementes "VAT category tax amount" (BT-117) entspricht dem Inhalt des Elementes "VAT category taxable amount" (BT-116),
	// multipliziert mit dem Inhalt des Elementes "VAT category rate" (BT-119) geteilt durch 100, gerundet auf zwei Dezimalstellen.
	for i := range inv.TradeTaxes {
		inv.checkContext()
		expected := roundHalfUp(inv.TradeTaxes[i].BasisAmount.Mul(inv.TradeTaxes[i].Percent).Div(decimal100), 2)
		if !vatAmountWithinOfficialTolerance(inv.TradeTaxes[i].CalculatedAmount, expected) {
			inv.addViolation(rules.BRCO17, fmt.Sprintf("VAT category tax amount %s does not match expected %s (basis %s × rate %s ÷ 100)", inv.TradeTaxes[i].CalculatedAmount.String(), expected.String(), inv.TradeTaxes[i].BasisAmount.String(), inv.TradeTaxes[i].Percent.String()))
		}
	}

	// BR-CO-18 Umsatzsteueraufschlüsselung
	// Eine Rechnung (INVOICE) soll mindestens eine Gruppe "VAT BREAKDOWN" (BG-23) enthalten.
	// Note: This rule only applies to profiles >= BasicWL (Minimum profile doesn't require VAT breakdown)
	if inv.ProfileLevel() >= levelBasicWL && len(inv.TradeTaxes) < 1 {
		inv.addViolation(rules.BRCO18, "Invoice should contain at least one VAT BREAKDOWN")
	}

	// BR-CO-19 Liefer- oder Rechnungszeitraum
	// Wenn die Gruppe "INVOICING PERIOD" (BG-14) verwendet wird, müssen entweder das Element "Invoicing period start date" (BT-73) oder das
	// Element "Invoicing period end date" (BT-74) oder beide gefüllt sein.
	// Note: Only validates parsed XML where BG-14 element was present (tracked via hasBillingPeriodInXML flag).
	if inv.isParsed && inv.hasBillingPeriodInXML {
		if inv.BillingSpecifiedPeriodStart.IsZero() && inv.BillingSpecifiedPeriodEnd.IsZero() {
			inv.addViolation(rules.BRCO19, "If invoicing period (BG-14) is used, either start date (BT-73) or end date (BT-74) must be filled")
		}
	}

	// BR-CO-20 Rechnungszeitraum auf Positionsebene
	// Wenn die Gruppe "INVOICE LINE PERIOD" (BG-26) verwendet wird, müssen entweder das Element "Invoice line period start date" (BT-134) oder
	// das Element "Invoice line period end date" (BT-135) oder beide gefüllt sein.
	// Note: Only validates parsed XML where BG-26 element was present (tracked via linePeriodPresent flag).
	for i := range inv.InvoiceLines {
		inv.checkContext()
		if inv.InvoiceLines[i].linePeriodPresent {
			if inv.InvoiceLines[i].BillingSpecifiedPeriodStart.IsZero() && inv.InvoiceLines[i].BillingSpecifiedPeriodEnd.IsZero() {
				inv.addViolation(rules.BRCO20, fmt.Sprintf("Invoice line %d: if line period (BG-26) is used, either start date (BT-134) or end date (BT-135) must be filled", i+1))
			}
		}
	}

	// BR-CO-25 was removed from the EN 16931 CII schematron in v1.3.16
	// (anticipating the EN 16931 revision); it is no longer validated.

	// BR-CO-26 Verkäufer
	// In order for the buyer to automatically identify a supplier, at least one of the following shall be present:
	// - Seller identifier (BT-29)
	// - Seller legal registration identifier (BT-30)
	// - Seller VAT identifier (BT-31)
	hasSellerID := len(inv.Seller.ID) > 0 || len(inv.Seller.GlobalID) > 0
	hasLegalReg := inv.Seller.SpecifiedLegalOrganization != nil && inv.Seller.SpecifiedLegalOrganization.ID != ""
	hasVATID := inv.Seller.VATaxRegistration != ""
	if !hasSellerID && !hasLegalReg && !hasVATID {
		inv.addViolation(rules.BRCO26, "At least one seller identifier must be present: Seller ID (BT-29), Legal registration (BT-30), or VAT ID (BT-31)")
	}

	// Note: BR-CO-05, BR-CO-06, BR-CO-07, and BR-CO-08 are not validated here.
	// These rules state that reason codes and reason text "shall indicate the same type"
	// when both are present. However, implementing this check would require a lookup table
	// mapping codes to text that is not part of the EN 16931 specification.
	// The required validations are already covered by:
	// - BR-33: Allowances must have reason (BT-97) OR code (BT-98)
	// - BR-38: Charges must have reason (BT-104) OR code (BT-105)
	// - BR-42: Line allowances must have reason (BT-139) OR code (BT-140)
	// - BR-44: Line charges must have reason (BT-144) OR code (BT-145)
}

func vatAmountWithinOfficialTolerance(actual, expected decimal.Decimal) bool {
	return actual.Abs().Sub(expected.Abs()).Abs().LessThan(decimal.NewFromInt(1))
}

func (inv *Invoice) validateCore() {
	// Helper function to check if this invoice allows negative amounts.
	// Credit notes (381) and correction invoices (384) may have negative amounts throughout
	// as per EN 16931 support for negative grand totals in correction scenarios.
	// Additionally, correction invoices with type 380 that reference other invoices
	// (via BillingReference/InvoiceReferencedDocument) may also have negative amounts.
	allowsNegativeAmounts := func() bool {
		if inv.InvoiceTypeCode == 381 || inv.InvoiceTypeCode == 384 {
			return true
		}
		// Allow negative amounts for correction invoices (type 380) with billing references
		if inv.InvoiceTypeCode == 380 && len(inv.InvoiceReferencedDocument) > 0 {
			return true
		}
		return false
	}

	// BR-1
	// Eine Rechnung (INVOICE) muss eine Spezifikationskennung "Specification identification" (BT-24) enthalten.
	if inv.GuidelineSpecifiedDocumentContextParameter == "" {
		inv.addViolation(rules.BR1, "GuidelineSpecifiedDocumentContextParameter (BT-24) is empty")
	}
	// 	BR-2 Rechnung
	// Eine Rechnung (INVOICE) muss eine Rechnungsnummer "Invoice number" (BT-1) enthalten.
	if inv.InvoiceNumber == "" {
		inv.addViolation(rules.BR2, "No invoice number found")
	}
	// BR-3 Rechnung
	// Eine Rechnung (INVOICE) muss ein Rechnungsdatum "Invoice issue date" (BT-2) enthalten.
	if inv.InvoiceDate.IsZero() {
		inv.addViolation(rules.BR3, "Date is zero")
	}
	// BR-4 Rechnung
	// Eine Rechnung (INVOICE) muss einen Rechnungstyp-Code "Invoice type code" (BT-3) enthalten.
	if inv.InvoiceTypeCode == 0 {
		inv.addViolation(rules.BR4, "Invoice type code is 0")
	}
	// BR-5 Rechnung
	// Eine Rechnung (INVOICE) muss einen Währungs-Code "Invoice currency code" (BT-5) enthalten.
	if inv.InvoiceCurrencyCode == "" {
		inv.addViolation(rules.BR5, "Invoice currency code is empty")
	}
	// BR-6 Verkäufer
	// Eine Rechnung (INVOICE) muss den Verkäufernamen "Seller name" (BT-27) enthalten.
	if inv.Seller.Name == "" {
		inv.addViolation(rules.BR6, "Seller name is empty")
	}
	// BR-7 Käufer
	// Eine Rechnung (INVOICE) muss den Erwerbernamen "Buyer name" (BT-44) enthalten.
	if inv.Buyer.Name == "" {
		inv.addViolation(rules.BR7, "Buyer name is empty")
	}
	// BR-8 Verkäufer
	// Eine Rechnung (INVOICE) muss die postalische Anschrift des Verkäufers "SELLER POSTAL ADDRESS" (BG-5) enthalten.
	if inv.Seller.PostalAddress == nil {
		inv.addViolation(rules.BR8, "Seller has no postal address")
	} else if inv.Seller.PostalAddress.CountryID == "" {
		// BR-9 Verkäufer
		// Eine postalische Anschrift des Verkäufers "SELLER POSTAL ADDRESS" (BG-5) muss einen Verkäufer-Ländercode "Seller country code" (BT-40) enthalten.
		inv.addViolation(rules.BR9, "Seller country code empty")
	}
	if inv.ProfileLevel() > levelMinimum {
		// BR-10 Käufer
		// Eine Rechnung (INVOICE) muss die postalische Anschrift des Erwerbers "BUYER POSTAL ADDRESS" (BG-8) enthalten.
		if inv.Buyer.PostalAddress == nil {
			inv.addViolation(rules.BR10, "Buyer has no postal address")
		} else if inv.Buyer.PostalAddress.CountryID == "" {
			// BR-11 Käufer
			// Eine postalische Anschrift des Erwerbers "BUYER POSTAL ADDRESS" (BG-8) muss einen Erwerber-Ländercode "Buyer country code" (BT-55)
			// enthalten.
			inv.addViolation(rules.BR11, "Buyer country code empty")
		}
	}
	// BR-12 Gesamtsummen auf Dokumentenebene
	// Eine Rechnung (INVOICE) muss die Summe der Rechnungspositionen-Nettobeträge "Sum of Invoice line net amount" (BT-106) enthalten.
	// Note: EN 16931 schematron validates element presence in XML, not non-zero value.
	// Zero is valid (e.g., credit notes, zero-rated items).
	// This check only applies to parsed XML invoices; programmatically built invoices skip this.
	// Minimum profile doesn't require LineTotal (no invoice lines), so only check for profiles >= Basic
	if inv.isParsed {
		if inv.ProfileLevel() >= levelBasic && !inv.hasLineTotalInXML && inv.SchemaType == CII {
			inv.addViolation(rules.BR12, "LineTotalAmount element missing in XML")
		}
	}

	// BR-13 Gesamtsummen auf Dokumentenebene
	// Eine Rechnung (INVOICE) muss den Gesamtbetrag der Rechnung ohne Umsatzsteuer "Invoice total amount without VAT" (BT-109) enthalten.
	// Note: EN 16931 schematron validates element presence in XML, not non-zero value.
	// Zero is valid (e.g., zero-rated items).
	// This check only applies to parsed XML invoices; programmatically built invoices skip this.
	if inv.isParsed {
		if !inv.hasTaxBasisTotalInXML && inv.SchemaType == CII {
			inv.addViolation(rules.BR13, "TaxBasisTotalAmount element missing in XML")
		}
	}

	// BR-14 Gesamtsummen auf Dokumentenebene
	// Eine Rechnung (INVOICE) muss den Gesamtbetrag der Rechnung mit Umsatzsteuer "Invoice total amount with VAT" (BT-112) enthalten.
	// Note: EN 16931 schematron validates element presence in XML, not non-zero value.
	// Zero is valid (e.g., zero-rated items).
	// This check only applies to parsed XML invoices; programmatically built invoices skip this.
	if inv.isParsed {
		if !inv.hasGrandTotalInXML && inv.SchemaType == CII {
			inv.addViolation(rules.BR14, "GrandTotalAmount element missing in XML")
		}
	}

	// BR-15 Gesamtsummen auf Dokumentenebene
	// Eine Rechnung (INVOICE) muss den ausstehenden Betrag "Amount due for payment" (BT-115) enthalten.
	// Note: EN 16931 schematron validates element presence in XML, not non-zero value.
	// Zero is valid for prepaid invoices (TotalPrepaidAmount = GrandTotalAmount).
	// This check only applies to parsed XML invoices; programmatically built invoices skip this.
	if inv.isParsed {
		if !inv.hasDuePayableAmountInXML && inv.SchemaType == CII {
			inv.addViolation(rules.BR15, "DuePayableAmount element missing in XML")
		}
	}

	// BR-16 Rechnung
	// Eine Rechnung (INVOICE) muss mindestens eine Rechnungsposition "INVOICE LINE" (BG-25) enthalten.
	if is(levelBasic, inv) {
		if len(inv.InvoiceLines) == 0 {
			inv.addViolation(rules.BR16, "Invoice lines must be at least 1")
		}
	}
	// BR-17 Zahlungsempfänger
	// Eine Rechnung (INVOICE) muss den Namen des Zahlungsempfängers "Payee name" (BT-59) enthalten, wenn sich der Zahlungsempfänger "PAYEE"
	// (BG-10) vom Verkäufer "SELLER" (BG-4) unterscheidet.
	if inv.PayeeTradeParty != nil {
		if inv.PayeeTradeParty.Name == "" {
			inv.addViolation(rules.BR17, "Payee has no name, although different from seller")
		}
	}
	// BR-18 Steuerbevollmächtigter des Verkäufers
	// Eine Rechnung (INVOICE) muss den Namen des Steuervertreters des Verkäufers "Seller tax representative name" (BT-62) enthalten, wenn der
	// Verkäufer "SELLER" (BG-4) einen Steuervertreter (BG-11) hat.
	if trp := inv.SellerTaxRepresentativeTradeParty; trp != nil {
		if trp.Name == "" {
			inv.addViolation(rules.BR18, "Tax representative has no name, although seller has specified one")
		}
		// BR-19 Steuerbevollmächtigter des Verkäufers
		// Eine Rechnung (INVOICE) muss die postalische Anschrift des Steuervertreters "SELLER TAX REPRESENTATIVE POSTAL ADDRESS" (BG-12) enthalten,
		// wenn der Verkäufer "SELLER" (BG-4) einen Steuervertreter hat.
		if trp.PostalAddress == nil {
			inv.addViolation(rules.BR19, "Tax representative has no postal address")
		} else if trp.PostalAddress.CountryID == "" {
			// BR-20 Steuerbevollmächtigter des Verkäufers
			// Die postalische Anschrift des Steuervertreters des Verkäufers "SELLER TAX REPRESENTATIVE POSTAL ADDRESS" (BG-12) muss einen
			// Steuervertreter-Ländercode enthalten, wenn der Verkäufer "SELLER" (BG-4) einen Steuervertreter hat.
			inv.addViolation(rules.BR20, "Tax representative postal address missing country code")
		}
	}
	// Sub invoice line aggregation lines (GROUP / INFORMATION) may omit
	// quantity (BT-129), unit (BT-130) and net price (BT-146) per BR-FXEXT-*.
	// In the EXTENDED profile these base rules are replaced by their BR-FXEXT-*
	// counterparts (Factur-X 1.09).
	br22, br23, br26 := rules.BR22, rules.BR23, rules.BR26
	if inv.IsExtended() {
		br22, br23, br26 = rules.BRFXEXT22, rules.BRFXEXT23, rules.BRFXEXT26
	}
	for i := range inv.InvoiceLines {
		inv.checkContext()
		// BT-X-8 (EXTENDED): the invoice line subtype, when present, must be a
		// known value. isDetailLine treats any unknown value as an aggregation
		// line, which would silently drop the line from the totals and the VAT
		// breakdown, so a typo or future subtype is flagged here instead.
		if rc := inv.InvoiceLines[i].LineStatusReasonCode; rc != "" && rc != "DETAIL" && rc != "GROUP" && rc != "INFORMATION" {
			inv.addViolation(rules.BRUSER06, fmt.Sprintf("Invoice line %s has unknown subtype %q (BT-X-8); expected DETAIL, GROUP or INFORMATION", inv.InvoiceLines[i].LineID, rc))
		}
		isContainer := !inv.InvoiceLines[i].isDetailLine()
		// BR-21 Rechnungsposition
		// Jede Rechnungsposition "INVOICE LINE" (BG-25) muss eine eindeutige Bezeichnung "Invoice line identifier" (BT-126) haben.
		if inv.InvoiceLines[i].LineID == "" {
			inv.addViolation(rules.BR21, "Line has no line ID")
		}
		// BR-22 Rechnungsposition
		// Jede Rechnungsposition "INVOICE LINE" (BG-25) muss die Menge der in der betreffenden Position in Rechnung gestellten Waren oder
		// Dienstleistungen als Einzelposten "Invoiced quantity" (BT-129) enthalten.
		if !isContainer && inv.InvoiceLines[i].BilledQuantity.IsZero() {
			inv.addViolation(br22, "Line has no billed quantity")
		}
		// BR-23 Rechnungsposition
		// Jede Rechnungsposition "INVOICE LINE" (BG-25) muss eine Einheit zur Mengenangabe "Invoiced quantity unit of measure code" (BT-130)
		// enthalten.
		if !isContainer && inv.InvoiceLines[i].BilledQuantityUnit == "" {
			inv.addViolation(br23, "Line's billed quantity has no unit")
		}

		// BR-24 Rechnungsposition
		// Jede Rechnungsposition "INVOICE LINE" (BG-25) muss den Nettobetrag der Rechnungsposition "Invoice line net amount" (BT-131) enthalten.
		// Note: EN 16931 schematron validates element presence in XML, not non-zero value.
		// Zero is valid (e.g., free items, zero-rated services).
		// This check only applies to parsed XML invoices; programmatically built invoices skip this.
		if inv.isParsed {
			if !inv.InvoiceLines[i].hasLineTotalInXML && inv.SchemaType == CII {
				inv.addViolation(rules.BR24, "LineTotalAmount element missing in XML for line "+inv.InvoiceLines[i].LineID)
			}
		}

		// BR-25 Artikelinformationen
		// Jede Rechnungsposition "INVOICE LINE" (BG-25) muss den Namen des Postens "Item name" (BT-153) enthalten.
		if inv.InvoiceLines[i].ItemName == "" {
			inv.addViolation(rules.BR25, "Line's item name missing")
		}

		// BR-26 Detailinformationen zum Preis
		// Jede Rechnungsposition "INVOICE LINE" (BG-25) muss den Preis des Postens, ohne Umsatzsteuer, nach Abzug des für diese Rechnungsposition
		// geltenden Rabatts "Item net price" (BT-146) beinhalten.
		// Note: EN 16931 schematron validates element presence in XML, not non-zero value.
		// Zero is valid (e.g., free items, promotional products).
		// This check only applies to parsed XML invoices; programmatically built invoices skip this.
		if inv.isParsed && !isContainer {
			if !inv.InvoiceLines[i].hasNetPriceInXML && inv.SchemaType == CII {
				inv.addViolation(br26, "NetPrice ChargeAmount element missing in XML for line "+inv.InvoiceLines[i].LineID)
			}
		}

		// BR-27 Nettopreis des Artikels
		// Der Artikel-Nettobetrag "Item net price" (BT-146) darf nicht negativ sein.
		if inv.InvoiceLines[i].NetPrice.IsNegative() {
			inv.addViolation(rules.BR27, "Net price must not be negative")
		}
		// BR-28 Detailinformationen zum Preis
		// Der Einheitspreis ohne Umsatzsteuer vor Abzug des Postenpreisrabatts einer Rechnungsposition "Item gross price" (BT-148) darf nicht negativ
		// sein.
		if inv.InvoiceLines[i].GrossPrice.IsNegative() {
			inv.addViolation(rules.BR28, "Gross price must not be negative")
		}
	}
	// BR-29 Rechnungszeitraum
	// Wenn Start- und Enddatum des Rechnungszeitraums gegeben sind, muss das Enddatum "Invoicing period end date" (BT-74) nach dem Startdatum
	// "Invoicing period start date" (BT-73) liegen oder mit diesem identisch sein.
	// Only validate when BOTH dates are present (non-zero)
	if !inv.BillingSpecifiedPeriodStart.IsZero() && !inv.BillingSpecifiedPeriodEnd.IsZero() {
		if inv.BillingSpecifiedPeriodEnd.Before(inv.BillingSpecifiedPeriodStart) {
			inv.addViolation(rules.BR29, "Billing period end must be after start")
		}
	}
	for i := range inv.InvoiceLines {
		inv.checkContext()
		// BR-30 Rechnungszeitraum auf Positionsebene
		// Wenn Start- und Enddatum des Rechnungspositionenzeitraums gegeben sind, muss das Enddatum "Invoice line period end date" (BT-135) nach
		// dem Startdatum "Invoice line period start date" (BT-134) liegen oder mit diesem identisch sein.
		// Only validate when BOTH dates are present (non-zero)
		if !inv.InvoiceLines[i].BillingSpecifiedPeriodStart.IsZero() && !inv.InvoiceLines[i].BillingSpecifiedPeriodEnd.IsZero() {
			if inv.InvoiceLines[i].BillingSpecifiedPeriodEnd.Before(inv.InvoiceLines[i].BillingSpecifiedPeriodStart) {
				inv.addViolation(rules.BR30, "Line item billing period end must be after or identical to start")
			}
		}
	}

	for i := range inv.SpecifiedTradeAllowanceCharge {
		inv.checkContext()
		// BR-66 Specified Trade Allowance Charge
		// Each Specified Trade Allowance Charge shall contain a Charge Indicator.
		// Note: In Go, the boolean ChargeIndicator field always has a value (true or false),
		// so this rule is implicitly satisfied. This validation is kept for documentation
		// and to align with the EN 16931 specification.

		if inv.SpecifiedTradeAllowanceCharge[i].ChargeIndicator {
			// BR-36 Zuschläge auf Dokumentenebene
			// Jede Abgabe auf Dokumentenebene "DOCUMENT LEVEL CHARGES" (BG-21) muss einen Betrag "Document level charge amount" (BT-99)
			// aufweisen.
			if inv.isParsed && !inv.SpecifiedTradeAllowanceCharge[i].hasActualAmountInXML {
				inv.addViolation(rules.BR36, "Charge amount is missing")
			}

			// BR-37 Zuschläge auf Dokumentenebene
			// Jede Abgabe auf Dokumentenebene "DOCUMENT LEVEL CHARGES" (BG-21) muss einen Umsatzsteuer-Code "Document level charge VAT
			// category code" (BT-102) aufweisen.
			if inv.SpecifiedTradeAllowanceCharge[i].CategoryTradeTaxCategoryCode == "" {
				inv.addViolation(rules.BR37, "Charge tax category code not set")
			}
			// BR-38 Zuschläge auf Dokumentenebene
			// Jede Abgabe auf Dokumentenebene "DOCUMENT LEVEL CHARGES" (BG-21) muss einen Abgabegrund "Document level charge reason" (BT-104)
			// oder einen entsprechenden Code "Document level charge reason code" (BT-105) aufweisen.
			if inv.SpecifiedTradeAllowanceCharge[i].Reason == "" && inv.SpecifiedTradeAllowanceCharge[i].ReasonCode == "" {
				inv.addViolation(rules.BR38, "Charge reason empty or code unset")
				inv.addViolation(rules.BRCO22, "Document level charge must have a reason or reason code")
			}
			// BR-USER-03 Zuschläge auf Dokumentenebene
			// Der Betrag einer Abgabe auf Dokumentenebene "Document level charge amount" (BT-99) darf nicht negativ sein.
			// Note: Credit notes (381) and correction invoices (384) may have negative amounts as per EN 16931.
			if !allowsNegativeAmounts() && inv.SpecifiedTradeAllowanceCharge[i].ActualAmount.LessThan(decimal.Zero) {
				inv.addViolation(rules.BRUSER03, "Document level charge amount must not be negative")
			}
			// BR-USER-04 Zuschläge auf Dokumentenebene
			// Der Basisbetrag einer Abgabe auf Dokumentenebene "Document level charge base amount" (BT-100) darf nicht negativ sein.
			// Note: Credit notes (381) and correction invoices (384) may have negative amounts as per EN 16931.
			if !allowsNegativeAmounts() && inv.SpecifiedTradeAllowanceCharge[i].BasisAmount.LessThan(decimal.Zero) {
				inv.addViolation(rules.BRUSER04, "Document level charge base amount must not be negative")
			}
		} else {
			// BR-31 Abschläge auf Dokumentenebene
			// Jeder Nachlass für die Rechnung als Ganzes "DOCUMENT LEVEL ALLOWANCES" (BG-20) muss einen Betrag "Document level allowance amount"
			// (BT-92) aufweisen.
			if inv.isParsed && !inv.SpecifiedTradeAllowanceCharge[i].hasActualAmountInXML {
				inv.addViolation(rules.BR31, "Allowance amount is missing")
			}
			// BR-32 Abschläge auf Dokumentenebene
			// Jeder Nachlass für die Rechnung als Ganzes "DOCUMENT LEVEL ALLOWANCES" (BG-20) muss einen Umsatzsteuer-Code "Document level
			// allowance VAT category code" (BT-95) aufweisen.
			if inv.SpecifiedTradeAllowanceCharge[i].CategoryTradeTaxCategoryCode == "" {
				inv.addViolation(rules.BR32, "Allowance tax category code not set")
			}
			// BR-33 Abschläge auf Dokumentenebene
			// Jeder Nachlass für die Rechnung als Ganzes "DOCUMENT LEVEL ALLOWANCES" (BG-20) muss einen Nachlassgrund "Document level allowance
			// reason" (BT-97) oder einen entsprechenden Code "Document level allowance reason code" (BT-98") aufweisen.
			if inv.SpecifiedTradeAllowanceCharge[i].Reason == "" && inv.SpecifiedTradeAllowanceCharge[i].ReasonCode == "" {
				inv.addViolation(rules.BR33, "Allowance reason empty or code unset")
				inv.addViolation(rules.BRCO21, "Document level allowance must have a reason or reason code")
			}
			// BR-USER-01 Abschläge auf Dokumentenebene
			// Der Betrag eines Nachlasses auf Dokumentenebene "Document level allowance amount" (BT-92) darf nicht negativ sein.
			// Note: Credit notes (381) and correction invoices (384) may have negative amounts as per EN 16931.
			if !allowsNegativeAmounts() && inv.SpecifiedTradeAllowanceCharge[i].ActualAmount.LessThan(decimal.Zero) {
				inv.addViolation(rules.BRUSER01, "Document level allowance amount must not be negative")
			}
			// BR-USER-02 Abschläge auf Dokumentenebene
			// Der Basisbetrag eines Nachlasses auf Dokumentenebene "Document level allowance base amount" (BT-93) darf nicht negativ sein.
			// Note: Credit notes (381) and correction invoices (384) may have negative amounts as per EN 16931.
			if !allowsNegativeAmounts() && inv.SpecifiedTradeAllowanceCharge[i].BasisAmount.LessThan(decimal.Zero) {
				inv.addViolation(rules.BRUSER02, "Document level allowance base amount must not be negative")
			}
		}
	}

	for i := range inv.InvoiceLines {
		inv.checkContext()
		// BR-41 Abschläge auf Ebene der Rechnungsposition
		// Jeder Nachlass auf der Ebene der Rechnungsposition "INVOICE LINE ALLOWANCES" (BG-27) muss einen Betrag "Invoice line allowance amount"
		// (BT-136) aufweisen.
		for j := range inv.InvoiceLines[i].InvoiceLineAllowances {
			inv.checkContext()
			if inv.isParsed && !inv.InvoiceLines[i].InvoiceLineAllowances[j].hasActualAmountInXML {
				inv.addViolation(rules.BR41, "Line allowance amount is missing")
			}
			// BR-42 Abschläge auf Ebene der Rechnungsposition
			// Jeder Nachlass auf der Ebene der Rechnungsposition "INVOICE LINE ALLOWANCES" (BG-27) muss einen Nachlassgrund "Invoice line allowance
			// reason" (BT-139) oder einen entsprechenden Code "Invoice line allowance reason code" (BT-140) aufweisen.
			if inv.InvoiceLines[i].InvoiceLineAllowances[j].Reason == "" && inv.InvoiceLines[i].InvoiceLineAllowances[j].ReasonCode == "" {
				inv.addViolation(rules.BR42, "Line allowance must have a reason")
				inv.addViolation(rules.BRCO23, "Invoice line allowance must have a reason or reason code")
			}
		}
		for j := range inv.InvoiceLines[i].InvoiceLineCharges {
			inv.checkContext()
			// BR-43 Charge ou frais sur ligne de facture
			// Jede Abgabe auf der Ebene der Rechnungsposition "INVOICE LINE CHARGES" (BG-28) muss einen Betrag "Invoice line charge amount" (BT-141)
			// aufweisen.
			if inv.isParsed && !inv.InvoiceLines[i].InvoiceLineCharges[j].hasActualAmountInXML {
				inv.addViolation(rules.BR43, "Line charge amount is missing")
			}
			// BR-44 Charge ou frais sur ligne de facture
			// Jede Abgabe auf der Ebene der Rechnungsposition "INVOICE LINE CHARGES" (BG-28) muss einen Abgabegrund "Invoice line charge reason" (BT-
			// 144) oder einen entsprechenden Code "Invoice line charge reason code" (BT-145) aufweisen.
			if inv.InvoiceLines[i].InvoiceLineCharges[j].Reason == "" && inv.InvoiceLines[i].InvoiceLineCharges[j].ReasonCode == "" {
				inv.addViolation(rules.BR44, "Line charge must have a reason")
				inv.addViolation(rules.BRCO24, "Invoice line charge must have a reason or reason code")
			}
		}
	}

	for i := range inv.TradeTaxes {
		inv.checkContext()
		// BR-46 Umsatzsteueraufschlüsselung
		// Jede Umsatzsteueraufschlüsselung "VAT BREAKDOWN" (BG-23) muss den für
		// die betreffende Umsatzsteuerkategorie zu entrichtenden Gesamtbetrag
		// "VAT category tax amount" (BT-117) aufweisen.
		// Note: Zero is a valid value for exempt categories (E, AE, Z, G, O, IC, IG, IP).
		// Category-specific rules (BR-E-9, BR-AE-9, BR-Z-9, etc.) enforce when zero is required.
		// This rule only ensures the field is present, which it always is after parsing or calculation.

		// BR-48 Umsatzsteueraufschlüsselung
		// Jede Umsatzsteueraufschlüsselung "VAT BREAKDOWN" (BG-23) muss einen
		// Umsatzsteuersatz gemäß einer Kategorie "VAT category rate" (BT-119)
		// haben. Sofern die Rechnung von der Umsatzsteuer ausgenommen ist, ist
		// "0" zu übermitteln.
		// Note: Zero is a valid and required value for categories E, AE, Z, G, O, IC, IG, IP.
		// Category-specific rules (BR-S-5, BR-E-5, BR-AE-5, etc.) enforce the correct rate per category.
		// This rule only ensures the field is present, which it always is after parsing or calculation.

		// BR-45 Umsatzsteueraufschlüsselung
		// Jede Umsatzsteueraufschlüsselung "VAT BREAKDOWN" (BG-23) muss die
		// Summe aller nach dem jeweiligen Schlüssel zu versteuernden Beträge
		// "VAT category taxable amount" (BT-116) aufweisen.
		// BR-45 is a presence rule. Consistency of the taxable amount with
		// lines, allowances, and charges is covered by category-specific rules.
		if inv.isParsed && !inv.TradeTaxes[i].hasBasisAmountInXML {
			inv.addViolation(rules.BR45, "VAT category taxable amount is missing")
		}
		// BR-47 Umsatzsteueraufschlüsselung
		// Jede Umsatzsteueraufschlüsselung "VAT BREAKDOWN" (BG-23) muss über
		// eine codierte Bezeichnung einer Umsatzsteuerkategorie "VAT category
		// code" (BT-118) definiert werden.
		if inv.TradeTaxes[i].CategoryCode == "" {
			inv.addViolation(rules.BR47, "CategoryCode not set for applicable trade tax")
		}
	}
	for i := range inv.PaymentMeans {
		inv.checkContext()
		// BR-49 Zahlungsanweisungen
		// Die Zahlungsinstruktionen "PAYMENT INSTRUCTIONS" (BG-16) müssen den Zahlungsart-Code "Payment means type code" (BT-81) enthalten.
		if inv.PaymentMeans[i].TypeCode == 0 {
			inv.addViolation(rules.BR49, "Payment means type code must be set")
		}
	}
	// BR-50 Kontoinformationen
	// Die Kennung des Kontos, auf das die Zahlung erfolgen soll "Payment
	// account identifier" (BT-84), muss angegeben werden, wenn
	// Überweisungsinformationen in der Rechnung angegeben werden.
	// Note: This is a weaker version of BR-61 and is generally covered by BR-61

	// BR-51 Karteninformationen
	// Die letzten vier bis sechs Ziffern der Kreditkartennummer "Payment card
	// primary account number" (BT-87) sollen angegeben werden, wenn
	// Informationen zur Kartenzahlung übermittelt werden.
	// Note: This uses "sollen" (should) not "muss" (must), so it's a recommendation not requirement

	// BR-52 Rechnungsbegründende Unterlagen
	// Jede rechnungsbegründende Unterlage muss einen Dokumentenbezeichner
	// "Supporting document reference" (BT-122) haben.
	for i := range inv.AdditionalReferencedDocument {
		inv.checkContext()
		if inv.AdditionalReferencedDocument[i].IssuerAssignedID == "" {
			inv.addViolation(rules.BR52, "Supporting document must have a reference")
		}
	}

	// BR-53 Gesamtsummen auf Dokumentenebene
	// Wenn eine Währung für die Umsatzsteuerabrechnung angegeben wurde, muss
	// der Umsatzsteuergesamtbetrag in der Abrechnungswährung "Invoice total VAT
	// amount in accounting currency" (BT-111) angegeben werden.
	if inv.TaxCurrencyCode != "" && inv.isParsed && !inv.hasTaxTotalAccountingXML {
		inv.addViolation(rules.BR53, "Tax total in accounting currency must be specified when tax currency code is provided")
	}

	// BR-54 Artikelattribute
	// Jede Eigenschaft eines in Rechnung gestellten Postens "ITEM ATTRIBUTES"
	// (BG-32) muss eine Bezeichnung "Item attribute name" (BT-160) und einen
	// Wert "Item attribute value" (BT-161) haben.
	for i := range inv.InvoiceLines {
		inv.checkContext()
		for j := range inv.InvoiceLines[i].Characteristics {
			inv.checkContext()
			if inv.InvoiceLines[i].Characteristics[j].Description == "" || inv.InvoiceLines[i].Characteristics[j].Value == "" {
				inv.addViolation(rules.BR54, "Item attribute must have both name and value")
			}
		}
	}

	// BR-55 Referenz auf die vorausgegangene Rechnung
	// Jede Bezugnahme auf eine vorausgegangene Rechnung "Preceding Invoice
	// reference" (BT-25) muss die Nummer der vorausgegangenen Rechnung
	// enthalten.
	for _, ref := range inv.InvoiceReferencedDocument {
		inv.checkContext()
		if ref.ID == "" {
			inv.addViolation(rules.BR55, "Preceding invoice reference must contain invoice number")
		}
	}

	// BR-56 Steuerbevollmächtigter des Verkäufers
	// Jeder Steuervertreter des Verkäufers "SELLER TAX REPRESENTATIVE PARTY"
	// (BG-11) muss eine Umsatzsteuer-Identifikationsnummer "Seller tax
	// representative VAT identifier" (BT-63) haben.
	if inv.SellerTaxRepresentativeTradeParty != nil && inv.SellerTaxRepresentativeTradeParty.VATaxRegistration == "" {
		inv.addViolation(rules.BR56, "Seller tax representative must have VAT identifier")
	}

	// BR-57 Lieferanschrift
	// Jede Lieferadresse "DELIVER TO ADDRESS" (BG-15) muss einen entsprechenden
	// Ländercode "Deliver to country code" (BT-80) enthalten.
	if inv.ShipTo != nil && inv.ShipTo.PostalAddress != nil && inv.ShipTo.PostalAddress.CountryID == "" {
		inv.addViolation(rules.BR57, "Deliver-to address must have country code")
	}

	// BR-61: Payment account identifier (BT-84) required for credit transfers (codes 30, 58).
	// Note: Validates element presence per EN 16931 schematron, not value. Empty elements
	// like <ram:IBANID/> are valid. Only triggers when PayeePartyCreditorFinancialAccount
	// exists but neither IBANID nor ProprietaryID elements are present.
	if inv.isParsed {
		for i := range inv.PaymentMeans {
			inv.checkContext()
			if (inv.PaymentMeans[i].TypeCode == 30 || inv.PaymentMeans[i].TypeCode == 58) && inv.PaymentMeans[i].hasPayeeAccountInXML {
				if !inv.PaymentMeans[i].hasPayeeIBANInXML && !inv.PaymentMeans[i].hasPayeeProprietaryIDInXML {
					inv.addViolation(rules.BR61, "Payment account identifier required for credit transfer payment types")
				}
			}
		}
	}

	// BR-62 Elektronische Adresse des Verkäufers
	// Im Element "Seller electronic address" (BT-34) muss die Komponente
	// "Scheme Identifier" vorhanden sein.
	if inv.Seller.URIUniversalCommunication != "" && inv.Seller.URIUniversalCommunicationScheme == "" {
		inv.addViolation(rules.BR62, "Seller electronic address must have scheme identifier")
	}

	// BR-63 Elektronische Adresse des Käufers
	// Im Element "Buyer electronic address" (BT-49) muss die Komponente "Scheme
	// Identifier" vorhanden sein.
	if inv.Buyer.URIUniversalCommunication != "" && inv.Buyer.URIUniversalCommunicationScheme == "" {
		inv.addViolation(rules.BR63, "Buyer electronic address must have scheme identifier")
	}

	// BR-64 Kennung eines Artikels nach registriertem Schema
	// Im Element "Item standard identifier" (BT-157) muss die Komponente
	// "Scheme Identifier" vorhanden sein.
	for i := range inv.InvoiceLines {
		inv.checkContext()
		if inv.InvoiceLines[i].GlobalID != "" && inv.InvoiceLines[i].GlobalIDType == "" {
			inv.addViolation(rules.BR64, "Item standard identifier must have scheme identifier")
		}
	}

	// BR-65 Kennung der Artikelklassifizierung
	// Im Element "Item classification identifier" (BT-158) muss die Komponente
	// "Scheme Identifier" vorhanden sein.
	for i := range inv.InvoiceLines {
		inv.checkContext()
		for j := range inv.InvoiceLines[i].ProductClassification {
			inv.checkContext()
			if inv.InvoiceLines[i].ProductClassification[j].ClassCode != "" && inv.InvoiceLines[i].ProductClassification[j].ListID == "" {
				inv.addViolation(rules.BR65, "Item classification identifier must have scheme identifier")
			}
		}
	}

	// BR-B-1 Split payment (Italian domestic invoices)
	// An Invoice where the VAT category code is "Split payment" (B) shall be a domestic Italian invoice.
	// This means both seller and buyer must be located in Italy (IT).
	hasSplitPayment := false
	for i := range inv.InvoiceLines {
		inv.checkContext()
		if inv.InvoiceLines[i].TaxCategoryCode == "B" {
			hasSplitPayment = true
			break
		}
	}
	if !hasSplitPayment {
		for i := range inv.SpecifiedTradeAllowanceCharge {
			inv.checkContext()
			if inv.SpecifiedTradeAllowanceCharge[i].CategoryTradeTaxCategoryCode == "B" {
				hasSplitPayment = true
				break
			}
		}
	}
	if hasSplitPayment {
		// Check seller country
		sellerCountry := ""
		if inv.Seller.PostalAddress != nil {
			sellerCountry = inv.Seller.PostalAddress.CountryID
		}
		// Check buyer country
		buyerCountry := ""
		if inv.Buyer.PostalAddress != nil {
			buyerCountry = inv.Buyer.PostalAddress.CountryID
		}

		if sellerCountry != "IT" || buyerCountry != "IT" {
			inv.addViolation(rules.BRB1, "Split payment VAT category (B) requires both seller and buyer to be in Italy (IT)")
		}
	}

	// BR-B-2 Split payment and Standard rated exclusion
	// An Invoice with Split payment (B) shall not contain Standard rated (S) VAT category.
	hasStandardRated := false
	for i := range inv.InvoiceLines {
		inv.checkContext()
		if inv.InvoiceLines[i].TaxCategoryCode == "S" {
			hasStandardRated = true
			break
		}
	}
	if !hasStandardRated {
		for i := range inv.SpecifiedTradeAllowanceCharge {
			inv.checkContext()
			if inv.SpecifiedTradeAllowanceCharge[i].CategoryTradeTaxCategoryCode == "S" {
				hasStandardRated = true
				break
			}
		}
	}
	if hasSplitPayment && hasStandardRated {
		inv.addViolation(rules.BRB2, "Invoice with Split payment VAT category (B) must not contain Standard rated (S) category")
	}

	// VAT category validations - delegated to specialized methods
	inv.validateVATStandard()
	inv.validateVATReverse()
	inv.validateVATExempt()
	inv.validateVATZero()
	inv.validateVATExport()
	inv.validateVATIntracommunity()
	inv.validateVATIGIC()
	inv.validateVATIPSI()
	inv.validateVATNotSubject()
}

// hasMaxDecimals checks if a decimal value has at most maxDecimals decimal places.
// Values that equal their rounded form are considered valid (trailing zeros are ignored).
func hasMaxDecimals(value decimal.Decimal, maxDecimals int) bool {
	rounded := value.Round(int32(maxDecimals))
	return value.Equal(rounded)
}

func (inv *Invoice) validateDecimals() {
	// Helper function to validate decimal precision
	checkDecimalPrecision := func(value decimal.Decimal, fieldName string, btCode string, rule rules.Rule) {
		if !value.IsZero() && !hasMaxDecimals(value, 2) {
			inv.addViolation(rule, fmt.Sprintf("%s (%s) has more than 2 decimal places: %s", fieldName, btCode, value.String()))
		}
	}

	// BR-DEC-01: Document level allowance amount (BT-92)
	// BR-DEC-02: Document level allowance base amount (BT-93)
	// BR-DEC-05: Document level charge amount (BT-99)
	// BR-DEC-06: Document level charge base amount (BT-100)
	for i := range inv.SpecifiedTradeAllowanceCharge {
		inv.checkContext()
		if !inv.SpecifiedTradeAllowanceCharge[i].ChargeIndicator {
			// Allowance
			checkDecimalPrecision(inv.SpecifiedTradeAllowanceCharge[i].ActualAmount, "Document level allowance amount", "BT-92", rules.BRDEC1)
			checkDecimalPrecision(inv.SpecifiedTradeAllowanceCharge[i].BasisAmount, "Document level allowance base amount", "BT-93", rules.BRDEC2)
		} else {
			// Charge
			checkDecimalPrecision(inv.SpecifiedTradeAllowanceCharge[i].ActualAmount, "Document level charge amount", "BT-99", rules.BRDEC5)
			checkDecimalPrecision(inv.SpecifiedTradeAllowanceCharge[i].BasisAmount, "Document level charge base amount", "BT-100", rules.BRDEC6)
		}
	}

	// BR-DEC-09: Sum of Invoice line net amount (BT-106)
	checkDecimalPrecision(inv.LineTotal, "Sum of Invoice line net amount", "BT-106", rules.BRDEC9)

	// BR-DEC-10: Sum of allowances on document level (BT-107)
	checkDecimalPrecision(inv.AllowanceTotal, "Sum of allowances on document level", "BT-107", rules.BRDEC10)

	// BR-DEC-11: Sum of charges on document level (BT-108)
	checkDecimalPrecision(inv.ChargeTotal, "Sum of charges on document level", "BT-108", rules.BRDEC11)

	// BR-DEC-12: Invoice total amount without VAT (BT-109)
	checkDecimalPrecision(inv.TaxBasisTotal, "Invoice total amount without VAT", "BT-109", rules.BRDEC12)

	// BR-DEC-13: Invoice total VAT amount (BT-110)
	checkDecimalPrecision(inv.TaxTotal, "Invoice total VAT amount", "BT-110", rules.BRDEC13)

	// BR-DEC-14: Invoice total amount with VAT (BT-112)
	checkDecimalPrecision(inv.GrandTotal, "Invoice total amount with VAT", "BT-112", rules.BRDEC14)

	// BR-DEC-15: Invoice total VAT amount in accounting currency (BT-111)
	checkDecimalPrecision(inv.TaxTotalAccounting, "Invoice total VAT amount in accounting currency", "BT-111", rules.BRDEC15)

	// BR-DEC-16: Paid amount (BT-113)
	checkDecimalPrecision(inv.TotalPrepaid, "Paid amount", "BT-113", rules.BRDEC16)

	// BR-DEC-17: Rounding amount (BT-114)
	checkDecimalPrecision(inv.RoundingAmount, "Rounding amount", "BT-114", rules.BRDEC17)

	// BR-DEC-18: Amount due for payment (BT-115)
	checkDecimalPrecision(inv.DuePayableAmount, "Amount due for payment", "BT-115", rules.BRDEC18)

	// BR-DEC-19: VAT category taxable amount (BT-116)
	// BR-DEC-20: VAT category tax amount (BT-117)
	for i := range inv.TradeTaxes {
		inv.checkContext()
		checkDecimalPrecision(inv.TradeTaxes[i].BasisAmount, "VAT category taxable amount", "BT-116", rules.BRDEC19)
		checkDecimalPrecision(inv.TradeTaxes[i].CalculatedAmount, "VAT category tax amount", "BT-117", rules.BRDEC20)
	}

	// BR-DEC-23: Invoice line net amount (BT-131)
	// BR-DEC-24: Invoice line allowance amount (BT-136)
	// BR-DEC-25: Invoice line allowance base amount (BT-137)
	// BR-DEC-27: Invoice line charge amount (BT-141)
	// BR-DEC-28: Invoice line charge base amount (BT-142)
	for i := range inv.InvoiceLines {
		inv.checkContext()
		linePrefix := fmt.Sprintf("Line %d: ", i+1)
		checkDecimalPrecision(inv.InvoiceLines[i].Total, linePrefix+"Invoice line net amount", "BT-131", rules.BRDEC23)

		for j := range inv.InvoiceLines[i].InvoiceLineAllowances {
			inv.checkContext()
			checkDecimalPrecision(inv.InvoiceLines[i].InvoiceLineAllowances[j].ActualAmount, linePrefix+"Invoice line allowance amount", "BT-136", rules.BRDEC24)
			checkDecimalPrecision(inv.InvoiceLines[i].InvoiceLineAllowances[j].BasisAmount, linePrefix+"Invoice line allowance base amount", "BT-137", rules.BRDEC25)
		}

		for j := range inv.InvoiceLines[i].InvoiceLineCharges {
			inv.checkContext()
			checkDecimalPrecision(inv.InvoiceLines[i].InvoiceLineCharges[j].ActualAmount, linePrefix+"Invoice line charge amount", "BT-141", rules.BRDEC27)
			checkDecimalPrecision(inv.InvoiceLines[i].InvoiceLineCharges[j].BasisAmount, linePrefix+"Invoice line charge base amount", "BT-142", rules.BRDEC28)
		}
	}
}
