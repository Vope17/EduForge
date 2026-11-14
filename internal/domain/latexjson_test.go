package domain

import (
	"encoding/json"
	"testing"
)

func TestLatexInModelJSONSurvives(t *testing.T) {
	raw := `{"body":"權重 $\alpha$ 與 $\frac{a}{b}$、$\theta_i$、$\nabla f$ 與 $\text{Rel}$\n研究\nThe end\t.\"q\" \\ $\{x\}$ é $\underline{x}$ 價格 \$5","ok":"$\\beta$"}`
	var got struct{ Body, Ok string }
	if err := json.Unmarshal([]byte(RepairLatexJSON(raw)), &got); err != nil {
		t.Fatal(err)
	}
	want := "權重 $\\alpha$ 與 $\\frac{a}{b}$、$\\theta_i$、$\\nabla f$ 與 $\\text{Rel}$\n研究\nThe end\t.\"q\" \\ $\\{x\\}$ é $\\underline{x}$ 價格 \\$5"
	if got.Body != want {
		t.Fatalf("body = %q\nwant  %q", got.Body, want)
	}
	if got.Ok != `$\beta$` {
		t.Fatalf("already escaped LaTeX is kept: %q", got.Ok)
	}
}
