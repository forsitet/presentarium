package pptx

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewCLIConverter_Defaults(t *testing.T) {
	c := NewCLIConverter()
	if c.DPI != 150 {
		t.Errorf("DPI = %d", c.DPI)
	}
	if c.WebPQuality != 85 {
		t.Errorf("WebPQuality = %d", c.WebPQuality)
	}
	if c.Timeout != 10*time.Minute {
		t.Errorf("Timeout = %v", c.Timeout)
	}
	if c.LibreofficeBin == "" || c.PdftoppmBin == "" || c.CwebpBin == "" {
		t.Error("CLI bin paths should default to non-empty")
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.bin")
	src := bytes.NewBufferString("hello-bytes")
	if err := writeFile(path, src); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello-bytes" {
		t.Errorf("contents = %q", got)
	}
}

func TestWriteFile_BadPath(t *testing.T) {
	// Directory that does not exist → os.Create should fail.
	err := writeFile(filepath.Join(t.TempDir(), "no-such-dir", "x.bin"), bytes.NewBufferString("x"))
	if err == nil {
		t.Error("expected error writing to nonexistent directory")
	}
}

func TestReadPNGSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	img := image.NewRGBA(image.Rect(0, 0, 17, 23))
	img.Set(0, 0, color.Black)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	w, h, err := readPNGSize(path)
	if err != nil {
		t.Fatalf("readPNGSize: %v", err)
	}
	if w != 17 || h != 23 {
		t.Errorf("size = %dx%d, want 17x23", w, h)
	}
}

func TestReadPNGSize_MissingFile(t *testing.T) {
	if _, _, err := readPNGSize(filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadPNGSize_NotPNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "junk.png")
	if err := os.WriteFile(path, []byte("not really a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readPNGSize(path); err == nil {
		t.Error("expected error decoding non-png bytes as png")
	}
}

func TestCLIConverter_Convert_MissingBinariesFails(t *testing.T) {
	c := &CLIConverter{
		DPI:            150,
		WebPQuality:    85,
		Timeout:        2 * time.Second,
		LibreofficeBin: "definitely-not-a-binary-libreoffice-xyz",
		PdftoppmBin:    "definitely-not-a-binary-pdftoppm-xyz",
		CwebpBin:       "definitely-not-a-binary-cwebp-xyz",
	}
	_, err := c.Convert(context.Background(), bytes.NewBufferString("fake-pptx-content"))
	if err == nil {
		t.Fatal("expected error when CLI binaries are missing")
	}
	if !strings.Contains(err.Error(), "libreoffice") {
		t.Errorf("error should mention libreoffice step, got %v", err)
	}
}

func TestCLIConverter_Convert_AlreadyCancelled(t *testing.T) {
	c := NewCLIConverter()
	c.Timeout = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Convert(ctx, bytes.NewBufferString(""))
	if err == nil {
		t.Error("expected error when context is cancelled before start")
	}
	if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "canceled") || strings.Contains(err.Error(), "libreoffice") {
		// Accept any plausible error; just verify Convert exits cleanly.
		return
	}
}
