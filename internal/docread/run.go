package docread

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// run executes a tool without a shell and with a time limit.
func run(ctx context.Context, limit time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s_timed_out", name)
		}
		return nil, fmt.Errorf("%s_failed: %w", name, err)
	}
	return out, nil
}

// toPDF converts an Office file to PDF with LibreOffice in a throwaway
// profile and output directory, and returns the PDF path and its cleanup.
func toPDF(ctx context.Context, input string) (pdf string, cleanup func(), err error) {
	work, err := os.MkdirTemp("", "docread-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(work) }
	profile := filepath.Join(work, "profile")
	out := filepath.Join(work, "out")
	if err = os.MkdirAll(out, 0o700); err != nil {
		cleanup()
		return "", nil, err
	}
	if _, err = run(ctx, 180*time.Second, "libreoffice", "-env:UserInstallation=file://"+profile, "--headless", "--convert-to", "pdf", "--outdir", out, input); err != nil {
		cleanup()
		return "", nil, err
	}
	pdf = filepath.Join(out, strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))+".pdf")
	if _, err = os.Stat(pdf); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("pdf_export_missing")
	}
	return pdf, cleanup, nil
}
