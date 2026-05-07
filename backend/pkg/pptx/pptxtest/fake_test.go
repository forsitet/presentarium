package pptxtest_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"presentarium/pkg/pptx/pptxtest"
)

func TestFakeConverter_Defaults(t *testing.T) {
	f := &pptxtest.FakeConverter{}
	slides, err := f.Convert(context.Background(), bytes.NewBufferString("input"))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(slides) != 3 {
		t.Errorf("default slide count = %d, want 3", len(slides))
	}
	for i, s := range slides {
		if s.Position != i+1 {
			t.Errorf("slide %d position = %d", i, s.Position)
		}
		if s.Width != 1920 || s.Height != 1080 {
			t.Errorf("slide %d size = %dx%d, want 1920x1080", i, s.Width, s.Height)
		}
		if len(s.WebP) == 0 {
			t.Errorf("slide %d has empty payload", i)
		}
	}
}

func TestFakeConverter_CustomSlideCount(t *testing.T) {
	f := &pptxtest.FakeConverter{SlideCount: 7, Width: 800, Height: 600}
	slides, err := f.Convert(context.Background(), bytes.NewBufferString(""))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(slides) != 7 {
		t.Errorf("got %d slides", len(slides))
	}
	if slides[0].Width != 800 || slides[0].Height != 600 {
		t.Errorf("size = %dx%d", slides[0].Width, slides[0].Height)
	}
}

func TestFakeConverter_ErrorPath(t *testing.T) {
	want := errors.New("forced failure")
	f := &pptxtest.FakeConverter{Err: want}
	slides, err := f.Convert(context.Background(), bytes.NewBufferString(""))
	if !errors.Is(err, want) {
		t.Errorf("Convert err = %v, want %v", err, want)
	}
	if slides != nil {
		t.Error("slides should be nil on error path")
	}
}

func TestFakeConverter_DrainsReader(t *testing.T) {
	r := strings.NewReader("some bytes that should be drained")
	f := &pptxtest.FakeConverter{}
	if _, err := f.Convert(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if r.Len() != 0 {
		t.Errorf("expected reader fully drained, %d bytes remain", r.Len())
	}
}
