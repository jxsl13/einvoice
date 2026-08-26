package einvoice

import (
	"strconv"
	"strings"

	"github.com/jxsl13/einvoice/rules"
)

const (
	ciiDocumentTypeCodes   = " 71 80 81 82 83 84 102 130 202 203 204 211 218 219 261 262 295 296 308 325 326 331 380 381 382 383 384 385 386 387 388 389 390 393 394 395 396 420 456 457 458 471 472 473 500 501 502 503 527 532 553 575 623 633 751 780 817 870 875 876 877 935 "
	ublInvoiceTypeCodes    = " 71 80 81 82 84 102 130 202 203 204 211 218 219 295 325 326 331 380 382 383 384 385 386 387 388 389 390 393 394 395 456 457 471 472 473 500 501 502 503 527 553 575 623 633 751 780 817 870 875 876 877 935 "
	ublCreditNoteTypeCodes = " 81 83 261 262 296 308 381 396 420 458 532 "
	ciiDueDateTypeCodes    = " 3 5 29 72 "
	iso4217CurrencyCodes   = " AED AFN ALL AMD ANG AOA ARS AUD AWG AZN BAM BBD BDT BGN BHD BIF BMD BND BOB BOV BRL BSD BTN BWP BYN BZD CAD CDF CHE CHF CHW CLF CLP CNH CNY COP COU CRC CUP CVE CZK DJF DKK DOP DZD EGP ERN ETB EUR FJD FKP GBP GEL GHS GIP GMD GNF GTQ GYD HKD HNL HTG HUF IDR ILS INR IQD IRR ISK JMD JOD JPY KES KGS KHR KMF KPW KRW KWD KYD KZT LAK LBP LKR LRD LSL LYD MAD MDL MGA MKD MMK MNT MOP MRU MUR MVR MWK MXN MXV MYR MZN NAD NGN NIO NOK NPR NZD OMR PAB PEN PGK PHP PKR PLN PYG QAR RON RSD RUB RWF SAR SBD SCR SDG SEK SGD SHP SLE SOS SRD SSP STD SVC SYP SZL THB TJS TMT TND TOP TRY TTD TWD TZS UAH UGX USD USN UYI UYU UYW UZS VES VED VND VUV WST XAF XAG XAU XBA XBB XBC XBD XCD XDR XOF XPD XPF XPT XSU XTS XUA XXX YER ZAR ZMW ZWG "
	untdid7143Codes        = " AA AB AC AD AE AF AG AH AI AJ AK AL AM AN AO AP AQ AR AS AT AU AV AW AX AY AZ BA BB BC BD BE BF BG BH BI BJ BK BL BM BN BO BP BQ BR BS BT BU BV BW BX BY BZ CC CG CL CR CV DR DW EC EF EMD EN FS GB GN GMN GS HS IB IN IS IT IZ MA MF MN MP NB ON PD PL PO PPI PV QS RC RN RU RY SA SG SK SN SRS SRT SRU SRV SRW SRX SRY SRZ SS SSA SSB SSC SSD SSE SSF SSG SSH SSI SSJ SSK SSL SSM SSN SSO SSP SSQ SSR SSS SST SSU SSV SSW SSX SSY SSZ ST STA STB STC STD STE STF STG STH STI STJ STK STL STM STN STO STP STQ STR STS STT STU STV STW STX STY STZ SUA SUB SUC SUD SUE SUF SUG SUH SUI SUJ SUK SUL SUM TG TSN TSO TSP TSQ TSR TSS TST TSU UA UP VN VP VS VX ZZZ "
	uncl5189AllowanceCodes = " 41 42 60 62 63 64 65 66 67 68 70 71 88 95 100 102 103 104 105 "
	uncl7161ChargeCodes    = " AA AAA AAC AAD AAE AAF AAH AAI AAS AAT AAV AAY AAZ ABA ABB ABC ABD ABF ABK ABL ABN ABR ABS ABT ABU ACF ACG ACH ACI ACJ ACK ACL ACM ACS ADC ADE ADJ ADK ADL ADM ADN ADO ADP ADQ ADR ADT ADW ADY ADZ AEA AEB AEC AED AEF AEH AEI AEJ AEK AEL AEM AEN AEO AEP AES AET AEU AEV AEW AEX AEY AEZ AJ AU CA CAB CAD CAE CAF CAI CAJ CAK CAL CAM CAN CAO CAP CAQ CAR CAS CAT CAU CAV CAW CAX CAY CAZ CD CG CS CT DAB DAD DAC DAF DAG DAH DAI DAJ DAK DAL DAM DAN DAO DAP DAQ DL EG EP ER FAA FAB FAC FC FH FI GAA HAA HD HH IAA IAB ID IF IR IS KO L1 LA LAA LAB LF MAE MI ML NAA OA PA PAA PC PL PRV RAB RAC RAD RAF RE RF RH RV SA SAA SAD SAE SAI SG SH SM SU TAB TAC TT TV V1 V2 WH XAA YY ZZZ "
)

