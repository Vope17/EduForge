package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// PDF follows Paper2Lab's Office-to-PDF approach. Every invocation has an
// isolated profile and directory; no source strings are interpolated into a shell.
func PDF(ctx context.Context, office []byte, format string) (_ []byte, err error) {
	if format != "docx" && format != "pptx" {
		return nil, fmt.Errorf("unsupported PDF source")
	}
	tmp, err := os.MkdirTemp("", "eduforge-export-")
	if err != nil {
		return nil, fmt.Errorf("create PDF workspace: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(tmp)) }()
	source := filepath.Join(tmp, "artifact."+format)
	if err := os.WriteFile(source, office, 0600); err != nil {
		return nil, fmt.Errorf("write PDF source: %w", err)
	}
	profile := (&url.URL{Scheme: "file", Path: filepath.Join(tmp, "profile")}).String()
	filter := "pdf:writer_pdf_Export"
	if format == "pptx" {
		filter = "pdf:impress_pdf_Export"
	}
	work, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(work, "libreoffice", "-env:UserInstallation="+profile,
		"--headless", "--nologo", "--nodefault", "--nofirststartwizard", "--convert-to", filter, "--outdir", tmp, source)
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("convert PDF: %w", err)
	}
	output := filepath.Join(tmp, "artifact.pdf")
	info, err := os.Stat(output)
	if err != nil {
		return nil, fmt.Errorf("read PDF output information: %w", err)
	}
	if info.Size() > 128<<20 {
		return nil, fmt.Errorf("PDF output too large")
	}
	data, err := os.ReadFile(output)
	if err != nil {
		return nil, fmt.Errorf("read PDF: %w", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, fmt.Errorf("invalid PDF output")
	}
	return data, nil
}
