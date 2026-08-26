package einvoice

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/speedata/cxpath"
)

// CII (ZUGFeRD/Factur-X) namespace URN for root element
const nsCIIRootInvoice = "urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"

// parseCIITime parses CII format dates (YYYYMMDD) into time.Time.
func parseCIITime(ctx *cxpath.Context, path string) (time.Time, error) {
	timestring := ctx.Eval(path).String()
	if timestring == "" {
		return time.Time{}, nil
	}

	parsedDate, err := time.Parse("20060102", timestring)
	if err != nil {
		return parsedDate, fmt.Errorf("%w", err)
	}

	return parsedDate, nil
}

// parseCIIParty parses a party (buyer, seller, payee, etc.) from CII format.
// Uses CII-specific XPath with ram: namespace prefixes.
func parseCIIParty(tradeParty *cxpath.Context) Party {
	adr := Party{}
	for id := range tradeParty.Each("ram:ID") {
		adr.ID = append(adr.ID, id.String())
	}

	for gid := range tradeParty.Each("ram:GlobalID") {
		scheme := GlobalID{
			Scheme: gid.Eval("@schemeID").String(),
			ID:     gid.String(),
		}
		adr.GlobalID = append(adr.GlobalID, scheme)
	}

	adr.Name = tradeParty.Eval("ram:Name").String()
	// BT-33: Seller additional legal information (or buyer/payee/etc. description)
	adr.Description = tradeParty.Eval("ram:Description").String()

	// BT-34, BT-49: Electronic address with scheme
	adr.URIUniversalCommunication = tradeParty.Eval("ram:URIUniversalCommunication/ram:URIID").String()
	adr.URIUniversalCommunicationScheme = tradeParty.Eval("ram:URIUniversalCommunication/ram:URIID/@schemeID").String()

	if tradeParty.Eval("count(ram:SpecifiedLegalOrganization) > 0").Bool() {
		slo := SpecifiedLegalOrganization{}
		slo.ID = tradeParty.Eval("ram:SpecifiedLegalOrganization/ram:ID").String()
		slo.Scheme = tradeParty.Eval("ram:SpecifiedLegalOrganization/ram:ID/@schemeID").String()
		slo.TradingBusinessName = tradeParty.Eval("ram:SpecifiedLegalOrganization/ram:TradingBusinessName").String()
		adr.SpecifiedLegalOrganization = &slo
	}

	for dtc := range tradeParty.Each("ram:DefinedTradeContact") {
		contact := DefinedTradeContact{}
		contact.PersonName = dtc.Eval("ram:PersonName").String()
		contact.DepartmentName = dtc.Eval("ram:DepartmentName").String()
		contact.PhoneNumber = dtc.Eval("ram:TelephoneUniversalCommunication/ram:CompleteNumber").String()
		contact.EMail = dtc.Eval("ram:EmailURIUniversalCommunication/ram:URIID").String()
		adr.DefinedTradeContact = append(adr.DefinedTradeContact, contact)
	}

	if tradeParty.Eval("count(ram:PostalTradeAddress)").Int() > 0 {
		postalAddress := &PostalAddress{
			PostcodeCode:           tradeParty.Eval("ram:PostalTradeAddress/ram:PostcodeCode").String(),
			Line1:                  tradeParty.Eval("ram:PostalTradeAddress/ram:LineOne").String(),
			Line2:                  tradeParty.Eval("ram:PostalTradeAddress/ram:LineTwo").String(),
			Line3:                  tradeParty.Eval("ram:PostalTradeAddress/ram:LineThree").String(),
			City:                   tradeParty.Eval("ram:PostalTradeAddress/ram:CityName").String(),
			CountryID:              tradeParty.Eval("ram:PostalTradeAddress/ram:CountryID").String(),
			CountrySubDivisionName: tradeParty.Eval("ram:PostalTradeAddress/ram:CountrySubDivisionName").String(),
		}
		adr.PostalAddress = postalAddress
	}

	adr.FCTaxRegistration = tradeParty.Eval("ram:SpecifiedTaxRegistration/ram:ID[@schemeID='FC']").String()
	adr.VATaxRegistration = tradeParty.Eval("ram:SpecifiedTaxRegistration/ram:ID[@schemeID='VA']").String()

	return adr
}

// parseCII interprets the XML file as a ZUGFeRD or Factur-X cross industry invoice.
// It sets up CII-specific namespaces and parses the document structure.
func parseCII(operationCtx context.Context, ctx *cxpath.Context) (*Invoice, error) {
	// Setup CII namespaces
	ctx.SetNamespace("rsm", nsCIIRootInvoice)
	ctx.SetNamespace("ram", "urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100")
	ctx.SetNamespace("udt", "urn:un:unece:uncefact:data:standard:UnqualifiedDataType:100")
	ctx.SetNamespace("qdt", "urn:un:unece:uncefact:data:standard:QualifiedDataType:100")

	// Get root element after namespace setup
	root := ctx.Root()

	inv := &Invoice{SchemaType: CII, operationContext: operationCtx}
	inv.peppolEmptyElementCount = root.Eval("count(//*[not(self::ram:ApplicableHeaderTradeDelivery) and not(*) and not(normalize-space())])").Int()
	inv.checkContext()

	var err error
	if err = parseCIIExchangedDocumentContext(root.Eval("rsm:ExchangedDocumentContext"), inv); err != nil {
		return nil, err
	}
	inv.checkContext()

	if err = parseCIIExchangedDocument(root.Eval("rsm:ExchangedDocument"), inv); err != nil {
		return nil, err
	}
	inv.checkContext()

	if err = parseCIISupplyChainTradeTransaction(root.Eval("rsm:SupplyChainTradeTransaction"), inv); err != nil {
		return nil, err
	}
	inv.checkContext()

	return inv, nil
}

