package artifacts

import (
	"context"
	"regexp"
	"strings"
	"time"

	"eduforge/internal/mathtex"
)

// Formulas in output text are TeX: inline between $…$ (or \(…\)), and on a
// line of their own between $$…$$ (or \[…\]). Readers render them as SVG
// (HTML, PDF, Word) or PNG (Word templates); slides show a slide's key
// formula as a picture and turn inline TeX in points into readable text.

// Math renders formulas; nil shows them as TeX source.
var Math mathtex.Renderer

// mathPiece is a run of text or a formula.
type mathPiece struct {
	Text    string
	TeX     string
	Display bool
}

func (p mathPiece) isMath() bool { return p.TeX != "" }

var (
	displayLine = regexp.MustCompile(`^\s*(?:\$\$([\s\S]+?)\$\$|\\\[([\s\S]+?)\\\])\s*$`)
	inlineMath  = regexp.MustCompile(`\\\((.+?)\\\)|\$([^\s$](?:[^$]*[^\s$\\])?)\$`)
)

// displayFormula returns the TeX of a line that is only a display formula.
func displayFormula(line string) (string, bool) {
	m := displayLine.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	tex := strings.TrimSpace(m[1] + m[2])
	return tex, tex != ""
}

// splitMath splits one line into text and formulas. A "$" followed by a
// digit after the closing one (prices such as $5 and $10) is not a formula,
// and \$ is a literal dollar sign.
func splitMath(line string) []mathPiece {
	if tex, ok := displayFormula(line); ok {
		return []mathPiece{{TeX: tex, Display: true}}
	}
	if !strings.Contains(line, "$") && !strings.Contains(line, `\(`) {
		return []mathPiece{{Text: line}}
	}
	out := []mathPiece{}
	last := 0
	for _, m := range inlineMath.FindAllStringSubmatchIndex(line, -1) {
		start, end := m[0], m[1]
		if start > 0 && line[start-1] == '\\' {
			continue
		}
		if end < len(line) && line[end] >= '0' && line[end] <= '9' {
			continue
		}
		tex := ""
		if m[2] >= 0 {
			tex = line[m[2]:m[3]]
		} else {
			tex = line[m[4]:m[5]]
		}
		if start > last {
			out = append(out, mathPiece{Text: line[last:start]})
		}
		out = append(out, mathPiece{TeX: strings.TrimSpace(tex)})
		last = end
	}
	if last < len(line) {
		out = append(out, mathPiece{Text: line[last:]})
	}
	for i := range out {
		if !out[i].isMath() {
			out[i].Text = strings.ReplaceAll(out[i].Text, `\$`, "$")
		}
	}
	return out
}

// formulasIn lists the formulas in texts, each once.
func formulasIn(texts ...string) []mathtex.Formula {
	out := []mathtex.Formula{}
	seen := map[mathtex.Formula]bool{}
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			for _, p := range splitMath(line) {
				f := mathtex.Formula{TeX: p.TeX, Display: p.Display}
				if p.isMath() && !seen[f] {
					seen[f] = true
					out = append(out, f)
				}
			}
		}
	}
	return out
}

// renderedSet holds the rendered formulas of a document.
type renderedSet map[mathtex.Formula]mathtex.Rendered

// renderFormulas renders every formula in texts; formulas that fail (or no
// renderer) are missing from the set and shown as TeX source.
func renderFormulas(texts ...string) renderedSet {
	out := renderedSet{}
	formulas := formulasIn(texts...)
	if Math == nil || len(formulas) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	results, err := Math.Render(ctx, formulas)
	if err != nil {
		return out
	}
	for i, f := range formulas {
		if results[i].Error == "" && results[i].SVG != "" {
			out[f] = results[i]
		}
	}
	return out
}

func (s renderedSet) get(p mathPiece) (mathtex.Rendered, bool) {
	r, ok := s[mathtex.Formula{TeX: p.TeX, Display: p.Display}]
	return r, ok
}

