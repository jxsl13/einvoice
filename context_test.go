package einvoice

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseReaderContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	input := `<Invoice xmlns="` + nsUBLInvoice + `">` + strings.Repeat(`<x/>`, 1_000) + `</Invoice>`
	reader := &cancelingChunkReader{src: strings.NewReader(input), cancel: cancel}

	invoice, err := ParseReaderContext(ctx, reader)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ParseReaderContext() error = %v, want context.Canceled", err)
	}
	if invoice != nil {
		t.Fatalf("ParseReaderContext() invoice = %#v, want nil", invoice)
	}
}

func TestContextAPIsRejectNilAndClearOperationState(t *testing.T) {
	t.Parallel()
	if _, err := ParseReaderContext(nil, strings.NewReader(`<x/>`)); err == nil { //nolint:staticcheck // Exercise the defensive API boundary.
		t.Fatal("ParseReaderContext(nil, reader) error = nil")
	}
	if _, err := ParseReaderContext(context.Background(), nil); err == nil {
		t.Fatal("ParseReaderContext(ctx, nil) error = nil")
	}

	invoice := &Invoice{}
	if err := invoice.ValidateContext(nil); err == nil { //nolint:staticcheck // Exercise the defensive API boundary.
		t.Fatal("ValidateContext(nil) error = nil")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := invoice.ValidateContext(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateContext(canceled) error = %v, want context.Canceled", err)
	}
	if invoice.operationContext != nil {
		t.Fatal("ValidateContext retained its context")
	}
	if invoice.violations != nil || invoice.warnings != nil {
		t.Fatal("canceled validation retained partial findings")
	}

	midValidation := &countdownContext{remaining: 2, done: make(chan struct{})}
	invoice = &Invoice{InvoiceLines: []InvoiceLine{{}}}
	if err := invoice.ValidateContext(midValidation); !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateContext(mid-validation cancel) error = %v, want context.Canceled", err)
	}
	if invoice.operationContext != nil || invoice.violations != nil || invoice.warnings != nil {
		t.Fatal("mid-validation cancellation retained operation state")
	}
}

type cancelingChunkReader struct {
	src    io.Reader
	cancel context.CancelFunc
	read   bool
}

func (r *cancelingChunkReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 32 {
		buffer = buffer[:32]
	}
	read, err := r.src.Read(buffer)
	if !r.read {
		r.read = true
		r.cancel()
	}
	return read, err
}

type countdownContext struct {
	remaining int
	done      chan struct{}
	once      sync.Once
}

func (*countdownContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *countdownContext) Done() <-chan struct{}     { return c.done }
func (*countdownContext) Value(any) any               { return nil }

func (c *countdownContext) Err() error {
	if c.remaining > 0 {
		c.remaining--
		return nil
	}
	c.once.Do(func() { close(c.done) })
	return context.Canceled
}