func parseCIIExchangedDocumentContext(ctx *cxpath.Context, inv *Invoice) error {
	// Store the raw URN value (BT-24 - Specification identifier)
	nc := ctx.Eval("ram:GuidelineSpecifiedDocumentContextParameter").Eval("ram:ID")
	inv.GuidelineSpecifiedDocumentContextParameter = nc.String()

	// Store the business process identifier (BT-23)
	inv.BPSpecifiedDocumentContextParameter = ctx.Eval("ram:BusinessProcessSpecifiedDocumentContextParameter/ram:ID").String()

	return nil
}

func parseCIIExchangedDocument(exchangedDocument *cxpath.Context, inv *Invoice) error {
	inv.InvoiceNumber = exchangedDocument.Eval("ram:ID/text()").String()
	inv.InvoiceTypeCode = CodeDocument(exchangedDocument.Eval("ram:TypeCode").Int())

	invoiceDate, err := parseCIITime(exchangedDocument, "ram:IssueDateTime/udt:DateTimeString")
	if err != nil {
		return err
	}

	inv.InvoiceDate = invoiceDate

	for note := range exchangedDocument.Each("ram:IncludedNote") {
		inv.checkContext()
		n := Note{}
		n.SubjectCode = note.Eval("ram:SubjectCode").String()
		n.Text = note.Eval("ram:Content").String()
		inv.Notes = append(inv.Notes, n)
	}

	return nil
}

