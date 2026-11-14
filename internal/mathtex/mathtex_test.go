package mathtex

import "testing"

func TestSizedUsesPoints(t *testing.T) {
	r := Rendered{SVG: `<svg xmlns="http://www.w3.org/2000/svg" width="12.5ex" height="2ex" role="img" viewBox="0 -750 5400 950"><g/></svg>`, Width: 12.5, Height: 2}
	if got, want := Sized(r, 4), `<svg xmlns="http://www.w3.org/2000/svg" width="50.00pt" height="8.00pt" role="img" viewBox="0 -750 5400 950"><g/></svg>`; got != want {
		t.Fatalf("Sized = %s\nwant    %s", got, want)
	}
}
