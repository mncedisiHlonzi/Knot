package storymedia

import "testing"

// TestParseVariant pins the `variant` query parameter's contract: the three
// current variants plus the two retired ones a legacy row may still answer, an
// empty value for "the full rendition", and a refusal for anything else.
//
// It lives here (rather than beside the encoders, where it was written) because
// ParseVariant is part of the read path and has nothing to do with encoding
// (KNOT-038).
func TestParseVariant(t *testing.T) {
	cases := []struct {
		raw    string
		want   Variant
		wantOK bool
	}{
		{raw: "", want: "", wantOK: true},
		{raw: "thumb", want: VariantThumb, wantOK: true},
		{raw: "display", want: VariantDisplay, wantOK: true},
		{raw: "stream", want: VariantStream, wantOK: true},
		{raw: " medium ", want: VariantMedium, wantOK: true},
		{raw: "poster", want: VariantPoster, wantOK: true},
		{raw: "huge", want: "", wantOK: false},
		{raw: "original", want: "", wantOK: false},
	}

	for _, testCase := range cases {
		got, ok := ParseVariant(testCase.raw)
		if got != testCase.want || ok != testCase.wantOK {
			t.Errorf("ParseVariant(%q) = %q, %v, want %q, %v", testCase.raw, got, ok, testCase.want, testCase.wantOK)
		}
	}
}