func parseCIISupplyChainTradeTransaction(supplyChainTradeTransaction *cxpath.Context, inv *Invoice) error {
	var err error
	for code := range supplyChainTradeTransaction.Each(".//ram:ApplicableTradeTax/ram:DueDateTypeCode") {
		inv.ciiDueDateTypeCodes = append(inv.ciiDueDateTypeCodes, code.String())
	}
	// BG-25
	for lineItem := range supplyChainTradeTransaction.Each("ram:IncludedSupplyChainTradeLineItem") {
		inv.checkContext()
		invoiceLine := InvoiceLine{}
		invoiceLine.LineID = lineItem.Eval("ram:AssociatedDocumentLineDocument/ram:LineID").String()
		invoiceLine.ParentLineID = lineItem.Eval("ram:AssociatedDocumentLineDocument/ram:ParentLineID").String()
		invoiceLine.LineStatusCode = lineItem.Eval("ram:AssociatedDocumentLineDocument/ram:LineStatusCode").String()
		invoiceLine.LineStatusReasonCode = lineItem.Eval("ram:AssociatedDocumentLineDocument/ram:LineStatusReasonCode").String()
		invoiceLine.Note = lineItem.Eval("ram:AssociatedDocumentLineDocument/ram:IncludedNote/ram:Content").String()

		parseSpecifiedTradeProduct(lineItem.Eval("ram:SpecifiedTradeProduct"), &invoiceLine)
		specifiedLineTradeAgreement := lineItem.Eval("ram:SpecifiedLineTradeAgreement")
		if err = parseSpecifiedLineTradeAgreement(specifiedLineTradeAgreement, &invoiceLine); err != nil {
			return err
		}

		invoiceLine.BilledQuantity, err = getDecimal(lineItem, "ram:SpecifiedLineTradeDelivery/ram:BilledQuantity")
		if err != nil {
			return err
		}
		invoiceLine.BilledQuantityUnit = lineItem.Eval("ram:SpecifiedLineTradeDelivery/ram:BilledQuantity/@unitCode").String()
		// BR-24: Track XML element presence to validate later
		invoiceLine.hasLineTotalInXML = lineItem.Eval("count(ram:SpecifiedLineTradeSettlement/ram:SpecifiedTradeSettlementLineMonetarySummation/ram:LineTotalAmount)").Int() > 0
		invoiceLine.Total, err = getDecimal(lineItem, "ram:SpecifiedLineTradeSettlement/ram:SpecifiedTradeSettlementLineMonetarySummation/ram:LineTotalAmount")
		if err != nil {
			return err
		}

		for allowanceCharge := range lineItem.Each("ram:SpecifiedLineTradeSettlement/ram:SpecifiedTradeAllowanceCharge") {
			inv.checkContext()
			basisAmount, err := getDecimal(allowanceCharge, "ram:BasisAmount")
			if err != nil {
				return err
			}
			actualAmount, err := getDecimal(allowanceCharge, "ram:ActualAmount")
			if err != nil {
				return err
			}
			calculationPercent, err := getDecimal(allowanceCharge, "ram:CalculationPercent")
			if err != nil {
				return err
			}
			categoryTaxRate, err := getDecimal(allowanceCharge, "ram:CategoryTradeTax/ram:RateApplicablePercent")
			if err != nil {
				return err
			}

			alc := AllowanceCharge{
				ChargeIndicator:                       allowanceCharge.Eval("string(ram:ChargeIndicator/udt:Indicator) = 'true'").Bool(),
				BasisAmount:                           basisAmount,
				ActualAmount:                          actualAmount,
				CalculationPercent:                    calculationPercent,
				ReasonCode:                            allowanceCharge.Eval("ram:ReasonCode").String(),
				Reason:                                allowanceCharge.Eval("ram:Reason").String(),
				CategoryTradeTaxType:                  allowanceCharge.Eval("ram:CategoryTradeTax/ram:TypeCode").String(),
				CategoryTradeTaxCategoryCode:          allowanceCharge.Eval("ram:CategoryTradeTax/ram:CategoryCode").String(),
				CategoryTradeTaxRateApplicablePercent: categoryTaxRate,
				hasActualAmountInXML:                  allowanceCharge.Eval("count(ram:ActualAmount)").Int() > 0,
				hasBasisAmountInXML:                   allowanceCharge.Eval("count(ram:BasisAmount)").Int() > 0,
				hasPercentInXML:                       allowanceCharge.Eval("count(ram:CalculationPercent)").Int() > 0,
				hasIndicatorInXML:                     allowanceCharge.Eval("count(ram:ChargeIndicator/udt:Indicator)").Int() > 0,
				indicatorValidXML:                     isXMLBoolean(allowanceCharge.Eval("ram:ChargeIndicator/udt:Indicator").String()),
			}
			// Im Fall eines Abschlags (BG-27) ist der Wert des ChargeIndicators auf "false" zu setzen.
			// Im Fall eines Zuschlags (BG-28) ist der Wert des ChargeIndicators auf "true" zu setzen.
			if alc.ChargeIndicator {
				invoiceLine.InvoiceLineCharges = append(invoiceLine.InvoiceLineCharges, alc)
			} else {
				invoiceLine.InvoiceLineAllowances = append(invoiceLine.InvoiceLineAllowances, alc)
			}
		}

		lineTradeTaxCount := lineItem.Eval("count(ram:SpecifiedLineTradeSettlement/ram:ApplicableTradeTax)").Int()
		if lineTradeTaxCount != 1 {
			inv.ciiLineTradeTaxInvalid = true
		}
		// The semantic model has one line VAT category. If malformed CII supplies
		// multiple BG-30 groups, bind the first one exactly as the CII Schematron
		// does for scalar business terms; CII-SR-454 reports the cardinality.
		for taxInfo := range lineItem.Each("ram:SpecifiedLineTradeSettlement/ram:ApplicableTradeTax") {
			invoiceLine.TaxTypeCode = taxInfo.Eval("ram:TypeCode").String()
			invoiceLine.TaxCategoryCode = taxInfo.Eval("ram:CategoryCode").String()
			invoiceLine.TaxRateApplicablePercent, err = getDecimal(taxInfo, "ram:RateApplicablePercent")
			if err != nil {
				return err
			}
			invoiceLine.hasTaxRateApplicablePercent = taxInfo.Eval("count(ram:RateApplicablePercent)").Int() > 0
			break
		}
		// BR-CO-20: Track BG-26 (INVOICE LINE PERIOD) presence to validate later
		invoiceLine.linePeriodPresent = lineItem.Eval("count(ram:SpecifiedLineTradeSettlement/ram:BillingSpecifiedPeriod)").Int() > 0
		invoiceLine.BillingSpecifiedPeriodStart, err = parseCIITime(lineItem, "ram:SpecifiedLineTradeSettlement/ram:BillingSpecifiedPeriod/ram:StartDateTime/udt:DateTimeString")
		if err != nil {
			return fmt.Errorf("invalid line billing period start date for line %s: %w", invoiceLine.LineID, err)
		}
		invoiceLine.BillingSpecifiedPeriodEnd, err = parseCIITime(lineItem, "ram:SpecifiedLineTradeSettlement/ram:BillingSpecifiedPeriod/ram:EndDateTime/udt:DateTimeString")
		if err != nil {
			return fmt.Errorf("invalid line billing period end date for line %s: %w", invoiceLine.LineID, err)
		}

		// BT-128: Referenced document (line level)
		invoiceLine.lineDocumentReferenceCount = lineItem.Eval("count(ram:SpecifiedLineTradeSettlement/ram:AdditionalReferencedDocument)").Int()
		invoiceLine.AdditionalReferencedDocumentID = lineItem.Eval("ram:SpecifiedLineTradeSettlement/ram:AdditionalReferencedDocument/ram:IssuerAssignedID").String()
		invoiceLine.AdditionalReferencedDocumentTypeCode = lineItem.Eval("ram:SpecifiedLineTradeSettlement/ram:AdditionalReferencedDocument/ram:TypeCode").String()

		inv.InvoiceLines = append(inv.InvoiceLines, invoiceLine)
	}
	if err = parseCIIApplicableHeaderTradeAgreement(supplyChainTradeTransaction.Eval("ram:ApplicableHeaderTradeAgreement"), inv); err != nil {
		return err
	}

	if err = parseCIIApplicableHeaderTradeDelivery(supplyChainTradeTransaction.Eval("ram:ApplicableHeaderTradeDelivery"), inv); err != nil {
		return err
	}

	if err = parseCIIApplicableHeaderTradeSettlement(supplyChainTradeTransaction.Eval("ram:ApplicableHeaderTradeSettlement"), inv); err != nil {
		return err
	}

	return nil
}

