package domain

import "strings"

// Models often write LaTeX inside JSON strings with single backslashes:
// "\alpha" is an invalid JSON escape, and "\frac", "\theta" or "\nabla" are
// read as a form feed, tab or newline followed by letters, silently breaking
// the formula. RepairLatexJSON doubles those backslashes before the reply is
// parsed: every invalid escape, and a valid-looking escape that starts a
// known LaTeX command (\frac but not "\nThe" or "\n研究").

var latexCommands = map[string]bool{}

func init() {
	for _, name := range strings.Fields(`beta bar boldsymbol big bigl bigr bigg Big begin binom bot bullet bmod bf bigcup bigcap bigoplus bigotimes backslash breve
		frac forall flat footnotesize
		nabla neq ne nu not notin neg newline norm nolimits ni nleq ngeq nmid
		rho right rangle rceil rfloor rightarrow rightleftharpoons rm rbrace rbrack
		text textbf textit textrm texttt textsf times tau theta tilde to top tfrac triangle triangleq tanh tan therefore tiny
		underline upsilon uparrow updownarrow underbrace`) {
		latexCommands[name] = true
	}
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// RepairLatexJSON returns s with LaTeX backslashes inside JSON escaped.
func RepairLatexJSON(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		next := s[i+1]
		end := i + 1
		for end < len(s) && isLetter(s[end]) {
			end++
		}
		command := latexCommands[s[i+1:end]]
		switch {
		case next == '\\' || next == '"' || next == '/':
			b.WriteByte(c)
			b.WriteByte(next)
			i++
		case next == 'u' && i+5 < len(s) && isHex(s[i+2]) && isHex(s[i+3]) && isHex(s[i+4]) && isHex(s[i+5]) && !command:
			b.WriteByte(c)
		case strings.IndexByte("bfnrt", next) >= 0 && !command:
			b.WriteByte(c)
		default:
			b.WriteString(`\\`)
		}
	}
	return b.String()
}
