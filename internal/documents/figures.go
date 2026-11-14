package documents

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// Figures in papers are usually vector drawings, so extracting embedded bitmaps
// yields icons and masks rather than the figure. Instead, locate each
// "Figure N" caption with pdftotext's layout boxes and crop the rendered page
// region between the caption and the nearest body text above it.

const figureDPI = 150

var (
	figureCaption      = regexp.MustCompile(`^(Figure|Fig\.?|FIGURE|圖)\s*\d+`)
	anyCaption         = regexp.MustCompile(`^(Figure|Fig\.?|FIGURE|圖|Table|TABLE|表)\s*\d+`)
	bboxPage           = regexp.MustCompile(`(?s)<page width="([\d.]+)" height="([\d.]+)">(.*?)</page>`)
	bboxBlock          = regexp.MustCompile(`(?s)<block xMin="([\d.]+)" yMin="([\d.]+)" xMax="([\d.]+)" yMax="([\d.]+)">(.*?)</block>`)
	bboxLine           = regexp.MustCompile(`<line xMin="[\d.]+" yMin="([\d.]+)" xMax="[\d.]+" yMax="([\d.]+)"`)
	bboxWord           = regexp.MustCompile(`<word xMin="([\d.]+)" yMin="[\d.]+" xMax="([\d.]+)"[^>]*>([^<]+)</word>`)
	equationNumberLine = regexp.MustCompile(`(?m)^\s*\(\d{1,3}\)\s*$`)
	numberedFormula    = regexp.MustCompile(`(?m)=.*\(\d{1,3}\)\s*$`)
)

type layoutBlock struct {
	x0, y0, x1, y1 float64
	lines          int
	lineHeight     float64
	text           string
}

type layoutWord struct {
	x0, x1 float64
	text   string
}

// blockText joins a block's words. Side-by-side captions ("Figure 6: … Figure
// 7: …" laid out as two columns) come out of pdftotext as one block whose
// lines interleave both captions, so such a block is read column by column.
func blockText(lines [][]layoutWord) string {
	split := captionColumn(lines)
	left, right := []string{}, []string{}
	for _, line := range lines {
		for _, w := range line {
			if split > 0 && w.x0 >= split-0.5 {
				right = append(right, w.text)
			} else {
				left = append(left, w.text)
			}
		}
	}
	if len(right) == 0 {
		return joinWords(left)
	}
	return joinWords(left) + " " + joinWords(right)
}

// captionColumn returns the x where a second caption column starts, or 0. The
// column must begin with "Figure N", other lines must also start words there,
// and no word may cross it.
func captionColumn(lines [][]layoutWord) float64 {
	if len(lines) < 2 || !figureCaption.MatchString(lines[0][0].text+" "+wordAt(lines[0], 1)) {
		return 0
	}
	for i := 1; i < len(lines[0]); i++ {
		w := lines[0][i]
		if !figureCaption.MatchString(w.text+" "+wordAt(lines[0], i+1)) || w.x0 < lines[0][0].x0+20 {
			continue
		}
		aligned, crossed := false, false
		for _, line := range lines[1:] {
			for _, o := range line {
				if o.x0 > w.x0-0.5 && o.x0 < w.x0+0.5 {
					aligned = true
				}
				if o.x0 < w.x0-0.5 && o.x1 > w.x0+0.5 {
					crossed = true
				}
			}
		}
		if aligned && !crossed {
			return w.x0
		}
	}
	return 0
}

func wordAt(line []layoutWord, i int) string {
	if i < len(line) {
		return line[i].text
	}
	return ""
}

type layoutPage struct {
	width, height float64
	blocks        []layoutBlock
}