func parseCIIApplicableHeaderTradeAgreement(applicableHeaderTradeAgreement *cxpath.Context, inv *Invoice) error {
	inv.BuyerReference = applicableHeaderTradeAgreement.Eval("ram:BuyerReference").String()
	// BT-13
	inv.BuyerOrderReferencedDocument = applicableHeaderTradeAgreement.Eval("ram:BuyerOrderReferencedDocument/ram:IssuerAssignedID").String() // BT-13
	// BT-12
	inv.ContractReferencedDocument = applicableHeaderTradeAgreement.Eval("ram:ContractReferencedDocument/ram:IssuerAssignedID").String() // BT-13
	inv.Buyer = parseCIIParty(applicableHeaderTradeAgreement.Eval("ram:BuyerTradeParty"))
	inv.Seller = parseCIIParty(applicableHeaderTradeAgreement.Eval("ram:SellerTradeParty"))

	if applicableHeaderTradeAgreement.Eval("count(ram:SellerTaxRepresentativeTradeParty)").Int() > 0 {
		trp := parseCIIParty(applicableHeaderTradeAgreement.Eval("ram:SellerTaxRepresentativeTradeParty"))
		inv.SellerTaxRepresentativeTradeParty = &trp
	}

	// BT-14: Seller order reference
	inv.SellerOrderReferencedDocument = applicableHeaderTradeAgreement.Eval("ram:SellerOrderReferencedDocument/ram:IssuerAssignedID").String()

	for additionalDocument := range applicableHeaderTradeAgreement.Each("ram:AdditionalReferencedDocument") {
		inv.checkContext()
		doc := Document{}
		doc.IssuerAssignedID = additionalDocument.Eval("ram:IssuerAssignedID").String()
		encoded := additionalDocument.Eval("ram:AttachmentBinaryObject").String()

		if encoded != "" {
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return fmt.Errorf("cannot decode attachment %w", err)
			}

			doc.AttachmentBinaryObject = data
		}

		doc.AttachmentFilename = additionalDocument.Eval("ram:AttachmentBinaryObject/@filename").String()
		doc.AttachmentMimeCode = additionalDocument.Eval("ram:AttachmentBinaryObject/@mimeCode").String()
		doc.URIID = additionalDocument.Eval("ram:URIID").String()
		doc.Name = additionalDocument.Eval("ram:Name").String()
		doc.TypeCode = additionalDocument.Eval("ram:TypeCode").String()
		doc.ReferenceTypeCode = additionalDocument.Eval("ram:ReferenceTypeCode").String()
		inv.AdditionalReferencedDocument = append(inv.AdditionalReferencedDocument, doc)
	}

	// BT-11: Project reference (ID and Name)
	inv.SpecifiedProcuringProjectID = applicableHeaderTradeAgreement.Eval("ram:SpecifiedProcuringProject/ram:ID").String()
	inv.SpecifiedProcuringProjectName = applicableHeaderTradeAgreement.Eval("ram:SpecifiedProcuringProject/ram:Name").String()

	return nil
}

func parseCIIApplicableHeaderTradeDelivery(applicableHeaderTradeDelivery *cxpath.Context, inv *Invoice) error {
	// BT-16: Despatch advice reference
	inv.DespatchAdviceReferencedDocument = applicableHeaderTradeDelivery.Eval("ram:DespatchAdviceReferencedDocument/ram:IssuerAssignedID").String()
	// BT-15: Receiving advice reference
	inv.ReceivingAdviceReferencedDocument = applicableHeaderTradeDelivery.Eval("ram:ReceivingAdviceReferencedDocument/ram:IssuerAssignedID").String()
	// BT-72
	var err error
	inv.OccurrenceDateTime, err = parseCIITime(applicableHeaderTradeDelivery, "ram:ActualDeliverySupplyChainEvent/ram:OccurrenceDateTime/udt:DateTimeString")
	if err != nil {
		return fmt.Errorf("invalid occurrence date time: %w", err)
	}

	if applicableHeaderTradeDelivery.Eval("count(ram:ShipToTradeParty)").Int() > 0 {
		st := parseCIIParty(applicableHeaderTradeDelivery.Eval("ram:ShipToTradeParty"))
		inv.ShipTo = &st
	}
	return nil
}

