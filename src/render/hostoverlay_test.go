package render

import "testing"

func TestSetBackgroundOpacity(t *testing.T) {
	r := &Renderer{theme: ThemeByName("ghostty")}
	r.SetBackgroundOpacity(0.8)
	if r.theme.Background[3] != 0.8 || !r.ConsumeUIDirty() {
		t.Fatalf("alpha=%v", r.theme.Background[3])
	}
	r.SetBackgroundOpacity(0) // invalid: opaque
	if r.theme.Background[3] != 1 {
		t.Fatalf("alpha=%v", r.theme.Background[3])
	}
	if p := ThemeByName("ghostty").Palette16(); p != standard16 {
		t.Fatal("theme without ANSI must use the standard palette")
	}
}
