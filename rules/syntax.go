package rules

var (
	BRCL1    = Rule{Code: "BR-CL-01", Fields: []string{"BT-3"}, Description: "Document type code must use the applicable UNTDID 1001 code list."}
	BRCL4    = Rule{Code: "BR-CL-04", Fields: []string{"BT-5"}, Description: "Invoice currency code must use ISO 4217 alpha-3."}
	BRCL5    = Rule{Code: "BR-CL-05", Fields: []string{"BT-6"}, Description: "Tax currency code must use ISO 4217 alpha-3."}
	BRCL6    = Rule{Code: "BR-CL-06", Fields: []string{"BT-8"}, Description: "VAT point date code must use the restricted UNTDID 2475 code list."}
	BRCL13   = Rule{Code: "BR-CL-13", Fields: []string{"BT-158"}, Description: "Item classification scheme must use UNTDID 7143."}
	BRCL19   = Rule{Code: "BR-CL-19", Fields: []string{"BT-98", "BT-140"}, Description: "Coded allowance reasons must use UNCL 5189."}
	BRCL20   = Rule{Code: "BR-CL-20", Fields: []string{"BT-105", "BT-145"}, Description: "Coded charge reasons must use UNCL 7161."}
	UBLDT1   = Rule{Code: "UBL-DT-01", Fields: nil, Description: "UBL amounts may have at most two fraction digits."}
	UBLSR53  = Rule{Code: "UBL-SR-53", Fields: []string{"BG-4", "BG-7", "BG-8", "BG-10", "BG-11"}, Description: "CompanyID must be stated when PartyTaxScheme/TaxScheme/ID is provided."}
	CIISR452 = Rule{Code: "CII-SR-452", Fields: []string{"BG-16", "BT-20"}, Description: "Only one SpecifiedTradePaymentTerms may be present."}
	CIISR453 = Rule{Code: "CII-SR-453", Fields: []string{"BT-20"}, Description: "Only one SpecifiedTradePaymentTerms Description may be present."}
	CIISR454 = Rule{Code: "CII-SR-454", Fields: []string{"BG-25", "BT-151", "BT-152"}, Description: "Only one ApplicableTradeTax may be present per invoice line."}
	CIISR461 = Rule{Code: "CII-SR-461", Fields: []string{"BT-7"}, Description: "Only one TaxPointDate may be present per VAT breakdown."}
	CIISR462 = Rule{Code: "CII-SR-462", Fields: []string{"BT-8"}, Description: "Only one DueDateTypeCode may be present per VAT breakdown."}
)