func parseCIIApplicableHeaderTradeSettlement(applicableHeaderTradeSettlement *cxpath.Context, inv *Invoice) error {
	var err error

	inv.InvoiceCurrencyCode = applicableHeaderTradeSettlement.Eval("ram:InvoiceCurrencyCode").String()
	// BT-6: Tax currency code (accounting currency, if different from invoice currency)
	inv.TaxCurrencyCode = applicableHeaderTradeSettlement.Eval("ram:TaxCurrencyCode").String()
	// BT-90: Creditor reference ID
	inv.CreditorReferenceID = applicableHeaderTradeSettlement.Eval("ram:CreditorReferenceID").String()
	// BT-83: Payment reference (remittance information)
	inv.PaymentReference = applicableHeaderTradeSettlement.Eval("ram:PaymentReference").String()
	// BG-10
	if applicableHeaderTradeSettlement.Eval("count(ram:PayeeTradeParty)").Int() > 0 {
		ptp := parseCIIParty(applicableHeaderTradeSettlement.Eval("ram:PayeeTradeParty"))
		inv.PayeeTradeParty = &ptp
	}

	for paymentMeans := range applicableHeaderTradeSettlement.Each("ram:SpecifiedTradeSettlementPaymentMeans") {
		inv.checkContext()
		// BG-16
		thisPaymentMeans := PaymentMeans{
			TypeCode:                                             paymentMeans.Eval("ram:TypeCode").Int(),
			Information:                                          paymentMeans.Eval("ram:Information").String(),
			PayeePartyCreditorFinancialAccountIBAN:               paymentMeans.Eval("ram:PayeePartyCreditorFinancialAccount/ram:IBANID").String(),
			PayeePartyCreditorFinancialAccountName:               paymentMeans.Eval("ram:PayeePartyCreditorFinancialAccount/ram:AccountName").String(),
			PayeePartyCreditorFinancialAccountProprietaryID:      paymentMeans.Eval("ram:PayeePartyCreditorFinancialAccount/ram:ProprietaryID").String(),
			PayeeSpecifiedCreditorFinancialInstitutionBIC:        paymentMeans.Eval("ram:PayeeSpecifiedCreditorFinancialInstitution/ram:BICID").String(),
			PayerPartyDebtorFinancialAccountIBAN:                 paymentMeans.Eval("ram:PayerPartyDebtorFinancialAccount/ram:IBANID").String(),
			ApplicableTradeSettlementFinancialCardID:             paymentMeans.Eval("ram:ApplicableTradeSettlementFinancialCard/ram:ID").String(),
			ApplicableTradeSettlementFinancialCardCardholderName: paymentMeans.Eval("ram:ApplicableTradeSettlementFinancialCard/ram:CardholderName").String(),
			// BR-61: Track XML element presence to validate later.
			// Per EN 16931 schematron, BR-61 test is "(ram:IBANID) or (ram:ProprietaryID)"
			// which checks for element PRESENCE, not value. An empty element <ram:IBANID/>
			// satisfies the test because the element exists.
			hasPayeeAccountInXML:       paymentMeans.Eval("count(ram:PayeePartyCreditorFinancialAccount)").Int() > 0,
			hasPayeeIBANInXML:          paymentMeans.Eval("count(ram:PayeePartyCreditorFinancialAccount/ram:IBANID)").Int() > 0,
			hasPayeeProprietaryIDInXML: paymentMeans.Eval("count(ram:PayeePartyCreditorFinancialAccount/ram:ProprietaryID)").Int() > 0,
			hasPaymentCardInXML:        paymentMeans.Eval("count(ram:ApplicableTradeSettlementFinancialCard)").Int() > 0,
			hasPayerAccountIDInXML:     paymentMeans.Eval("count(ram:PayerPartyDebtorFinancialAccount/ram:IBANID)").Int() > 0,
			hasPayeeInstitutionInXML:   paymentMeans.Eval("count(ram:PayeeSpecifiedCreditorFinancialInstitution)").Int() > 0,
			hasPayerInstitutionInXML:   paymentMeans.Eval("count(ram:PayerSpecifiedDebtorFinancialInstitution)").Int() > 0,
		}
		inv.PaymentMeans = append(inv.PaymentMeans, thisPaymentMeans)
	}

	for allowanceCharge := range applicableHeaderTradeSettlement.Each("ram:SpecifiedTradeAllowanceCharge") {
		inv.checkContext()
		basisAmount, err := getDecimal(allowanceCharge, "ram:BasisAmount")
		if err != nil {
			return err
		}
		actualAmount, err := getDecimal(allowanceCharge, "ram:ActualAmount")
		if err != nil {
			return err
		}
		calculationPercent, err := getDecimal(allowanceCharge, "ram:CalculationPercent")
		if err != nil {
			return err
		}
		categoryTaxRate, err := getDecimal(allowanceCharge, "ram:CategoryTradeTax/ram:RateApplicablePercent")
		if err != nil {
			return err
		}

		allowanceCharge := AllowanceCharge{
			ChargeIndicator:                       allowanceCharge.Eval("string(ram:ChargeIndicator/udt:Indicator) = 'true'").Bool(),
			BasisAmount:                           basisAmount,
			ActualAmount:                          actualAmount,
			CalculationPercent:                    calculationPercent,
			ReasonCode:                            allowanceCharge.Eval("ram:ReasonCode").String(),
			Reason:                                allowanceCharge.Eval("ram:Reason").String(),
			CategoryTradeTaxType:                  allowanceCharge.Eval("ram:CategoryTradeTax/ram:TypeCode").String(),
			CategoryTradeTaxCategoryCode:          allowanceCharge.Eval("ram:CategoryTradeTax/ram:CategoryCode").String(),
			CategoryTradeTaxRateApplicablePercent: categoryTaxRate,
			hasActualAmountInXML:                  allowanceCharge.Eval("count(ram:ActualAmount)").Int() > 0,
			hasBasisAmountInXML:                   allowanceCharge.Eval("count(ram:BasisAmount)").Int() > 0,
			hasPercentInXML:                       allowanceCharge.Eval("count(ram:CalculationPercent)").Int() > 0,
			hasIndicatorInXML:                     allowanceCharge.Eval("count(ram:ChargeIndicator/udt:Indicator)").Int() > 0,
			indicatorValidXML:                     isXMLBoolean(allowanceCharge.Eval("ram:ChargeIndicator/udt:Indicator").String()),
		}
		inv.SpecifiedTradeAllowanceCharge = append(inv.SpecifiedTradeAllowanceCharge, allowanceCharge)
	}

	// Parse SpecifiedLogisticsServiceCharge and convert to document-level charges
	// Per EN 16931, logistics service charges are document-level charges (BT-99)
	for logisticsCharge := range applicableHeaderTradeSettlement.Each("ram:SpecifiedLogisticsServiceCharge") {
		inv.checkContext()
		appliedAmount, err := getDecimal(logisticsCharge, "ram:AppliedAmount")
		if err != nil {
			return err
		}
		categoryTaxRate, err := getDecimal(logisticsCharge, "ram:AppliedTradeTax/ram:RateApplicablePercent")
		if err != nil {
			return err
		}

		charge := AllowanceCharge{
			ChargeIndicator:                       true, // Logistics charges are always charges, not allowances
			ActualAmount:                          appliedAmount,
			hasActualAmountInXML:                  logisticsCharge.Eval("count(ram:AppliedAmount)").Int() > 0,
			Reason:                                logisticsCharge.Eval("ram:Description").String(),
			CategoryTradeTaxType:                  logisticsCharge.Eval("ram:AppliedTradeTax/ram:TypeCode").String(),
			CategoryTradeTaxCategoryCode:          logisticsCharge.Eval("ram:AppliedTradeTax/ram:CategoryCode").String(),
			CategoryTradeTaxRateApplicablePercent: categoryTaxRate,
		}
		inv.SpecifiedTradeAllowanceCharge = append(inv.SpecifiedTradeAllowanceCharge, charge)
	}

	// BR-CO-19: Track BG-14 (INVOICING PERIOD) presence to validate later
	inv.hasBillingPeriodInXML = applicableHeaderTradeSettlement.Eval("count(ram:BillingSpecifiedPeriod)").Int() > 0
	inv.BillingSpecifiedPeriodStart, err = parseCIITime(applicableHeaderTradeSettlement, "ram:BillingSpecifiedPeriod/ram:StartDateTime/udt:DateTimeString")
	if err != nil {
		return fmt.Errorf("invalid billing period start date: %w", err)
	}
	inv.BillingSpecifiedPeriodEnd, err = parseCIITime(applicableHeaderTradeSettlement, "ram:BillingSpecifiedPeriod/ram:EndDateTime/udt:DateTimeString")
	if err != nil {
		return fmt.Errorf("invalid billing period end date: %w", err)
	}

	// ram:SpecifiedTradePaymentTerms
	inv.ciiPaymentTermsCount = applicableHeaderTradeSettlement.Eval("count(ram:SpecifiedTradePaymentTerms)").Int()
	inv.ciiPaymentDescriptionCount = applicableHeaderTradeSettlement.Eval("count(ram:SpecifiedTradePaymentTerms/ram:Description)").Int()
	for paymentTerm := range applicableHeaderTradeSettlement.Each("ram:SpecifiedTradePaymentTerms") {
		inv.checkContext()
		spt := SpecifiedTradePaymentTerms{}
		spt.Description = paymentTerm.Eval("ram:Description").String()
		spt.DueDate, err = parseCIITime(paymentTerm, "ram:DueDateDateTime/udt:DateTimeString")
		if err != nil {
			return err
		}

		spt.DirectDebitMandateID = paymentTerm.Eval("ram:DirectDebitMandateID").String()
		inv.SpecifiedTradePaymentTerms = append(inv.SpecifiedTradePaymentTerms, spt)
	}

	inv.ciiTaxPointDateMax = applicableHeaderTradeSettlement.Eval("count(ram:ApplicableTradeTax/ram:TaxPointDate)").Int()
	for att := range applicableHeaderTradeSettlement.Each("ram:ApplicableTradeTax") {
		inv.checkContext()
		tradeTax := TradeTax{}
		tradeTax.CalculatedAmount, err = getDecimal(att, "ram:CalculatedAmount")
		if err != nil {
			return err
		}
		tradeTax.BasisAmount, err = getDecimal(att, "ram:BasisAmount")
		if err != nil {
			return err
		}
		tradeTax.hasBasisAmountInXML = att.Eval("count(ram:BasisAmount)").Int() > 0
		tradeTax.hasPercentInXML = att.Eval("count(ram:RateApplicablePercent)").Int() > 0
		tradeTax.TypeCode = att.Eval("ram:TypeCode").String()
		tradeTax.ExemptionReason = att.Eval("ram:ExemptionReason").String()
		tradeTax.ExemptionReasonCode = att.Eval("ram:ExemptionReasonCode").String()
		tradeTax.CategoryCode = att.Eval("ram:CategoryCode").String()
		tradeTax.Percent, err = getDecimal(att, "ram:RateApplicablePercent") // BT-119
		if err != nil {
			return err
		}
		inv.TradeTaxes = append(inv.TradeTaxes, tradeTax)
	}

	summation := applicableHeaderTradeSettlement.Eval("ram:SpecifiedTradeSettlementHeaderMonetarySummation")

	// BR-12 through BR-15: Track XML element presence to validate later
	// This allows validation to distinguish between missing elements and zero values
	inv.hasLineTotalInXML = summation.Eval("count(ram:LineTotalAmount)").Int() > 0
	inv.hasTaxBasisTotalInXML = summation.Eval("count(ram:TaxBasisTotalAmount)").Int() > 0
	inv.hasGrandTotalInXML = summation.Eval("count(ram:GrandTotalAmount)").Int() > 0
	inv.hasDuePayableAmountInXML = summation.Eval("count(ram:DuePayableAmount)").Int() > 0

	inv.LineTotal, err = getDecimal(summation, "ram:LineTotalAmount")
	if err != nil {
		return err
	}
	inv.ChargeTotal, err = getDecimal(summation, "ram:ChargeTotalAmount")
	if err != nil {
		return err
	}
	inv.AllowanceTotal, err = getDecimal(summation, "ram:AllowanceTotalAmount")
	if err != nil {
		return err
	}
	inv.TaxBasisTotal, err = getDecimal(summation, "ram:TaxBasisTotalAmount")
	if err != nil {
		return err
	}

	// BT-110 and BT-111: Parse TaxTotalAmount by matching currencyID (not position)
	// EN 16931 specifies which currency each total must be in, regardless of XML order
	taxTotalIndex := 0
	for taxTotal := range summation.Each("ram:TaxTotalAmount") {
		inv.checkContext()
		currency := taxTotal.Eval("@currencyID").String()
		amount, err := getDecimal(taxTotal, ".")
		if err != nil {
			return fmt.Errorf("invalid TaxTotalAmount with currency %s: %w", currency, err)
		}
		inv.taxTotalsXML = append(inv.taxTotalsXML, taxTotalXML{currency: currency, amount: amount})

		// The CII semantic mapping applies BT-110 rules to the first
		// TaxTotalAmount regardless of currency, while a matching BT-6 also
		// makes that element BT-111. One element can therefore satisfy both.
		if taxTotalIndex == 0 {
			inv.TaxTotalCurrency = currency
			inv.TaxTotal = amount
		}
		if inv.TaxCurrencyCode != "" && inv.TaxCurrencyCode != inv.InvoiceCurrencyCode && currency == inv.TaxCurrencyCode {
			inv.TaxTotalAccountingCurrency = currency
			inv.TaxTotalAccounting = amount
			inv.hasTaxTotalAccountingXML = true
		} else if taxTotalIndex > 0 && currency != inv.InvoiceCurrencyCode {
			inv.unexpectedTaxCurrencies = append(inv.unexpectedTaxCurrencies, currency)
		}
		taxTotalIndex++
	}

	inv.GrandTotal, err = getDecimal(summation, "ram:GrandTotalAmount")
	if err != nil {
		return err
	}
	inv.TotalPrepaid, err = getDecimal(summation, "ram:TotalPrepaidAmount")
	if err != nil {
		return err
	}
	inv.RoundingAmount, err = getDecimal(summation, "ram:RoundingAmount")
	if err != nil {
		return err
	}
	inv.DuePayableAmount, err = getDecimal(summation, "ram:DuePayableAmount")
	if err != nil {
		return err
	}

	// BG-3
	for refdoc := range applicableHeaderTradeSettlement.Each("ram:InvoiceReferencedDocument") {
		inv.checkContext()
		refDoc := ReferencedDocument{}

		refDoc.Date, err = parseCIITime(refdoc, "ram:FormattedIssueDateTime/qdt:DateTimeString")
		if err != nil {
			return err
		}

		refDoc.ID = refdoc.Eval("ram:IssuerAssignedID").String()
		inv.InvoiceReferencedDocument = append(inv.InvoiceReferencedDocument, refDoc)
	}

	// BT-19: Buyer accounting reference
	inv.ReceivableSpecifiedTradeAccountingAccount = applicableHeaderTradeSettlement.Eval("ram:ReceivableSpecifiedTradeAccountingAccount/ram:ID").String()

	return nil
}