func (inv *Invoice) validateSyntaxRules() {
	if !inv.isParsed {
		return
	}
	profile := inv.GuidelineSpecifiedDocumentContextParameter
	if profile != SpecEN16931 && profile != SpecXRechnung30 && profile != SpecPEPPOLBilling30 {
		return
	}

	code := " " + strconv.Itoa(int(inv.InvoiceTypeCode)) + " "
	allowedCodes := ciiDocumentTypeCodes
	if inv.SchemaType == UBL {
		allowedCodes = ublInvoiceTypeCodes
		if inv.isUBLCreditNoteXML {
			allowedCodes = ublCreditNoteTypeCodes
		}
	}
	if !strings.Contains(allowedCodes, code) {
		inv.addViolation(rules.BRCL1, "Document type code is outside the syntax-specific UNTDID 1001 code list")
	}
	if !strings.Contains(iso4217CurrencyCodes, " "+inv.InvoiceCurrencyCode+" ") {
		inv.addViolation(rules.BRCL4, "Invoice currency code is outside the pinned ISO 4217 list")
	}
	if inv.TaxCurrencyCode != "" && !strings.Contains(iso4217CurrencyCodes, " "+inv.TaxCurrencyCode+" ") {
		inv.addViolation(rules.BRCL5, "Tax currency code is outside the pinned ISO 4217 list")
	}
	for i := range inv.InvoiceLines {
		for _, classification := range inv.InvoiceLines[i].ProductClassification {
			if classification.ClassCode != "" && classification.hasListIDInXML && !strings.Contains(untdid7143Codes, " "+classification.ListID+" ") {
				inv.addViolation(rules.BRCL13, "Item classification scheme is outside the pinned UNTDID 7143 list")
			}
		}
	}
	validateReasonCodes := func(values []AllowanceCharge) {
		for i := range values {
			if values[i].ReasonCode == "" {
				continue
			}
			codes, rule := uncl5189AllowanceCodes, rules.BRCL19
			if values[i].ChargeIndicator {
				codes, rule = uncl7161ChargeCodes, rules.BRCL20
			}
			if !strings.Contains(codes, " "+values[i].ReasonCode+" ") {
				inv.addViolation(rule, "Allowance/charge reason code is outside the applicable UNCL code list")
			}
		}
	}
	validateReasonCodes(inv.SpecifiedTradeAllowanceCharge)
	for i := range inv.InvoiceLines {
		validateReasonCodes(inv.InvoiceLines[i].InvoiceLineAllowances)
		validateReasonCodes(inv.InvoiceLines[i].InvoiceLineCharges)
	}

	if inv.SchemaType == UBL {
		parties := []*Party{&inv.Seller, &inv.Buyer, inv.PayeeTradeParty, inv.SellerTaxRepresentativeTradeParty, inv.ShipTo}
		for _, party := range parties {
			if party != nil && party.ublTaxSchemeMissingCompanyID {
				inv.addViolation(rules.UBLSR53, "PartyTaxScheme supplies TaxScheme/ID without CompanyID")
			}
		}
		for i := range inv.InvoiceLines {
			if !hasMaxDecimals(inv.InvoiceLines[i].Total, 2) {
				inv.addViolation(rules.UBLDT1, "UBL amount has more than two fraction digits")
				break
			}
		}
		if profile == SpecXRechnung30 {
			inv.validateXRechnungTaxOverlay()
		}
		return
	}

	if inv.ciiPaymentTermsCount > 1 {
		inv.addWarning(rules.CIISR452, "More than one SpecifiedTradePaymentTerms is present")
	}
	if inv.ciiPaymentDescriptionCount > 1 {
		inv.addWarning(rules.CIISR453, "More than one payment-terms Description is present")
	}
	if inv.ciiLineTradeTaxInvalid {
		inv.addWarning(rules.CIISR454, "An invoice line does not contain exactly one ApplicableTradeTax")
	}
	if inv.ciiTaxPointDateMax > 1 {
		inv.addViolation(rules.CIISR461, "More than one TaxPointDate is present")
	}
	uniqueDueDateTypes := make(map[string]struct{}, len(inv.ciiDueDateTypeCodes))
	invalidDueDateType := false
	for _, dueDateType := range inv.ciiDueDateTypeCodes {
		uniqueDueDateTypes[dueDateType] = struct{}{}
		if !strings.Contains(ciiDueDateTypeCodes, " "+dueDateType+" ") {
			invalidDueDateType = true
		}
	}
	if invalidDueDateType {
		inv.addViolation(rules.BRCL6, "VAT point date code is outside the restricted UNTDID 2475 code list")
	}
	if len(uniqueDueDateTypes) > 1 {
		inv.addViolation(rules.CIISR462, "More than one DueDateTypeCode is present")
	}
	if inv.TaxCurrencyCode != "" && countTaxTotals(inv.taxTotalsXML, inv.TaxCurrencyCode) == 0 {
		inv.addViolation(rules.BRDEC15, "No CII accounting-currency tax total satisfies BR-DEC-15")
	}
	if profile == SpecXRechnung30 {
		inv.validateXRechnungTaxOverlay()
	}
}

