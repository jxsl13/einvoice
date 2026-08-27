package einvoice

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"github.com/speedata/cxpath"
)

// UBL 2.1 namespace URNs for Invoice and CreditNote documents
const (
	nsUBLInvoice    = "urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"
	nsUBLCreditNote = "urn:oasis:names:specification:ubl:schema:xsd:CreditNote-2"
	nsUBLCAC        = "urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2"
	nsUBLCBC        = "urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2"
)

// parseTimeUBL parses the XML Schema date lexical forms used in UBL. An
// xs:date may include Z or a numeric timezone even though it has no time part.
func parseTimeUBL(ctx *cxpath.Context, path string) (time.Time, error) {
	timestring := ctx.Eval(path).String()
	if timestring == "" {
		return time.Time{}, nil
	}

	for _, layout := range []string{"2006-01-02", "2006-01-02Z07:00"} {
		parsedDate, err := time.Parse(layout, timestring)
		if err == nil {
			return parsedDate, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q at %s", timestring, path)
}

// parseUBL parses a UBL 2.1 Invoice or CreditNote document into an Invoice struct.
// Both document types are mapped to the same Invoice struct, differentiated by InvoiceTypeCode.
func parseUBL(operationCtx context.Context, ctx *cxpath.Context) (*Invoice, error) {
	inv := &Invoice{SchemaType: UBL, operationContext: operationCtx}
	inv.checkContext()

	// Setup UBL namespaces
	ctx.SetNamespace("inv", nsUBLInvoice)
	ctx.SetNamespace("cn", nsUBLCreditNote)
	ctx.SetNamespace("cac", nsUBLCAC)
	ctx.SetNamespace("cbc", nsUBLCBC)

	// Get root element after namespace setup
	root := ctx.Root()
	inv.peppolEmptyElementCount = root.Eval("count(//*[not(*) and not(normalize-space())])").Int()

	// Determine document type (Invoice vs CreditNote)
	localName := root.Eval("local-name()").String()

	// Set namespace prefix based on document type
	prefix := "inv:"
	if localName == "CreditNote" {
		prefix = "cn:"
		inv.isUBLCreditNoteXML = true
	}

	// Parse all components
	if err := parseUBLHeader(root, inv, prefix); err != nil {
		return nil, fmt.Errorf("parse UBL header: %w", err)
	}
	inv.checkContext()

	if err := parseUBLParties(root, inv, prefix); err != nil {
		return nil, fmt.Errorf("parse UBL parties: %w", err)
	}
	inv.checkContext()

	if err := parseUBLAllowanceCharge(root, inv, prefix); err != nil {
		return nil, fmt.Errorf("parse UBL allowances/charges: %w", err)
	}
	inv.checkContext()

	if err := parseUBLTaxTotal(root, inv, prefix); err != nil {
		return nil, fmt.Errorf("parse UBL tax total: %w", err)
	}
	inv.checkContext()

	if err := parseUBLMonetarySummation(root, inv, prefix); err != nil {
		return nil, fmt.Errorf("parse UBL monetary summation: %w", err)
	}
	inv.checkContext()

	if err := parseUBLPaymentMeans(root, inv, prefix); err != nil {
		return nil, fmt.Errorf("parse UBL payment means: %w", err)
	}
	inv.checkContext()

	if err := parseUBLPaymentTerms(root, inv, prefix); err != nil {
		return nil, fmt.Errorf("parse UBL payment terms: %w", err)
	}
	inv.checkContext()

	if err := parseUBLLines(root, inv, prefix); err != nil {
		return nil, fmt.Errorf("parse UBL lines: %w", err)
	}
	inv.checkContext()

	return inv, nil
}

// parseUBLHeader parses the document header elements (BT-1 to BT-24, BG-1, BG-3, BG-14, BG-24).
func parseUBLHeader(root *cxpath.Context, inv *Invoice, prefix string) error {
	// BT-24: CustomizationID (Specification identifier)
	inv.GuidelineSpecifiedDocumentContextParameter = root.Eval("cbc:CustomizationID").String()

	// BT-23: ProfileID (Business process type)
	inv.BPSpecifiedDocumentContextParameter = root.Eval("cbc:ProfileID").String()

	// BT-1: Invoice number
	inv.InvoiceNumber = root.Eval("cbc:ID").String()

	// BT-3: Invoice type code
	inv.InvoiceTypeCode = CodeDocument(root.Eval("cbc:InvoiceTypeCode").Int())
	if inv.InvoiceTypeCode == 0 {
		// Try CreditNoteTypeCode for credit notes
		inv.InvoiceTypeCode = CodeDocument(root.Eval("cbc:CreditNoteTypeCode").Int())
	}
	// If still 0 and this is a CreditNote document, default to 381
	if inv.InvoiceTypeCode == 0 && prefix == "cn:" {
		inv.InvoiceTypeCode = 381
	}

	// BT-2: Invoice date
	var err error
	inv.InvoiceDate, err = parseTimeUBL(root, "cbc:IssueDate")
	if err != nil {
		return err
	}

	// BT-72: Actual delivery date (optional, in cac:Delivery)
	inv.OccurrenceDateTime, err = parseTimeUBL(root, "cac:Delivery/cbc:ActualDeliveryDate")
	if err != nil {
		return fmt.Errorf("invalid occurrence date time: %w", err)
	}

	// BT-5: Invoice currency
	inv.InvoiceCurrencyCode = root.Eval("cbc:DocumentCurrencyCode").String()

	// BT-6: Tax currency (optional)
	inv.TaxCurrencyCode = root.Eval("cbc:TaxCurrencyCode").String()

	// BT-10: Buyer reference (optional)
	inv.BuyerReference = root.Eval("cbc:BuyerReference").String()

	// BT-19: Accounting cost (Buyer accounting reference)
	inv.ReceivableSpecifiedTradeAccountingAccount = root.Eval("cbc:AccountingCost").String()

	// BT-13: Purchase order reference
	inv.BuyerOrderReferencedDocument = root.Eval("cac:OrderReference/cbc:ID").String()

	// BT-14: Sales order reference
	inv.SellerOrderReferencedDocument = root.Eval("cac:OrderReference/cbc:SalesOrderID").String()

	// BT-12: Contract document reference
	inv.ContractReferencedDocument = root.Eval("cac:ContractDocumentReference/cbc:ID").String()

	// BT-11: Project reference
	inv.SpecifiedProcuringProjectID = root.Eval("cac:ProjectReference/cbc:ID").String()

	// BT-16: Despatch advice reference
	inv.DespatchAdviceReferencedDocument = root.Eval("cac:DespatchDocumentReference/cbc:ID").String()

	// BT-15: Receiving advice reference
	inv.ReceivingAdviceReferencedDocument = root.Eval("cac:ReceiptDocumentReference/cbc:ID").String()

	// BG-1: Process notes
	noteCount := root.Eval("count(cbc:Note)").Int()
	if noteCount > 0 {
		inv.Notes = make([]Note, 0, noteCount)
		for note := range root.Each("cbc:Note") {
			inv.checkContext()
			inv.Notes = append(inv.Notes, Note{
				Text: note.String(),
				// UBL doesn't typically have subject codes in Note elements
			})
		}
	}

	// BG-3: Preceding invoice references
	refCount := root.Eval("count(cac:BillingReference/cac:InvoiceDocumentReference)").Int()
	if refCount > 0 {
		inv.InvoiceReferencedDocument = make([]ReferencedDocument, 0, refCount)
		for ref := range root.Each("cac:BillingReference/cac:InvoiceDocumentReference") {
			inv.checkContext()
			refDoc := ReferencedDocument{
				ID: ref.Eval("cbc:ID").String(),
			}

			refDoc.Date, err = parseTimeUBL(ref, "cbc:IssueDate")
			if err != nil {
				return fmt.Errorf("invalid referenced document date: %w", err)
			}

			inv.InvoiceReferencedDocument = append(inv.InvoiceReferencedDocument, refDoc)
		}
	}

	// BG-14: Invoice period (document level)
	// BR-CO-19: Track BG-14 (INVOICING PERIOD) presence to validate later
	if root.Eval("count(cac:InvoicePeriod)").Int() > 0 {
		inv.hasBillingPeriodInXML = true
		inv.BillingSpecifiedPeriodStart, err = parseTimeUBL(root, "cac:InvoicePeriod/cbc:StartDate")
		if err != nil {
			return fmt.Errorf("invalid billing period start date: %w", err)
		}
		inv.BillingSpecifiedPeriodEnd, err = parseTimeUBL(root, "cac:InvoicePeriod/cbc:EndDate")
		if err != nil {
			return fmt.Errorf("invalid billing period end date: %w", err)
		}
	}

	// BG-24: Additional supporting documents
	docCount := root.Eval("count(cac:AdditionalDocumentReference)").Int()
	if docCount > 0 {
		inv.AdditionalReferencedDocument = make([]Document, 0, docCount)
		for doc := range root.Each("cac:AdditionalDocumentReference") {
			inv.checkContext()
			addDoc := Document{
				IssuerAssignedID: doc.Eval("cbc:ID").String(),
				TypeCode:         doc.Eval("cbc:DocumentTypeCode").String(),
				Name:             doc.Eval("cbc:DocumentDescription").String(),
				URIID:            doc.Eval("cac:Attachment/cac:ExternalReference/cbc:URI").String(),
			}

			// Handle embedded binary object
			binaryData := doc.Eval("cac:Attachment/cbc:EmbeddedDocumentBinaryObject").String()
			if binaryData != "" {
				addDoc.AttachmentMimeCode = doc.Eval("cac:Attachment/cbc:EmbeddedDocumentBinaryObject/@mimeCode").String()
				addDoc.AttachmentFilename = doc.Eval("cac:Attachment/cbc:EmbeddedDocumentBinaryObject/@filename").String()

				// Decode base64-encoded attachment data
				data, err := base64.StdEncoding.DecodeString(binaryData)
				if err != nil {
					return fmt.Errorf("cannot decode attachment: %w", err)
				}
				addDoc.AttachmentBinaryObject = data
			}

			inv.AdditionalReferencedDocument = append(inv.AdditionalReferencedDocument, addDoc)
		}
	}

	return nil
}

// parseUBLParties parses all party elements (BG-4, BG-7, BG-10, BG-11, BG-13).
func parseUBLParties(root *cxpath.Context, inv *Invoice, prefix string) error {
	// BG-4: Seller (AccountingSupplierParty)
	inv.Seller = parseUBLParty(root.Eval("cac:AccountingSupplierParty/cac:Party"))

	// BG-7: Buyer (AccountingCustomerParty)
	inv.Buyer = parseUBLParty(root.Eval("cac:AccountingCustomerParty/cac:Party"))

	// BG-10: Payee (optional)
	if root.Eval("count(cac:PayeeParty)").Int() > 0 {
		payee := parseUBLParty(root.Eval("cac:PayeeParty"))
		inv.PayeeTradeParty = &payee
	}

	// BG-11: Seller tax representative (optional)
	if root.Eval("count(cac:TaxRepresentativeParty)").Int() > 0 {
		taxRep := parseUBLParty(root.Eval("cac:TaxRepresentativeParty"))
		inv.SellerTaxRepresentativeTradeParty = &taxRep
	}

	// BG-13: Delivery information (optional)
	if deliveryCtx := root.Eval("cac:Delivery"); deliveryCtx.Eval("count()").Int() > 0 {
		// Delivery party
		if deliveryPartyCtx := deliveryCtx.Eval("cac:DeliveryParty"); deliveryPartyCtx.Eval("count()").Int() > 0 {
			shipTo := parseUBLParty(deliveryPartyCtx)
			inv.ShipTo = &shipTo
		} else if locationCtx := deliveryCtx.Eval("cac:DeliveryLocation"); locationCtx.Eval("count()").Int() > 0 {
			// If no DeliveryParty, create one from DeliveryLocation address
			shipTo := Party{}
			if addrCtx := locationCtx.Eval("cac:Address"); addrCtx.Eval("count()").Int() > 0 {
				postalAddr := &PostalAddress{
					Line1:                  addrCtx.Eval("cbc:StreetName").String(),
					Line2:                  addrCtx.Eval("cbc:AdditionalStreetName").String(),
					Line3:                  addrCtx.Eval("cac:AddressLine/cbc:Line").String(),
					City:                   addrCtx.Eval("cbc:CityName").String(),
					PostcodeCode:           addrCtx.Eval("cbc:PostalZone").String(),
					CountrySubDivisionName: addrCtx.Eval("cbc:CountrySubentity").String(),
					CountryID:              addrCtx.Eval("cac:Country/cbc:IdentificationCode").String(),
				}
				shipTo.PostalAddress = postalAddr
			}
			inv.ShipTo = &shipTo
		}
	}

	return nil
}

// parseUBLParty parses a single party (reusable for Seller, Buyer, Payee, etc.).
// Takes a context already positioned at the party element.
func parseUBLParty(partyCtx *cxpath.Context) Party {
	party := Party{}

	// Electronic address (BT-34, BT-49, BT-98)
	party.URIUniversalCommunication = partyCtx.Eval("cbc:EndpointID").String()
	party.URIUniversalCommunicationScheme = partyCtx.Eval("cbc:EndpointID/@schemeID").String()

	// Party identification (BT-29, BT-46, BT-60, BT-71)
	idCount := partyCtx.Eval("count(cac:PartyIdentification)").Int()
	if idCount > 0 {
		party.GlobalID = make([]GlobalID, 0, idCount)
		party.ID = make([]string, 0, idCount)
		for id := range partyCtx.Each("cac:PartyIdentification") {
			idValue := id.Eval("cbc:ID").String()
			idScheme := id.Eval("cbc:ID/@schemeID").String()

			if idScheme != "" {
				party.GlobalID = append(party.GlobalID, GlobalID{
					ID:     idValue,
					Scheme: idScheme,
				})
			} else {
				party.ID = append(party.ID, idValue)
			}
		}
	}

	// Party name (BT-27, BT-44, BT-59, BT-70)
	party.Name = partyCtx.Eval("cac:PartyName/cbc:Name").String()
	if party.Name == "" {
		// Fallback to PartyLegalEntity/RegistrationName
		party.Name = partyCtx.Eval("cac:PartyLegalEntity/cbc:RegistrationName").String()
	}

	// Postal address (BG-5, BG-8, BG-12, BG-15)
	if partyCtx.Eval("count(cac:PostalAddress)").Int() > 0 {
		postalAddr := &PostalAddress{
			Line1:                  partyCtx.Eval("cac:PostalAddress/cbc:StreetName").String(),
			Line2:                  partyCtx.Eval("cac:PostalAddress/cbc:AdditionalStreetName").String(),
			Line3:                  partyCtx.Eval("cac:PostalAddress/cac:AddressLine/cbc:Line").String(),
			City:                   partyCtx.Eval("cac:PostalAddress/cbc:CityName").String(),
			PostcodeCode:           partyCtx.Eval("cac:PostalAddress/cbc:PostalZone").String(),
			CountrySubDivisionName: partyCtx.Eval("cac:PostalAddress/cbc:CountrySubentity").String(),
			CountryID:              partyCtx.Eval("cac:PostalAddress/cac:Country/cbc:IdentificationCode").String(),
		}
		party.PostalAddress = postalAddr
	}

	// Legal organization (BT-30, BT-47, BT-61)
	if partyCtx.Eval("count(cac:PartyLegalEntity)").Int() > 0 {
		legalOrg := &SpecifiedLegalOrganization{
			ID:                  partyCtx.Eval("cac:PartyLegalEntity/cbc:CompanyID").String(),
			Scheme:              partyCtx.Eval("cac:PartyLegalEntity/cbc:CompanyID/@schemeID").String(),
			TradingBusinessName: partyCtx.Eval("cac:PartyLegalEntity/cbc:RegistrationName").String(),
		}
		party.SpecifiedLegalOrganization = legalOrg
	}

	// Tax registration (BT-31, BT-32, BT-48, BT-63)
	for taxScheme := range partyCtx.Each("cac:PartyTaxScheme") {
		taxID := taxScheme.Eval("cbc:CompanyID").String()
		scheme := taxScheme.Eval("cac:TaxScheme/cbc:ID").String()
		if scheme != "" && taxScheme.Eval("count(cbc:CompanyID)").Int() == 0 {
			party.ublTaxSchemeMissingCompanyID = true
		}

		switch scheme {
		case "VAT":
			party.VATaxRegistration = taxID
		case "FC":
			party.FCTaxRegistration = taxID
		}
	}

	// Contact (BG-6, BG-9)
	contactCount := partyCtx.Eval("count(cac:Contact)").Int()
	if contactCount > 0 {
		party.DefinedTradeContact = make([]DefinedTradeContact, 0, contactCount)
		for contact := range partyCtx.Each("cac:Contact") {
			dtc := DefinedTradeContact{
				PersonName:     contact.Eval("cbc:Name").String(),
				DepartmentName: contact.Eval("cbc:Department").String(),
				PhoneNumber:    contact.Eval("cbc:Telephone").String(),
				EMail:          contact.Eval("cbc:ElectronicMail").String(),
			}
			party.DefinedTradeContact = append(party.DefinedTradeContact, dtc)
		}
	}

	return party
}

// parseUBLAllowanceCharge parses document-level allowances and charges (BG-20, BG-21).
func parseUBLAllowanceCharge(root *cxpath.Context, inv *Invoice, prefix string) error {
	acCount := root.Eval("count(cac:AllowanceCharge)").Int()
	if acCount > 0 {
		inv.SpecifiedTradeAllowanceCharge = make([]AllowanceCharge, 0, acCount)
		for ac := range root.Each("cac:AllowanceCharge") {
			inv.checkContext()
			chargeIndicator := ac.Eval("string(cbc:ChargeIndicator) = 'true'").Bool()

			basisAmount, err := getDecimal(ac, "cbc:BaseAmount")
			if err != nil {
				return err
			}

			actualAmount, err := getDecimal(ac, "cbc:Amount")
			if err != nil {
				return err
			}

			calculationPercent, err := getDecimal(ac, "cbc:MultiplierFactorNumeric")
			if err != nil {
				return err
			}

			categoryTaxRate, err := getDecimal(ac, "cac:TaxCategory/cbc:Percent")
			if err != nil {
				return err
			}

			allowanceCharge := AllowanceCharge{
				ChargeIndicator:                       chargeIndicator,
				BasisAmount:                           basisAmount,
				ActualAmount:                          actualAmount,
				CalculationPercent:                    calculationPercent,
				ReasonCode:                            ac.Eval("cbc:AllowanceChargeReasonCode").String(),
				Reason:                                ac.Eval("cbc:AllowanceChargeReason").String(),
				CategoryTradeTaxType:                  ac.Eval("cac:TaxCategory/cac:TaxScheme/cbc:ID").String(),
				CategoryTradeTaxCategoryCode:          ac.Eval("cac:TaxCategory/cbc:ID").String(),
				CategoryTradeTaxRateApplicablePercent: categoryTaxRate,
				hasActualAmountInXML:                  ac.Eval("count(cbc:Amount)").Int() > 0,
				hasBasisAmountInXML:                   ac.Eval("count(cbc:BaseAmount)").Int() > 0,
				hasPercentInXML:                       ac.Eval("count(cbc:MultiplierFactorNumeric)").Int() > 0,
				hasIndicatorInXML:                     ac.Eval("count(cbc:ChargeIndicator)").Int() > 0,
				indicatorValidXML:                     isXMLBoolean(ac.Eval("cbc:ChargeIndicator").String()),
			}

			inv.SpecifiedTradeAllowanceCharge = append(inv.SpecifiedTradeAllowanceCharge, allowanceCharge)
		}
	}

	return nil
}

// parseUBLTaxTotal parses the tax breakdown (BG-23).
func parseUBLTaxTotal(root *cxpath.Context, inv *Invoice, prefix string) error {
	var err error
	invoiceTaxTotalSet := false

	// BT-110 and BT-111: Parse TaxTotal by matching currencyID (not position)
	// EN 16931 specifies which currency each total must be in, regardless of XML order
	for taxTotal := range root.Each("cac:TaxTotal") {
		inv.checkContext()
		currency := taxTotal.Eval("cbc:TaxAmount/@currencyID").String()
		if currency == "" {
			currency = inv.InvoiceCurrencyCode // Default if missing
		}

		amount, err := getDecimal(taxTotal, "cbc:TaxAmount")
		if err != nil {
			return fmt.Errorf("invalid TaxAmount with currency %s: %w", currency, err)
		}
		subtotalSum := decimal.Zero
		for subtotal := range taxTotal.Each("cac:TaxSubtotal") {
			subtotalAmount, subtotalErr := getDecimal(subtotal, "cbc:TaxAmount")
			if subtotalErr != nil {
				return subtotalErr
			}
			subtotalSum = subtotalSum.Add(subtotalAmount)
		}
		hasTaxSubtotal := taxTotal.Eval("count(cac:TaxSubtotal)").Int() > 0
		inv.taxTotalsXML = append(inv.taxTotalsXML, taxTotalXML{
			currency:       currency,
			amount:         amount,
			hasTaxSubtotal: hasTaxSubtotal,
			subtotalSum:    subtotalSum,
		})

		// BT-110 is the tax total that owns the VAT breakdown. Selecting by
		// structure mirrors the EN 16931 UBL binding and avoids treating an
		// accounting-only TaxTotal as the invoice-currency total.
		switch {
		case hasTaxSubtotal && !invoiceTaxTotalSet:
			inv.TaxTotalCurrency = currency
			inv.TaxTotal = amount
			invoiceTaxTotalSet = true
		case inv.TaxCurrencyCode != "" && currency == inv.TaxCurrencyCode:
			// BT-111: Tax total in accounting currency (must match BT-6)
			inv.TaxTotalAccountingCurrency = currency
			inv.TaxTotalAccounting = amount
			inv.hasTaxTotalAccountingXML = true
		default:
			// Track unexpected TaxTotal currencies for validation
			inv.unexpectedTaxCurrencies = append(inv.unexpectedTaxCurrencies, currency)
		}
	}

	// BG-23: VAT breakdown (TaxSubtotal elements)
	taxSubtotalCount := root.Eval("count(cac:TaxTotal/cac:TaxSubtotal)").Int()
	if taxSubtotalCount > 0 {
		inv.TradeTaxes = make([]TradeTax, 0, taxSubtotalCount)
		for subtotal := range root.Each("cac:TaxTotal/cac:TaxSubtotal") {
			inv.checkContext()
			tradeTax := TradeTax{}

			tradeTax.BasisAmount, err = getDecimal(subtotal, "cbc:TaxableAmount")
			if err != nil {
				return err
			}
			tradeTax.hasBasisAmountInXML = subtotal.Eval("count(cbc:TaxableAmount)").Int() > 0
			tradeTax.hasPercentInXML = subtotal.Eval("count(cac:TaxCategory/cbc:Percent)").Int() > 0

			tradeTax.CalculatedAmount, err = getDecimal(subtotal, "cbc:TaxAmount")
			if err != nil {
				return err
			}

			tradeTax.TypeCode = subtotal.Eval("cac:TaxCategory/cac:TaxScheme/cbc:ID").String()
			if tradeTax.TypeCode == "" {
				tradeTax.TypeCode = "VAT" // Default to VAT
			}

			tradeTax.CategoryCode = subtotal.Eval("cac:TaxCategory/cbc:ID").String()

			tradeTax.Percent, err = getDecimal(subtotal, "cac:TaxCategory/cbc:Percent")
			if err != nil {
				return err
			}

			tradeTax.ExemptionReason = subtotal.Eval("cac:TaxCategory/cbc:TaxExemptionReason").String()
			tradeTax.ExemptionReasonCode = subtotal.Eval("cac:TaxCategory/cbc:TaxExemptionReasonCode").String()

			inv.TradeTaxes = append(inv.TradeTaxes, tradeTax)
		}
	}

	return nil
}

// parseUBLMonetarySummation parses the monetary totals (BT-106 to BT-115).
func parseUBLMonetarySummation(root *cxpath.Context, inv *Invoice, prefix string) error {
	legalMonetaryTotal := root.Eval("cac:LegalMonetaryTotal")

	// Track XML element presence for BR-12 through BR-15 validation
	inv.hasLineTotalInXML = legalMonetaryTotal.Eval("count(cbc:LineExtensionAmount)").Int() > 0
	inv.hasTaxBasisTotalInXML = legalMonetaryTotal.Eval("count(cbc:TaxExclusiveAmount)").Int() > 0
	inv.hasGrandTotalInXML = legalMonetaryTotal.Eval("count(cbc:TaxInclusiveAmount)").Int() > 0
	inv.hasDuePayableAmountInXML = legalMonetaryTotal.Eval("count(cbc:PayableAmount)").Int() > 0

	var err error

	// BT-106: Sum of Invoice line net amount
	inv.LineTotal, err = getDecimal(legalMonetaryTotal, "cbc:LineExtensionAmount")
	if err != nil {
		return err
	}

	// BT-107: Sum of allowances on document level
	inv.AllowanceTotal, err = getDecimal(legalMonetaryTotal, "cbc:AllowanceTotalAmount")
	if err != nil {
		return err
	}

	// BT-108: Sum of charges on document level
	inv.ChargeTotal, err = getDecimal(legalMonetaryTotal, "cbc:ChargeTotalAmount")
	if err != nil {
		return err
	}

	// BT-109: Invoice total amount without VAT
	inv.TaxBasisTotal, err = getDecimal(legalMonetaryTotal, "cbc:TaxExclusiveAmount")
	if err != nil {
		return err
	}

	// BT-112: Invoice total amount with VAT
	inv.GrandTotal, err = getDecimal(legalMonetaryTotal, "cbc:TaxInclusiveAmount")
	if err != nil {
		return err
	}

	// BT-113: Paid amount
	inv.TotalPrepaid, err = getDecimal(legalMonetaryTotal, "cbc:PrepaidAmount")
	if err != nil {
		return err
	}

	// BT-114: Rounding amount
	inv.RoundingAmount, err = getDecimal(legalMonetaryTotal, "cbc:PayableRoundingAmount")
	if err != nil {
		return err
	}

	// BT-115: Amount due for payment
	inv.DuePayableAmount, err = getDecimal(legalMonetaryTotal, "cbc:PayableAmount")
	if err != nil {
		return err
	}

	return nil
}

// parseUBLPaymentMeans parses payment means elements (BG-16, BG-17, BG-18, BG-19).
func parseUBLPaymentMeans(root *cxpath.Context, inv *Invoice, prefix string) error {
	pmCount := root.Eval("count(cac:PaymentMeans)").Int()
	if pmCount > 0 {
		inv.PaymentMeans = make([]PaymentMeans, 0, pmCount)
		for pm := range root.Each("cac:PaymentMeans") {
			inv.checkContext()
			paymentMeans := PaymentMeans{
				TypeCode:    pm.Eval("cbc:PaymentMeansCode").Int(),
				Information: pm.Eval("cbc:InstructionNote").String(),
			}

			// BT-83: Remittance information
			inv.PaymentReference = pm.Eval("cbc:PaymentID").String()

			// BG-17: Credit transfer (IBAN/BIC)
			if pm.Eval("count(cac:PayeeFinancialAccount)").Int() > 0 {
				paymentMeans.PayeePartyCreditorFinancialAccountIBAN = pm.Eval("cac:PayeeFinancialAccount/cbc:ID").String()
				paymentMeans.PayeePartyCreditorFinancialAccountName = pm.Eval("cac:PayeeFinancialAccount/cbc:Name").String()
				paymentMeans.PayeePartyCreditorFinancialAccountProprietaryID = pm.Eval("cac:PayeeFinancialAccount/cac:ID").String()
				paymentMeans.PayeeSpecifiedCreditorFinancialInstitutionBIC = pm.Eval("cac:PayeeFinancialAccount/cac:FinancialInstitutionBranch/cbc:ID").String()
				// BR-61: Track XML element presence to validate later.
				// Per EN 16931 schematron, BR-61 test is "(ram:IBANID) or (ram:ProprietaryID)"
				// which checks for element PRESENCE, not value. An empty element <cbc:ID/>
				// satisfies the test because the element exists.
				paymentMeans.hasPayeeAccountInXML = true
				paymentMeans.hasPayeeIBANInXML = pm.Eval("count(cac:PayeeFinancialAccount/cbc:ID)").Int() > 0
				paymentMeans.hasPayeeProprietaryIDInXML = pm.Eval("count(cac:PayeeFinancialAccount/cac:ID)").Int() > 0
			}

			// BG-18: Payment card information
			if pm.Eval("count(cac:CardAccount)").Int() > 0 {
				paymentMeans.hasPaymentCardInXML = true
				paymentMeans.ApplicableTradeSettlementFinancialCardID = pm.Eval("cac:CardAccount/cbc:PrimaryAccountNumberID").String()
				paymentMeans.ApplicableTradeSettlementFinancialCardCardholderName = pm.Eval("cac:CardAccount/cbc:HolderName").String()
			}

			// BG-19: Direct debit
			if pm.Eval("count(cac:PaymentMandate)").Int() > 0 {
				paymentMeans.hasPaymentMandateInXML = true
				paymentMeans.mandateIDXML = pm.Eval("cac:PaymentMandate/cbc:ID").String()
				paymentMeans.hasPayerAccountIDInXML = pm.Eval("count(cac:PaymentMandate/cac:PayerFinancialAccount/cbc:ID)").Int() > 0
				paymentMeans.PayerPartyDebtorFinancialAccountIBAN = pm.Eval("cac:PaymentMandate/cac:PayerFinancialAccount/cbc:ID").String()
			}

			inv.PaymentMeans = append(inv.PaymentMeans, paymentMeans)
		}
	}

	return nil
}

// parseUBLPaymentTerms parses payment terms (BT-20, BT-9, BT-89).
func parseUBLPaymentTerms(root *cxpath.Context, inv *Invoice, prefix string) error {
	// BT-9: Payment due date at invoice level
	// In UBL, DueDate is at the root Invoice/CreditNote level, not inside PaymentTerms
	rootDueDate, err := parseTimeUBL(root, "cbc:DueDate")
	if err != nil {
		return err
	}

	ptCount := root.Eval("count(cac:PaymentTerms)").Int()
	if ptCount > 0 {
		inv.SpecifiedTradePaymentTerms = make([]SpecifiedTradePaymentTerms, 0, ptCount)
		for pt := range root.Each("cac:PaymentTerms") {
			inv.checkContext()
			paymentTerm := SpecifiedTradePaymentTerms{
				Description: pt.Eval("cbc:Note").String(),
			}

			// BT-9: Payment due date (prefer element-level DueDate if present)
			paymentTerm.DueDate, err = parseTimeUBL(pt, "cbc:PaymentDueDate")
			if err != nil {
				return err
			}
			// If not in PaymentTerms, use root-level DueDate
			if paymentTerm.DueDate.IsZero() && !rootDueDate.IsZero() {
				paymentTerm.DueDate = rootDueDate
			}

			// BT-89: Direct debit mandate identifier
			paymentTerm.DirectDebitMandateID = pt.Eval("cbc:PaymentMeansID").String()

			inv.SpecifiedTradePaymentTerms = append(inv.SpecifiedTradePaymentTerms, paymentTerm)
		}
	} else if !rootDueDate.IsZero() {
		// If there are no PaymentTerms elements but there's a root-level DueDate,
		// create a single PaymentTerms entry with just the DueDate
		inv.SpecifiedTradePaymentTerms = []SpecifiedTradePaymentTerms{
			{DueDate: rootDueDate},
		}
	}

	return nil
}

// parseUBLLines parses all invoice line items (BG-25).
func parseUBLLines(root *cxpath.Context, inv *Invoice, prefix string) error {
	// Determine line element and quantity element names based on document type
	lineElementName := "cac:InvoiceLine"
	quantityElementName := "cbc:InvoicedQuantity"
	if prefix == "cn:" {
		lineElementName = "cac:CreditNoteLine"
		quantityElementName = "cbc:CreditedQuantity"
	}

	lineCount := root.Eval("count(" + lineElementName + ")").Int()
	if lineCount > 0 {
		inv.InvoiceLines = make([]InvoiceLine, 0, lineCount)
	}

	for lineItem := range root.Each(lineElementName) {
		inv.checkContext()
		invoiceLine := InvoiceLine{}
		var err error

		// BT-126: Invoice line identifier
		invoiceLine.LineID = lineItem.Eval("cbc:ID").String()

		// BT-127: Invoice line note
		invoiceLine.Note = lineItem.Eval("cbc:Note").String()

		// BG-26: Invoice line period
		// BR-CO-20: Track BG-26 (INVOICE LINE PERIOD) presence to validate later
		if lineItem.Eval("count(cac:InvoicePeriod)").Int() > 0 {
			invoiceLine.linePeriodPresent = true
			invoiceLine.BillingSpecifiedPeriodStart, err = parseTimeUBL(lineItem, "cac:InvoicePeriod/cbc:StartDate")
			if err != nil {
				return fmt.Errorf("invalid line billing period start date for line %s: %w", invoiceLine.LineID, err)
			}
			invoiceLine.BillingSpecifiedPeriodEnd, err = parseTimeUBL(lineItem, "cac:InvoicePeriod/cbc:EndDate")
			if err != nil {
				return fmt.Errorf("invalid line billing period end date for line %s: %w", invoiceLine.LineID, err)
			}
		}

		// BT-128: Invoice line object identifier
		invoiceLine.lineDocumentReferenceCount = lineItem.Eval("count(cac:DocumentReference)").Int()
		invoiceLine.AdditionalReferencedDocumentID = lineItem.Eval("cac:DocumentReference/cbc:ID").String()
		invoiceLine.AdditionalReferencedDocumentTypeCode = lineItem.Eval("cac:DocumentReference/cbc:DocumentTypeCode").String()

		// BT-132: Referenced purchase order line
		invoiceLine.BuyerOrderReferencedDocument = lineItem.Eval("cac:OrderLineReference/cbc:LineID").String()

		// BT-133: Invoice line Buyer accounting reference
		invoiceLine.ReceivableSpecifiedTradeAccountingAccount = lineItem.Eval("cbc:AccountingCost").String()

		// BT-129: Invoiced quantity (or Credited quantity for credit notes)
		invoiceLine.BilledQuantity, err = getDecimal(lineItem, quantityElementName)
		if err != nil {
			return err
		}

		// BT-130: Invoiced quantity unit of measure
		invoiceLine.BilledQuantityUnit = lineItem.Eval(quantityElementName + "/@unitCode").String()

		// BT-131: Invoice line net amount
		// Track XML element presence for BR-24 validation
		invoiceLine.hasLineTotalInXML = lineItem.Eval("count(cbc:LineExtensionAmount)").Int() > 0
		invoiceLine.Total, err = getDecimal(lineItem, "cbc:LineExtensionAmount")
		if err != nil {
			return err
		}

		// Parse item information
		if err := parseUBLLineItem(lineItem, &invoiceLine); err != nil {
			return err
		}

		// Parse price information
		if err := parseUBLLinePrice(lineItem, &invoiceLine); err != nil {
			return err
		}

		// BG-27: Line level allowances
		// BG-28: Line level charges
		lineACCount := lineItem.Eval("count(cac:AllowanceCharge)").Int()
		if lineACCount > 0 {
			// Pre-allocate both slices with full capacity since we don't know the split
			invoiceLine.InvoiceLineAllowances = make([]AllowanceCharge, 0, lineACCount)
			invoiceLine.InvoiceLineCharges = make([]AllowanceCharge, 0, lineACCount)
			for ac := range lineItem.Each("cac:AllowanceCharge") {
				inv.checkContext()
				chargeIndicator := ac.Eval("string(cbc:ChargeIndicator) = 'true'").Bool()

				basisAmount, err := getDecimal(ac, "cbc:BaseAmount")
				if err != nil {
					return err
				}

				actualAmount, err := getDecimal(ac, "cbc:Amount")
				if err != nil {
					return err
				}

				calculationPercent, err := getDecimal(ac, "cbc:MultiplierFactorNumeric")
				if err != nil {
					return err
				}

				alc := AllowanceCharge{
					ChargeIndicator:      chargeIndicator,
					BasisAmount:          basisAmount,
					ActualAmount:         actualAmount,
					CalculationPercent:   calculationPercent,
					ReasonCode:           ac.Eval("cbc:AllowanceChargeReasonCode").String(),
					Reason:               ac.Eval("cbc:AllowanceChargeReason").String(),
					hasActualAmountInXML: ac.Eval("count(cbc:Amount)").Int() > 0,
					hasBasisAmountInXML:  ac.Eval("count(cbc:BaseAmount)").Int() > 0,
					hasPercentInXML:      ac.Eval("count(cbc:MultiplierFactorNumeric)").Int() > 0,
					hasIndicatorInXML:    ac.Eval("count(cbc:ChargeIndicator)").Int() > 0,
					indicatorValidXML:    isXMLBoolean(ac.Eval("cbc:ChargeIndicator").String()),
				}

				if chargeIndicator {
					invoiceLine.InvoiceLineCharges = append(invoiceLine.InvoiceLineCharges, alc)
				} else {
					invoiceLine.InvoiceLineAllowances = append(invoiceLine.InvoiceLineAllowances, alc)
				}
			}
		}

		// Parse line tax information
		taxInfo := lineItem.Eval("cac:Item/cac:ClassifiedTaxCategory")
		invoiceLine.TaxTypeCode = taxInfo.Eval("cac:TaxScheme/cbc:ID").String()
		if invoiceLine.TaxTypeCode == "" {
			invoiceLine.TaxTypeCode = "VAT" // Default to VAT
		}
		invoiceLine.TaxCategoryCode = taxInfo.Eval("cbc:ID").String()
		invoiceLine.TaxRateApplicablePercent, err = getDecimal(taxInfo, "cbc:Percent")
		if err != nil {
			return err
		}
		invoiceLine.hasTaxRateApplicablePercent = taxInfo.Eval("count(cbc:Percent)").Int() > 0

		inv.InvoiceLines = append(inv.InvoiceLines, invoiceLine)
	}

	return nil
}

// parseUBLLineItem parses item-specific information within a line.
func parseUBLLineItem(lineItem *cxpath.Context, invoiceLine *InvoiceLine) error {
	item := lineItem.Eval("cac:Item")

	// BT-153: Item name
	invoiceLine.ItemName = item.Eval("cbc:Name").String()

	// BT-154: Item description
	invoiceLine.Description = item.Eval("cbc:Description").String()

	// BT-155: Item Seller's identifier
	invoiceLine.ArticleNumber = item.Eval("cac:SellersItemIdentification/cbc:ID").String()

	// BT-156: Item Buyer's identifier
	invoiceLine.ArticleNumberBuyer = item.Eval("cac:BuyersItemIdentification/cbc:ID").String()

	// BT-157: Item standard identifier
	invoiceLine.GlobalID = item.Eval("cac:StandardItemIdentification/cbc:ID").String()
	invoiceLine.GlobalIDType = item.Eval("cac:StandardItemIdentification/cbc:ID/@schemeID").String()

	// BT-158: Item classification identifier
	classCount := item.Eval("count(cac:CommodityClassification)").Int()
	if classCount > 0 {
		invoiceLine.ProductClassification = make([]Classification, 0, classCount)
		for class := range item.Each("cac:CommodityClassification") {
			classification := Classification{
				ClassCode:      class.Eval("cbc:ItemClassificationCode").String(),
				ListID:         class.Eval("cbc:ItemClassificationCode/@listID").String(),
				ListVersionID:  class.Eval("cbc:ItemClassificationCode/@listVersionID").String(),
				hasListIDInXML: class.Eval("count(cbc:ItemClassificationCode/@listID)").Int() > 0,
			}
			invoiceLine.ProductClassification = append(invoiceLine.ProductClassification, classification)
		}
	}

	// BT-159: Item country of origin
	invoiceLine.OriginTradeCountry = item.Eval("cac:OriginCountry/cbc:IdentificationCode").String()

	// BG-32: Item attributes
	attrCount := item.Eval("count(cac:AdditionalItemProperty)").Int()
	if attrCount > 0 {
		invoiceLine.Characteristics = make([]Characteristic, 0, attrCount)
		for attr := range item.Each("cac:AdditionalItemProperty") {
			characteristic := Characteristic{
				Description: attr.Eval("cbc:Name").String(),
				Value:       attr.Eval("cbc:Value").String(),
			}
			invoiceLine.Characteristics = append(invoiceLine.Characteristics, characteristic)
		}
	}

	return nil
}

// parseUBLLinePrice parses price information within a line.
func parseUBLLinePrice(lineItem *cxpath.Context, invoiceLine *InvoiceLine) error {
	price := lineItem.Eval("cac:Price")

	var err error

	// BT-146: Item net price
	// Track XML element presence for BR-26 validation
	invoiceLine.hasNetPriceInXML = price.Eval("count(cbc:PriceAmount)").Int() > 0
	invoiceLine.NetPrice, err = getDecimal(price, "cbc:PriceAmount")
	if err != nil {
		return err
	}

	// BT-149: Item price base quantity with unit code
	invoiceLine.BasisQuantity, err = getDecimal(price, "cbc:BaseQuantity")
	if err != nil {
		return err
	}
	invoiceLine.BasisQuantityUnit = price.Eval("cbc:BaseQuantity/@unitCode").String()
	invoiceLine.hasNetBasisQuantityInXML = price.Eval("count(cbc:BaseQuantity)").Int() > 0

	// BT-148: Item gross price (price before allowances)
	// UBL doesn't have a direct gross price field, but may have allowances on price
	// For now, calculate from net price if allowances exist on price
	priceACCount := price.Eval("count(cac:AllowanceCharge)").Int()
	if priceACCount > 0 {
		invoiceLine.AppliedTradeAllowanceCharge = make([]AllowanceCharge, 0, priceACCount)
		for ac := range price.Each("cac:AllowanceCharge") {
			chargeIndicator := ac.Eval("string(cbc:ChargeIndicator) = 'true'").Bool()

			basisAmount, err := getDecimal(ac, "cbc:BaseAmount")
			if err != nil {
				return err
			}

			actualAmount, err := getDecimal(ac, "cbc:Amount")
			if err != nil {
				return err
			}

			calculationPercent, err := getDecimal(ac, "cbc:MultiplierFactorNumeric")
			if err != nil {
				return err
			}

			allowanceCharge := AllowanceCharge{
				ChargeIndicator:      chargeIndicator,
				BasisAmount:          basisAmount,
				ActualAmount:         actualAmount,
				CalculationPercent:   calculationPercent,
				ReasonCode:           ac.Eval("cbc:AllowanceChargeReasonCode").String(),
				Reason:               ac.Eval("cbc:AllowanceChargeReason").String(),
				hasActualAmountInXML: ac.Eval("count(cbc:Amount)").Int() > 0,
				hasBasisAmountInXML:  ac.Eval("count(cbc:BaseAmount)").Int() > 0,
				hasPercentInXML:      ac.Eval("count(cbc:MultiplierFactorNumeric)").Int() > 0,
				hasIndicatorInXML:    ac.Eval("count(cbc:ChargeIndicator)").Int() > 0,
				indicatorValidXML:    isXMLBoolean(ac.Eval("cbc:ChargeIndicator").String()),
			}

			invoiceLine.AppliedTradeAllowanceCharge = append(invoiceLine.AppliedTradeAllowanceCharge, allowanceCharge)

			// Calculate gross price if we have basis amount
			if !basisAmount.IsZero() && invoiceLine.GrossPrice.IsZero() {
				invoiceLine.GrossPrice = basisAmount
			}
		}
	}

	return nil
}
