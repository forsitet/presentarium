// Package pptxtest provides a fake pptx.Converter implementation for tests.
// Importing this package lets integration/e2e tests exercise the presentation
// upload flow without requiring libreoffice, pdftoppm, or cwebp to be
// installed on the test host.
package pptxtest

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"presentarium/pkg/pptx"
)

// FakeConverter returns a fixed number of synthetic slides. The produced WebP
// bytes are NOT a valid WebP image — they are opaque placeholders that
// MemStorage happily stores and returns for URL assertions.
type FakeConverter struct {
	// SlideCount is the number of slides to pretend the .pptx contained.
	// Defaults to 3 if zero.
	SlideCount int
	// Width/Height are reported in every returned slide. Defaults to 1920x1080.
	Width, Height int
	// Err, if set, causes Convert to fail without producing any slides
	// (useful for testing the failure branch).
	Err error
}

// Convert implements pptx.Converter.
func (f *FakeConverter) Convert(_ context.Context, src io.Reader) ([]pptx.ConvertedSlide, error) {
	_, _ = io.Copy(io.Discard, src)
	if f.Err != nil {
		return nil, f.Err
	}
	count := f.SlideCount
	if count == 0 {
		count = 3
	}
	w := f.Width
	if w == 0 {
		w = 1920
	}
	h := f.Height
	if h == 0 {
		h = 1080
	}
	out := make([]pptx.ConvertedSlide, count)
	for i := 0; i < count; i++ {
		payload := bytes.Repeat([]byte(fmt.Sprintf("slide-%d-", i+1)), 4)
		out[i] = pptx.ConvertedSlide{
			Position: i + 1,
			WebP:     payload,
			Width:    w,
			Height:   h,
		}
	}
	return out, nil
}

// Compile-time check that FakeConverter implements pptx.Converter.
var _ pptx.Converter = (*FakeConverter)(nil)