// formulaPNG renders one formula as a PNG for slides and Word templates.
func formulaPNG(tex string, display bool, color string, scale float64) ([]byte, mathtex.Rendered, bool) {
	if Math == nil || strings.TrimSpace(tex) == "" {
		return nil, mathtex.Rendered{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	results, err := Math.Render(ctx, []mathtex.Formula{{TeX: tex, Display: display}})
	if err != nil || len(results) != 1 || results[0].Error != "" {
		return nil, mathtex.Rendered{}, false
	}
	b, err := Math.PNG(ctx, results[0], color, scale)
	if err != nil {
		return nil, mathtex.Rendered{}, false
	}
	return b, results[0], true
}

// ---- TeX as readable text (slide points, notes, plain-text exports) ----

var texSymbols = map[string]string{
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "varepsilon": "ε", "zeta": "ζ", "eta": "η", "theta": "θ", "vartheta": "ϑ",
	"iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "pi": "π", "rho": "ρ", "sigma": "σ", "tau": "τ", "upsilon": "υ",
	"phi": "φ", "varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω", "Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π",
	"Sigma": "Σ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω", "cdot": "·", "times": "×", "div": "÷", "pm": "±", "mp": "∓", "le": "≤", "leq": "≤", "ge": "≥",
	"geq": "≥", "neq": "≠", "ne": "≠", "approx": "≈", "sim": "∼", "equiv": "≡", "propto": "∝", "infty": "∞", "sum": "Σ", "prod": "∏", "int": "∫",
	"partial": "∂", "nabla": "∇", "in": "∈", "notin": "∉", "subset": "⊂", "subseteq": "⊆", "cup": "∪", "cap": "∩", "forall": "∀", "exists": "∃",
	"rightarrow": "→", "to": "→", "leftarrow": "←", "Rightarrow": "⇒", "Leftarrow": "⇐", "leftrightarrow": "↔", "mapsto": "↦", "ldots": "…",
	"cdots": "⋯", "dots": "…", "log": "log", "exp": "exp", "max": "max", "min": "min", "arg": "arg", "sqrt": "√", "lVert": "‖", "rVert": "‖",
	"|": "‖", "langle": "⟨", "rangle": "⟩", "lfloor": "⌊", "rfloor": "⌋", "lceil": "⌈", "rceil": "⌉", "circ": "∘", "oplus": "⊕", "otimes": "⊗",
	"neg": "¬", "land": "∧", "lor": "∨", "wedge": "∧", "vee": "∨", "top": "⊤", "emptyset": "∅", "ell": "ℓ", "hat": "", "bar": "", "tilde": "",
}

var (
	subDigits   = strings.NewReplacer("0", "₀", "1", "₁", "2", "₂", "3", "₃", "4", "₄", "5", "₅", "6", "₆", "7", "₇", "8", "₈", "9", "₉", "+", "₊", "-", "₋", "=", "₌", "(", "₍", ")", "₎", "i", "ᵢ", "j", "ⱼ", "k", "ₖ", "n", "ₙ", "t", "ₜ", "a", "ₐ", "e", "ₑ", "o", "ₒ", "x", "ₓ", "m", "ₘ", "p", "ₚ", "s", "ₛ", "l", "ₗ", "r", "ᵣ", "u", "ᵤ", "v", "ᵥ")
	supDigits   = strings.NewReplacer("0", "⁰", "1", "¹", "2", "²", "3", "³", "4", "⁴", "5", "⁵", "6", "⁶", "7", "⁷", "8", "⁸", "9", "⁹", "+", "⁺", "-", "⁻", "=", "⁼", "(", "⁽", ")", "⁾", "i", "ⁱ", "n", "ⁿ", "t", "ᵗ", "T", "ᵀ", "k", "ᵏ", "*", "*", "′", "′")
	texFrac     = regexp.MustCompile(`\\[dt]?frac\s*\{([^{}]*)\}\s*\{([^{}]*)\}`)
	texSqrt     = regexp.MustCompile(`\\sqrt\s*\{([^{}]*)\}`)
	texFont     = regexp.MustCompile(`\\(?:mathrm|mathbf|mathit|mathcal|mathbb|mathsf|text|textrm|textit|textbf|operatorname|boldsymbol|mathtt)\s*\{([^{}]*)\}`)
	texAccent   = regexp.MustCompile(`\\(?:hat|bar|tilde|vec|dot|overline|widehat)\s*\{([^{}]*)\}`)
	texScript   = regexp.MustCompile(`([_^])\s*(\{[^{}]*\}|\\[A-Za-z]+|.)`)
	texCommand  = regexp.MustCompile(`\\([A-Za-z]+|.)`)
	texSpacing  = regexp.MustCompile(`\\[,;:! ]|\\quad|\\qquad|~`)
	texSizing   = regexp.MustCompile(`\\(?:left|right|big|Big|bigg|Bigg)[lr]?\b`)
	multiSpaces = regexp.MustCompile(`\s{2,}`)
)

// TexText writes a TeX formula as readable text: Greek letters and symbols
// as Unicode, simple sub- and superscripts as Unicode where possible,
// fractions as a/b and font commands dropped.
func TexText(tex string) string {
	s := texSpacing.ReplaceAllString(tex, " ")
	s = texSizing.ReplaceAllString(s, "")
	for i := 0; i < 4; i++ {
		s = texFont.ReplaceAllString(s, "$1")
		s = texAccent.ReplaceAllString(s, "$1")
		s = texSqrt.ReplaceAllString(s, "√($1)")
		s = texFrac.ReplaceAllStringFunc(s, func(m string) string {
			p := texFrac.FindStringSubmatch(m)
			wrap := func(v string) string {
				if strings.ContainsAny(v, "+-−·× ") {
					return "(" + v + ")"
				}
				return v
			}
			return wrap(p[1]) + "/" + wrap(p[2])
		})
	}
	s = texCommand.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1:]
		if sym, ok := texSymbols[name]; ok {
			return sym
		}
		if len(name) == 1 {
			return name // \{ \} \% \_ …
		}
		return name
	})
	s = texScript.ReplaceAllStringFunc(s, func(m string) string {
		p := texScript.FindStringSubmatch(m)
		body := strings.Trim(p[2], "{}")
		mapped := subDigits
		if p[1] == "^" {
			mapped = supDigits
		}
		out := mapped.Replace(body)
		if utf8Same(out, body) && body != "" {
			// Not every letter has a Unicode sub/superscript: keep the marker.
			if len([]rune(body)) == 1 {
				return p[1] + body
			}
			return p[1] + "(" + body + ")"
		}
		return out
	})
	s = strings.NewReplacer("{", "", "}", "", "&", " ", `\\`, " ").Replace(s)
	return strings.TrimSpace(multiSpaces.ReplaceAllString(s, " "))
}

// utf8Same reports whether mapping left some character unmapped.
func utf8Same(mapped, body string) bool {
	m, b := []rune(mapped), []rune(body)
	if len(m) != len(b) {
		return true
	}
	for i := range m {
		if m[i] == b[i] {
			return true
		}
	}
	return false
}

// MathAsText replaces the formulas in text with readable text.
func MathAsText(text string) string {
	if !strings.Contains(text, "$") && !strings.Contains(text, `\(`) && !strings.Contains(text, `\[`) {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		var b strings.Builder
		for _, p := range splitMath(line) {
			if p.isMath() {
				b.WriteString(TexText(p.TeX))
			} else {
				b.WriteString(p.Text)
			}
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}