func parseSpecifiedLineTradeAgreement(specifiedLineTradeAgreement *cxpath.Context, invoiceLine *InvoiceLine) error {
	var err error

	// BT-132: Referenced purchase order line reference
	invoiceLine.BuyerOrderReferencedDocument = specifiedLineTradeAgreement.Eval("ram:BuyerOrderReferencedDocument/ram:LineID").String()

	// BR-26: Track XML element presence to validate later
	invoiceLine.hasNetPriceInXML = specifiedLineTradeAgreement.Eval("count(ram:NetPriceProductTradePrice/ram:ChargeAmount)").Int() > 0
	invoiceLine.NetPrice, err = getDecimal(specifiedLineTradeAgreement, "ram:NetPriceProductTradePrice/ram:ChargeAmount")
	if err != nil {
		return err
	}
	// BT-149: Item price base quantity with unit code (from NetPrice)
	invoiceLine.BasisQuantity, err = getDecimal(specifiedLineTradeAgreement, "ram:NetPriceProductTradePrice/ram:BasisQuantity")
	if err != nil {
		return err
	}
	invoiceLine.BasisQuantityUnit = specifiedLineTradeAgreement.Eval("ram:NetPriceProductTradePrice/ram:BasisQuantity/@unitCode").String()
	invoiceLine.hasNetBasisQuantityInXML = specifiedLineTradeAgreement.Eval("count(ram:NetPriceProductTradePrice/ram:BasisQuantity)").Int() > 0
	invoiceLine.hasGrossBasisQuantityInXML = specifiedLineTradeAgreement.Eval("count(ram:GrossPriceProductTradePrice/ram:BasisQuantity)").Int() > 0
	invoiceLine.grossBasisQuantity, err = getDecimal(specifiedLineTradeAgreement, "ram:GrossPriceProductTradePrice/ram:BasisQuantity")
	if err != nil {
		return err
	}
	invoiceLine.grossBasisQuantityUnit = specifiedLineTradeAgreement.Eval("ram:GrossPriceProductTradePrice/ram:BasisQuantity/@unitCode").String()

	invoiceLine.GrossPrice, err = getDecimal(specifiedLineTradeAgreement, "ram:GrossPriceProductTradePrice/ram:ChargeAmount")
	if err != nil {
		return err
	}
	invoiceLine.hasGrossPriceInXML = specifiedLineTradeAgreement.Eval("count(ram:GrossPriceProductTradePrice/ram:ChargeAmount)").Int() > 0
	// ZUGFeRD extended has unbound BT-147
	for allowanceCharge := range specifiedLineTradeAgreement.Each("ram:GrossPriceProductTradePrice/ram:AppliedTradeAllowanceCharge") {
		basisAmount, err := getDecimal(allowanceCharge, "ram:BasisAmount")
		if err != nil {
			return err
		}
		actualAmount, err := getDecimal(allowanceCharge, "ram:ActualAmount")
		if err != nil {
			return err
		}
		calculationPercent, err := getDecimal(allowanceCharge, "ram:CalculationPercent")
		if err != nil {
			return err
		}
		categoryTaxRate, err := getDecimal(allowanceCharge, "ram:CategoryTradeTax/ram:RateApplicablePercent")
		if err != nil {
			return err
		}

		allowanceCharge := AllowanceCharge{
			ChargeIndicator:                       allowanceCharge.Eval("string(ram:ChargeIndicator/udt:Indicator) = 'true'").Bool(),
			BasisAmount:                           basisAmount,
			ActualAmount:                          actualAmount,
			CalculationPercent:                    calculationPercent,
			ReasonCode:                            allowanceCharge.Eval("ram:ReasonCode").String(),
			Reason:                                allowanceCharge.Eval("ram:Reason").String(),
			CategoryTradeTaxType:                  allowanceCharge.Eval("ram:CategoryTradeTax/ram:TypeCode").String(),
			CategoryTradeTaxCategoryCode:          allowanceCharge.Eval("ram:CategoryTradeTax/ram:CategoryCode").String(),
			CategoryTradeTaxRateApplicablePercent: categoryTaxRate,
			hasActualAmountInXML:                  allowanceCharge.Eval("count(ram:ActualAmount)").Int() > 0,
			hasBasisAmountInXML:                   allowanceCharge.Eval("count(ram:BasisAmount)").Int() > 0,
			hasPercentInXML:                       allowanceCharge.Eval("count(ram:CalculationPercent)").Int() > 0,
			hasIndicatorInXML:                     allowanceCharge.Eval("count(ram:ChargeIndicator/udt:Indicator)").Int() > 0,
			indicatorValidXML:                     isXMLBoolean(allowanceCharge.Eval("ram:ChargeIndicator/udt:Indicator").String()),
		}
		invoiceLine.AppliedTradeAllowanceCharge = append(invoiceLine.AppliedTradeAllowanceCharge, allowanceCharge)
	}
	return nil
}