func parseLayout(b []byte) []layoutPage {
	pages := []layoutPage{}
	for _, pm := range bboxPage.FindAllSubmatch(b, -1) {
		w, _ := strconv.ParseFloat(string(pm[1]), 64)
		h, _ := strconv.ParseFloat(string(pm[2]), 64)
		page := layoutPage{width: w, height: h}
		for _, bm := range bboxBlock.FindAllSubmatch(pm[3], -1) {
			var blk layoutBlock
			blk.x0, _ = strconv.ParseFloat(string(bm[1]), 64)
			blk.y0, _ = strconv.ParseFloat(string(bm[2]), 64)
			blk.x1, _ = strconv.ParseFloat(string(bm[3]), 64)
			blk.y1, _ = strconv.ParseFloat(string(bm[4]), 64)
			total := 0.0
			for _, lm := range bboxLine.FindAllSubmatch(bm[5], -1) {
				top, _ := strconv.ParseFloat(string(lm[1]), 64)
				bottom, _ := strconv.ParseFloat(string(lm[2]), 64)
				total += bottom - top
				blk.lines++
			}
			if blk.lines > 0 {
				blk.lineHeight = total / float64(blk.lines)
			}
			lines := [][]layoutWord{}
			for _, line := range strings.SplitAfter(string(bm[5]), "</line>") {
				words := []layoutWord{}
				for _, wm := range bboxWord.FindAllStringSubmatch(line, -1) {
					x0, _ := strconv.ParseFloat(wm[1], 64)
					x1, _ := strconv.ParseFloat(wm[2], 64)
					words = append(words, layoutWord{x0: x0, x1: x1, text: html.UnescapeString(wm[3])})
				}
				if len(words) > 0 {
					lines = append(lines, words)
				}
			}
			blk.text = blockText(lines)
			page.blocks = append(page.blocks, blk)
		}
		pages = append(pages, page)
	}
	return pages
}

type figureRegion struct {
	left, top, right, bottom float64
	caption                  string
}

// figureRegions returns, for one page, the area above each figure caption.
func figureRegions(page layoutPage) []figureRegion {
	heights := []float64{}
	for _, b := range page.blocks {
		for i := 0; i < b.lines; i++ {
			heights = append(heights, b.lineHeight)
		}
	}
	sort.Float64s(heights)
	median := 10.0
	if len(heights) > 0 {
		median = heights[len(heights)/2]
	}
	margin, mid := 36.0, page.width/2
	out := []figureRegion{}
	for ci, c := range page.blocks {
		if !figureCaption.MatchString(c.text) {
			continue
		}
		left, right := margin, page.width-margin
		switch {
		case (c.x0 < mid-20 && c.x1 > mid+20) || c.x1-c.x0 > 0.55*page.width:
		case c.x1 <= mid+10:
			right = mid - 6
		default:
			left = mid + 6
		}
		bound := func(strict bool) float64 {
			top := 0.0
			for bi, b := range page.blocks {
				if bi == ci || b.y1 > c.y0+1 || min(b.x1, right)-max(b.x0, left) < 20 {
					continue
				}
				body := b.lines >= 2 && b.x1-b.x0 >= 0.6*(right-left) && b.lineHeight >= 0.9*median
				if anyCaption.MatchString(b.text) || b.y1 < 0.1*page.height || (strict && body) {
					top = max(top, b.y1+2)
				}
			}
			return top
		}
		bottom := c.y0 - 2
		top := bound(true)
		if bottom-top < 30 {
			top = bound(false)
		}
		if bottom-top < 30 {
			continue
		}
		caption := RepairLigatures(c.text)
		if r := []rune(caption); len(r) > 300 {
			caption = string(r[:300]) + "…"
		}
		out = append(out, figureRegion{left: left, top: top, right: right, bottom: bottom, caption: caption})
	}
	return out
}

// trimFigure narrows a crop to the figure: scanning up from the caption, it
// stops at the first tall blank band (the gap between body text and figure).
func trimFigure(img image.Image, r image.Rectangle) (image.Rectangle, bool) {
	blank := func(y int) bool {
		for x := r.Min.X; x < r.Max.X; x += 2 {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			if (cr+cg+cb)/3 < 245*257 {
				return false
			}
		}
		return true
	}
	gap := 16 * figureDPI / 72
	y := r.Max.Y - 1
	for y > r.Min.Y && blank(y) {
		y--
	}
	end, run, cut := y, 0, r.Min.Y
	for ; y >= r.Min.Y; y-- {
		if blank(y) {
			run++
			if run >= gap {
				cut = y + run
				break
			}
		} else {
			run = 0
		}
	}
	for cut < end && blank(cut) {
		cut++
	}
	if end-cut < 30*figureDPI/72 {
		return r, false
	}
	return image.Rect(r.Min.X, cut, r.Max.X, end+1), true
}

