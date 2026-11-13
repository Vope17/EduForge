// Package render creates downloads from immutable artifact revisions.
package render

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/template"
)

var ErrLayout = errors.New("slide content does not fit the export layout")

const relNS = "http://schemas.openxmlformats.org/package/2006/relationships"
const officeRel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
const wNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
const aNS = "http://schemas.openxmlformats.org/drawingml/2006/main"
const pNS = "http://schemas.openxmlformats.org/presentationml/2006/main"
const rNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

type Image struct {
	Data          []byte
	Width, Height int
}
type Input struct {
	Artifact artifact.Artifact
	Spec     template.Spec
	Font     []byte
	Images   map[string]Image
}

type packageFile struct {
	name string
	data []byte
}
type packageBuilder struct {
	files []packageFile
	types string
}

func (p *packageBuilder) add(name, contentType string, data []byte) {
	p.files = append(p.files, packageFile{name, data})
	if contentType != "" {
		p.types += `<Override PartName="/` + name + `" ContentType="` + contentType + `"/>`
	}
}
func (p *packageBuilder) xml(name, contentType, data string) {
	p.add(name, contentType, []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+data))
}
func (p *packageBuilder) finish(ctx context.Context) ([]byte, error) {
	p.xml("[Content_Types].xml", "", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`+p.types+`</Types>`)
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, f := range p.files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		w, err := z.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func esc(s string) string { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
func num(i int) string    { return strconv.Itoa(i) }
func rel(id, kind, target string) string {
	return `<Relationship Id="` + id + `" Type="` + officeRel + kind + `" Target="` + esc(target) + `"/>`
}
func relationships(body string) string {
	return `<Relationships xmlns="` + relNS + `">` + body + `</Relationships>`
}
func (in Input) family() string {
	if in.Spec.Font == "serif" {
		return "Noto Serif TC"
	}
	return "Noto Sans TC"
}
func (in Input) color() string {
	switch in.Spec.Palette {
	case "ocean":
		return "176B9B"
	case "warm":
		return "9D502C"
	default:
		return "28634A"
	}
}
func Office(ctx context.Context, in Input, format string) ([]byte, error) {
	if err := in.Artifact.Content.ValidateFor(in.Artifact.Type); err != nil {
		return nil, err
	}
	if len(in.Font) < 32 {
		return nil, fmt.Errorf("export font is missing")
	}
	for _, b := range artifact.FlattenBlocks(in.Artifact.Content.Blocks) {
		if b.Kind == "image" && b.ImageURL != "" {
			if _, ok := in.Images[b.ImageURL]; !ok {
				return nil, fmt.Errorf("export image is missing")
			}
		}
	}
	switch {
	case format == "docx" && in.Artifact.Type == artifact.DocumentType:
		return word(ctx, in)
	case format == "pptx" && in.Artifact.Type == artifact.SlidesType:
		return slides(ctx, in)
	default:
		return nil, fmt.Errorf("unsupported export format")
	}
}
