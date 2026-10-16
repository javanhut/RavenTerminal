package fonts

import (
	"strings"
	"testing"

	"golang.org/x/image/font/sfnt"
)

func subfamily(t *testing.T, data []byte) string {
	t.Helper()
	f, err := sfnt.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	name, err := f.Name(nil, sfnt.NameIDSubfamily)
	if err != nil {
		t.Fatalf("subfamily: %v", err)
	}
	return name
}

func TestVariantsEmbedded(t *testing.T) {
	cases := []struct {
		font         string
		bold, italic bool
		want         string
	}{
		{"jetbrains", true, false, "Bold"},
		{"jetbrains", false, true, "Italic"},
		{"jetbrains", true, true, "Bold Italic"},
		{"hack", true, false, "Bold"},
		{"hack", true, true, "Bold Italic"},
		{"ubuntu", false, true, "Italic"},
		{"firacode", true, false, "Bold"},
	}
	for _, c := range cases {
		data, ok := Variant(c.font, c.bold, c.italic)
		if !ok {
			t.Fatalf("Variant(%s,%v,%v) missing", c.font, c.bold, c.italic)
		}
		if got := subfamily(t, data); !strings.EqualFold(got, c.want) {
			t.Errorf("Variant(%s,%v,%v) subfamily %q, want %q", c.font, c.bold, c.italic, got, c.want)
		}
		again, _ := Variant(c.font, c.bold, c.italic)
		if &again[0] != &data[0] {
			t.Errorf("Variant(%s) decompressed twice", c.font)
		}
	}
	if _, ok := Variant("firacode", false, true); ok {
		t.Errorf("Fira Code has no italic; Variant must report false")
	}
	if data, ok := Variant("jetbrains", false, false); !ok || &data[0] != &JetBrainsMono[0] {
		t.Errorf("regular variant must be the embedded regular font")
	}
	if _, ok := Variant("nope", true, false); ok {
		t.Errorf("unknown font reported a variant")
	}
}
