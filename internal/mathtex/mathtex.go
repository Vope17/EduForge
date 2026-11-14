// Package mathtex renders the TeX formulas outputs contain: SVG for pages
// and documents, PNG for slides and Word templates.
package mathtex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Formula is one TeX formula, shown inline in a line of text or on its own.
type Formula struct {
	TeX     string `json:"tex"`
	Display bool   `json:"display"`
}

// Rendered is a formula as SVG. Sizes are in ex of the surrounding text;
// Depth is how far it reaches below the baseline. Error is set when the TeX
// cannot be parsed.
type Rendered struct {
	SVG    string  `json:"svg,omitempty"`
	Width  float64 `json:"width,omitempty"`
	Height float64 `json:"height,omitempty"`
	Depth  float64 `json:"depth,omitempty"`
	Error  string  `json:"error,omitempty"`
}

// Renderer turns formulas into pictures.
type Renderer interface {
	Render(ctx context.Context, formulas []Formula) ([]Rendered, error)
	// PNG rasterises a rendered formula in colour (RRGGBB) at scale pixels
	// per ex.
	PNG(ctx context.Context, r Rendered, color string, scale float64) ([]byte, error)
}

// Colored fills a rendered formula with color (RRGGBB).
func Colored(svg, color string) string {
	if len(color) != 6 {
		color = "000000"
	}
	return strings.ReplaceAll(svg, "currentColor", "#"+color)
}

var rootSize = regexp.MustCompile(`^(<svg\b[^>]*?)\s(width|height)="[^"]*"([^>]*?)\s(width|height)="[^"]*"`)

// Sized sets a rendered formula's size in points, for renderers that do not
// understand MathJax's ex units (LibreOffice draws such an SVG as nothing).
func Sized(r Rendered, pointsPerEx float64) string {
	return rootSize.ReplaceAllStringFunc(r.SVG, func(m string) string {
		p := rootSize.FindStringSubmatch(m)
		size := map[string]float64{"width": r.Width * pointsPerEx, "height": r.Height * pointsPerEx}
		return fmt.Sprintf(`%s %s="%.2fpt"%s %s="%.2fpt"`, p[1], p[2], size[p[2]], p[3], p[4], size[p[4]])
	})
}

// Node renders with MathJax (math/render.mjs) under Node.js and rasterises
// with rsvg-convert. Results are cached in memory.
type Node struct {
	Script string
	mu     sync.Mutex
	cache  map[Formula]Rendered
}

// New returns a renderer for the MathJax script, or nil when the script or
// Node.js is not available (formulas are then shown as TeX source).
func New(script string) Renderer {
	if _, err := os.Stat(script); err != nil {
		return nil
	}
	if _, err := exec.LookPath("node"); err != nil {
		return nil
	}
	return &Node{Script: script, cache: map[Formula]Rendered{}}
}

const cacheLimit = 5000

func (n *Node) Render(ctx context.Context, formulas []Formula) ([]Rendered, error) {
	out := make([]Rendered, len(formulas))
	missing := []Formula{}
	seen := map[Formula]bool{}
	n.mu.Lock()
	for i, f := range formulas {
		if r, ok := n.cache[f]; ok {
			out[i] = r
		} else if !seen[f] {
			seen[f] = true
			missing = append(missing, f)
		}
	}
	n.mu.Unlock()
	if len(missing) > 0 {
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "node", n.Script)
		input, err := json.Marshal(missing)
		if err != nil {
			return nil, err
		}
		cmd.Stdin = bytes.NewReader(input)
		raw, err := cmd.Output()
		if err != nil {
			return nil, errors.New("math_renderer_failed")
		}
		var results []Rendered
		if err = json.Unmarshal(raw, &results); err != nil || len(results) != len(missing) {
			return nil, errors.New("math_renderer_failed")
		}
		n.mu.Lock()
		if len(n.cache)+len(results) > cacheLimit {
			n.cache = map[Formula]Rendered{}
		}
		for i, f := range missing {
			n.cache[f] = results[i]
		}
		for i, f := range formulas {
			out[i] = n.cache[f]
		}
		n.mu.Unlock()
	}
	return out, nil
}

func (n *Node) PNG(ctx context.Context, r Rendered, color string, scale float64) ([]byte, error) {
	if r.SVG == "" || r.Width <= 0 {
		return nil, errors.New("math_not_rendered")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rsvg-convert", "--format", "png", "--width", strconv.Itoa(max(1, int(r.Width*scale))), "--keep-aspect-ratio")
	cmd.Stdin = strings.NewReader(Colored(r.SVG, color))
	b, err := cmd.Output()
	if err != nil {
		return nil, errors.New("math_png_failed")
	}
	return b, nil
}
