package einvoice

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/shopspring/decimal"
	"github.com/speedata/cxpath"
)

// getDecimal parses a decimal value from an XPath evaluation result.
// Shared by both CII and UBL parsers.
func getDecimal(ctx *cxpath.Context, eval string) (decimal.Decimal, error) {
	a := ctx.Eval(eval).String()
	if a == "" {
		return decimal.Zero, nil
	}
	str, err := decimal.NewFromString(a)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid decimal value '%s' at %s: %w", a, eval, err)
	}
	return str, nil
}

// ParseReader reads the XML from the reader and auto-detects the format (CII or UBL).
// It detects the format by examining the root element namespace and routes to the
// appropriate parser. Each parser handles its own namespace setup.
func ParseReader(r io.Reader) (*Invoice, error) {
	return ParseReaderContext(context.Background(), r)
}

// ParseReaderContext reads one XML invoice and stops when ctx is canceled.
// ParseReader remains available for callers that do not need cancellation.
func ParseReaderContext(operationCtx context.Context, r io.Reader) (inv *Invoice, err error) {
	defer func() {
		if inv != nil {
			inv.operationContext = nil
		}
		if recovered := recover(); recovered != nil {
			if canceled, ok := recovered.(operationCanceled); ok {
				inv = nil
				err = canceled.cause
				return
			}
			panic(recovered)
		}
	}()

	if operationCtx == nil || r == nil {
		return nil, fmt.Errorf("einvoice: nil parse argument")
	}
	if err := operationCtx.Err(); err != nil {
		return nil, err
	}

	xpathCtx, err := cxpath.NewFromReader(contextReader{ctx: operationCtx, src: r})
	if err != nil {
		if contextErr := operationCtx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, fmt.Errorf("cannot read from reader: %w", err)
	}
	if err := operationCtx.Err(); err != nil {
		return nil, err
	}

	// Detect format by checking root element namespace
	root := xpathCtx.Root()
	rootns := root.Eval("namespace-uri()").String()

	switch rootns {
	case "":
		return nil, fmt.Errorf("empty root element namespace")

	// CII format (ZUGFeRD/Factur-X)
	case nsCIIRootInvoice:
		inv, err = parseCII(operationCtx, xpathCtx)
		if err != nil {
			return nil, fmt.Errorf("parse CII: %w", err)
		}

	// UBL format (Invoice or CreditNote)
	case nsUBLInvoice, nsUBLCreditNote:
		inv, err = parseUBL(operationCtx, xpathCtx)
		if err != nil {
			return nil, fmt.Errorf("parse UBL: %w", err)
		}

	default:
		return nil, fmt.Errorf("unknown root element namespace: %s", rootns)
	}
	if contextErr := operationCtx.Err(); contextErr != nil {
		return nil, contextErr
	}

	inv.isParsed = true
	return inv, nil
}

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

// ParseXMLFile reads the XML file at filename.
func ParseXMLFile(filename string) (*Invoice, error) {
	r, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("einvoice: cannot open file (%w)", err)
	}
	defer func() { _ = r.Close() }()

	return ParseReader(r)
}