func (inv *Invoice) validateXRechnungTaxOverlay() {
	if inv.TaxCurrencyCode != "" && inv.TaxCurrencyCode == inv.InvoiceCurrencyCode {
		inv.addViolation(rules.PEPPOLEN16931R5, "Tax currency equals invoice currency")
	}

	if inv.SchemaType == CII {
		invoiceTotals := countTaxTotals(inv.taxTotalsXML, inv.InvoiceCurrencyCode)
		nonInvoiceTotals := len(inv.taxTotalsXML) - invoiceTotals
		if invoiceTotals > 1 {
			inv.addViolation(rules.PEPPOLEN16931R53, "More than one invoice-currency tax total is present")
		}
		expectedNonInvoiceTotals := 0
		if inv.TaxCurrencyCode != "" {
			expectedNonInvoiceTotals = 1
		}
		if nonInvoiceTotals != expectedNonInvoiceTotals {
			inv.addViolation(rules.PEPPOLEN16931R54, "Accounting-currency tax total cardinality is invalid")
		}
		if inv.TaxCurrencyCode != "" && invoiceTotals > 0 && !taxTotalSignsMatch(inv.taxTotalsXML, inv.InvoiceCurrencyCode, inv.TaxCurrencyCode) {
			inv.addViolation(rules.PEPPOLEN16931R55, "Invoice and accounting tax totals do not have the same operational sign")
		}
		return
	}

	withSubtotals := 0
	withoutSubtotals := 0
	for _, total := range inv.taxTotalsXML {
		if total.hasTaxSubtotal {
			withSubtotals++
		} else {
			withoutSubtotals++
		}
	}
	if withSubtotals != 1 {
		inv.addViolation(rules.PEPPOLEN16931R53, "Exactly one UBL tax total with subtotals is required")
	}
	expectedWithoutSubtotals := 0
	if inv.TaxCurrencyCode != "" {
		expectedWithoutSubtotals = 1
	}
	if withoutSubtotals != expectedWithoutSubtotals {
		inv.addViolation(rules.PEPPOLEN16931R54, "UBL accounting tax total cardinality is invalid")
	}
	if inv.TaxCurrencyCode != "" && !taxTotalSignsMatch(inv.taxTotalsXML, inv.InvoiceCurrencyCode, inv.TaxCurrencyCode) {
		inv.addViolation(rules.PEPPOLEN16931R55, "Invoice and accounting tax totals do not have the same operational sign")
	}
}

func countTaxTotals(totals []taxTotalXML, currency string) int {
	count := 0
	for _, total := range totals {
		if total.currency == currency {
			count++
		}
	}
	return count
}

func taxTotalSignsMatch(totals []taxTotalXML, invoiceCurrency, taxCurrency string) bool {
	var invoiceFound, taxFound bool
	var invoiceNegative, taxNegative bool
	for _, total := range totals {
		if total.currency == invoiceCurrency {
			invoiceFound = true
			invoiceNegative = total.amount.IsNegative()
		}
		if total.currency == taxCurrency {
			taxFound = true
			taxNegative = total.amount.IsNegative()
		}
	}
	return invoiceFound && taxFound && invoiceNegative == taxNegative
}