func isXMLBoolean(value string) bool {
	return value == "true" || value == "false"
}

func parseSpecifiedTradeProduct(specifiedTradeProduct *cxpath.Context, invoiceLine *InvoiceLine) {
	invoiceLine.GlobalID = specifiedTradeProduct.Eval("ram:GlobalID").String()
	invoiceLine.GlobalIDType = specifiedTradeProduct.Eval("ram:GlobalID/@schemeID").String()
	invoiceLine.ArticleNumber = specifiedTradeProduct.Eval("ram:SellerAssignedID").String()
	invoiceLine.ArticleNumberBuyer = specifiedTradeProduct.Eval("ram:BuyerAssignedID").String()
	invoiceLine.ItemName = specifiedTradeProduct.Eval("ram:Name").String()
	invoiceLine.Description = specifiedTradeProduct.Eval("ram:Description").String()

	for itm := range specifiedTradeProduct.Each("ram:ApplicableProductCharacteristic") {
		ch := Characteristic{
			Description: itm.Eval("ram:Description").String(),
			Value:       itm.Eval("ram:Value").String(),
		}
		invoiceLine.Characteristics = append(invoiceLine.Characteristics, ch)
	}
	for itm := range specifiedTradeProduct.Each("ram:DesignatedProductClassification") {
		ch := Classification{
			ClassCode:      itm.Eval("ram:ClassCode").String(),
			ListID:         itm.Eval("ram:ClassCode/@listID").String(),
			ListVersionID:  itm.Eval("ram:ClassCode/@listVersionID").String(),
			hasListIDInXML: itm.Eval("count(ram:ClassCode/@listID)").Int() > 0,
		}
		invoiceLine.ProductClassification = append(invoiceLine.ProductClassification, ch)
	}

	invoiceLine.OriginTradeCountry = specifiedTradeProduct.Eval("ram:OriginTradeCountry/ram:ID").String()
}