type subImager interface {
	SubImage(image.Rectangle) image.Image
}

func extractFigures(ctx context.Context, s *store.Store, file, id, tmp string) ([]domain.Image, error) {
	layoutFile := filepath.Join(tmp, "layout.html")
	if _, err := Command(ctx, "pdftotext", "-bbox-layout", "-enc", "UTF-8", file, layoutFile); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(layoutFile)
	if err != nil {
		return nil, err
	}
	images := []domain.Image{}
	scale := float64(figureDPI) / 72
	for i, page := range parseLayout(b) {
		regions := figureRegions(page)
		if len(regions) == 0 {
			continue
		}
		loc := i + 1
		base := filepath.Join(tmp, fmt.Sprintf("figpage-%d", loc))
		if _, err = Command(ctx, "pdftoppm", "-f", strconv.Itoa(loc), "-l", strconv.Itoa(loc), "-r", strconv.Itoa(figureDPI), "-singlefile", "-png", file, base); err != nil {
			continue
		}
		raw, err := os.ReadFile(base + ".png")
		if err != nil {
			continue
		}
		rendered, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		sub, ok := rendered.(subImager)
		if !ok {
			continue
		}
		for _, region := range regions {
			rect := image.Rect(int(region.left*scale), int(region.top*scale), int(region.right*scale), int(region.bottom*scale)).Intersect(rendered.Bounds())
			rect, ok = trimFigure(rendered, rect)
			if !ok {
				continue
			}
			var buf bytes.Buffer
			if png.Encode(&buf, sub.SubImage(rect)) != nil {
				continue
			}
			rel, _, err := s.Blob(buf.Bytes(), ".png")
			if err == nil {
				images = append(images, domain.Image{Path: rel, Citation: fmt.Sprintf("%s:p%d", id, loc), Original: true, Caption: region.caption})
			}
		}
	}
	return images, nil
}

// embeddedImages is the fallback for documents without figure captions (for
// example slides exported to PDF). Masked images are usually icons composited
// over vector art, so they are skipped along with small images.
func embeddedImages(ctx context.Context, s *store.Store, file, id, tmp string) ([]domain.Image, error) {
	listing, err := Command(ctx, "pdfimages", "-list", file)
	if err != nil {
		return nil, err
	}
	if _, err = Command(ctx, "pdfimages", "-png", file, filepath.Join(tmp, "figure")); err != nil {
		return nil, err
	}
	rows := strings.Split(string(listing), "\n")
	files, _ := filepath.Glob(filepath.Join(tmp, "figure-*.png"))
	sort.Strings(files)
	images := []domain.Image{}
	for _, f := range files {
		n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "figure-"), ".png"))
		if n+2 >= len(rows) {
			continue
		}
		fields := strings.Fields(rows[n+2])
		if len(fields) < 5 || fields[2] != "image" {
			continue
		}
		if next := strings.Fields(rows[min(n+3, len(rows)-1)]); len(next) > 2 && next[2] == "smask" {
			continue
		}
		w, _ := strconv.Atoi(fields[3])
		h, _ := strconv.Atoi(fields[4])
		if w < 200 || h < 150 {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		rel, _, err := s.Blob(b, ".png")
		if err == nil {
			images = append(images, domain.Image{Path: rel, Citation: id + ":p" + fields[0], Original: true})
		}
	}
	return images, nil
}

// needsFormulaReview flags pages with display equations rather than any page
// containing an "=" sign. In reading-order text an equation number such as
// "(3)" sits on its own line, so it counts when the page also has an "=" or
// several math-italic letters (Unicode Mathematical Alphanumeric Symbols).
func needsFormulaReview(text string) bool {
	if strings.ContainsAny(text, "∑∫∇∂√∏") || numberedFormula.MatchString(text) {
		return true
	}
	if !equationNumberLine.MatchString(text) {
		return false
	}
	math := 0
	for _, r := range text {
		if r >= 0x1D400 && r <= 0x1D7FF {
			math++
		}
	}
	return strings.Contains(text, "=") || math >= 10
}
