// Package pptx converts PowerPoint (.pptx) files into a sequence of WebP
// images suitable for the web. The pipeline is:
//
//  1. libreoffice --headless --convert-to pdf   (.pptx -> .pdf)
//  2. pdftoppm -r 150 -png                      (.pdf  -> slide-N.png)
//  3. cwebp -q 85                               (.png  -> .webp)
//
// All three tools are invoked as child processes; Go never parses the
// binary formats directly. The Converter interface lets tests substitute a
// fake pipeline so the service layer can be exercised without LibreOffice
// installed.
package pptx

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ConvertedSlide is one rendered slide returned by Convert. WebP is the
// compressed image bytes; Width/Height are the native pixel dimensions of
// the PNG before WebP encoding (WebP preserves them).
type ConvertedSlide struct {
	Position int
	WebP     []byte
	Width    int
	Height   int
}

// Converter turns a .pptx byte stream into a slice of WebP slides.
type Converter interface {
	Convert(ctx context.Context, src io.Reader) ([]ConvertedSlide, error)
}

// CLIConverter is the production Converter, wrapping the libreoffice +
// pdftoppm + cwebp CLI pipeline.
type CLIConverter struct {
	// DPI controls the pdftoppm rasterisation density. 150 gives a sharp
	// image on retina displays without ballooning file size.
	DPI int
	// WebPQuality is passed to cwebp's -q flag (0..100). 85 is a good
	// quality/size trade-off for rasterised slide backgrounds.
	WebPQuality int
	// Timeout bounds a single Convert invocation. Converting a 100-slide
	// deck can take a while; 10min is a safe ceiling.
	Timeout time.Duration

	LibreofficeBin string
	PdftoppmBin    string
	CwebpBin       string
}

// NewCLIConverter returns a Converter with sensible defaults. The CLI
// binaries are resolved via $PATH at conversion time.
func NewCLIConverter() *CLIConverter {
	return &CLIConverter{
		DPI:            150,
		WebPQuality:    85,
		Timeout:        10 * time.Minute,
		LibreofficeBin: "libreoffice",
		PdftoppmBin:    "pdftoppm",
		CwebpBin:       "cwebp",
	}
}

// Convert writes src to a temp directory, runs the three-step pipeline and
// returns the resulting slides. The temp directory is removed before Convert
// returns (on success or failure).
func (c *CLIConverter) Convert(ctx context.Context, src io.Reader) ([]ConvertedSlide, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	tmpDir, err := os.MkdirTemp("", "pptx-convert-*")
	if err != nil {
		return nil, fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	pptxPath := filepath.Join(tmpDir, "input.pptx")
	if err := writeFile(pptxPath, src); err != nil {
		return nil, fmt.Errorf("write pptx: %w", err)
	}

	pdfPath, err := c.pptxToPDF(ctx, pptxPath, tmpDir)
	if err != nil {
		return nil, err
	}

	pngs, err := c.pdfToPNGs(ctx, pdfPath, tmpDir)
	if err != nil {
		return nil, err
	}
	if len(pngs) == 0 {
		return nil, errors.New("pdftoppm produced no slides")
	}

	slides := make([]ConvertedSlide, 0, len(pngs))
	for i, pngPath := range pngs {
		w, h, err := readPNGSize(pngPath)
		if err != nil {
			return nil, fmt.Errorf("read png %s: %w", pngPath, err)
		}
		webp, err := c.pngToWebP(ctx, pngPath, tmpDir)
		if err != nil {
			return nil, err
		}
		slides = append(slides, ConvertedSlide{
			Position: i + 1,
			WebP:     webp,
			Width:    w,
			Height:   h,
		})
	}
	return slides, nil
}

func (c *CLIConverter) pptxToPDF(ctx context.Context, pptxPath, outDir string) (string, error) {
	// LibreOffice stores a user profile in $HOME/.config/libreoffice; when
	// running inside a minimal container as a non-persistent user the
	// default path is either unwritable or shared across concurrent
	// conversions. -env:UserInstallation pins it to a fresh per-call dir.
	userInstall := "-env:UserInstallation=file://" + filepath.Join(outDir, "lo-profile")

	cmd := exec.CommandContext(ctx, c.LibreofficeBin,
		"--headless",
		userInstall,
		"--convert-to", "pdf",
		"--outdir", outDir,
		pptxPath,
	)
	// LibreOffice refuses to run without HOME on some distros.
	cmd.Env = append(os.Environ(), "HOME="+outDir)

	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("libreoffice convert: %w: %s", err, strings.TrimSpace(string(out)))
	}

	pdfPath := filepath.Join(outDir, "input.pdf")
	if _, err := os.Stat(pdfPath); err != nil {
		return "", fmt.Errorf("libreoffice produced no pdf at %s: %w", pdfPath, err)
	}
	return pdfPath, nil
}

func (c *CLIConverter) pdfToPNGs(ctx context.Context, pdfPath, outDir string) ([]string, error) {
	prefix := filepath.Join(outDir, "slide")
	cmd := exec.CommandContext(ctx, c.PdftoppmBin,
		"-r", strconv.Itoa(c.DPI),
		"-png",
		pdfPath, prefix,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %w: %s", err, strings.TrimSpace(string(out)))
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		return nil, err
	}
	type row struct {
		path string
		num  int
	}
	var rows []row
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "slide-") || !strings.HasSuffix(name, ".png") {
			continue
		}
		numStr := strings.TrimSuffix(strings.TrimPrefix(name, "slide-"), ".png")
		n, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}
		rows = append(rows, row{path: filepath.Join(outDir, name), num: n})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].num < rows[j].num })

	paths := make([]string, len(rows))
	for i, r := range rows {
		paths[i] = r.path
	}
	return paths, nil
}

func (c *CLIConverter) pngToWebP(ctx context.Context, pngPath, outDir string) ([]byte, error) {
	webpPath := pngPath + ".webp"
	cmd := exec.CommandContext(ctx, c.CwebpBin,
		"-quiet",
		"-q", strconv.Itoa(c.WebPQuality),
		pngPath,
		"-o", webpPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("cwebp %s: %w: %s", pngPath, err, strings.TrimSpace(string(out)))
	}
	data, err := os.ReadFile(webpPath)
	if err != nil {
		return nil, err
	}
	// Free the intermediate WebP file early — otherwise we'd wait for the
	// MkdirTemp defer to remove the whole tree at the end of Convert.
	_ = os.Remove(webpPath)
	return data, nil
}

func writeFile(path string, src io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, src)
	return err
}

func readPNGSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}
