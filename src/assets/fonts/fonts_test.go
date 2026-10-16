package fonts

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
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

func nerdFontsVersion(t *testing.T, data []byte) string {
	t.Helper()
	f, err := sfnt.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v, err := f.Name(nil, sfnt.NameIDVersion)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, after, ok := strings.Cut(v, "Nerd Fonts "); ok {
		return after
	}
	return v
}

// Every embedded style of a family comes from the same Nerd Fonts release.
func TestStylesShareRelease(t *testing.T) {
	for _, fi := range AvailableFonts() {
		want := nerdFontsVersion(t, fi.Data)
		if want != "3.5.1" {
			t.Errorf("%s regular is Nerd Fonts %s, want 3.5.1", fi.Name, want)
		}
		for _, s := range [][2]bool{{true, false}, {false, true}, {true, true}} {
			if data, ok := Variant(fi.Name, s[0], s[1]); ok {
				if got := nerdFontsVersion(t, data); got != want {
					t.Errorf("%s variant %v is Nerd Fonts %s, regular %s", fi.Name, s, got, want)
				}
			}
		}
	}
}

// Variants ship as small deltas against the regular face.
func TestVariantsStoredCompactly(t *testing.T) {
	entries, err := variantFS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, e := range entries {
		info, _ := e.Info()
		total += int(info.Size())
	}
	if len(entries) != 10 {
		t.Errorf("%d embedded variants, want 10", len(entries))
	}
	if total > 2<<20 {
		t.Errorf("embedded variants take %d bytes, want under 2MiB", total)
	}
}

func TestApplyDelta(t *testing.T) {
	base := []byte("0123456789abcdefghij")
	want := []byte("xx456789abYY0123")
	var d []byte
	d = binary.AppendUvarint(d, uint64(len(want)))
	d = binary.LittleEndian.AppendUint32(d, crc32.ChecksumIEEE(want))
	d = binary.AppendUvarint(d, 2)
	d = append(d, "xx"...)
	d = binary.AppendVarint(d, 4)
	d = binary.AppendUvarint(d, 8)
	d = binary.AppendUvarint(d, 2)
	d = append(d, "YY"...)
	d = binary.AppendVarint(d, -12)
	d = binary.AppendUvarint(d, 4)
	d = binary.AppendUvarint(d, 0)
	if got := applyDelta(base, d); !bytes.Equal(got, want) {
		t.Fatalf("applyDelta = %q, want %q", got, want)
	}
	if applyDelta(base[:5], d) != nil {
		t.Errorf("copy past the base accepted")
	}
	bad := append([]byte(nil), d...)
	bad[3] ^= 0xFF
	if applyDelta(base, bad) != nil {
		t.Errorf("checksum mismatch accepted")
	}
	bad = append(append([]byte(nil), d[:len(d)-1]...), 1)
	if applyDelta(base, bad) != nil {
		t.Errorf("truncated delta accepted")
	}
}
