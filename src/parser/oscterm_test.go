package parser

import (
	"strings"
	"testing"
	"time"
)

func TestOSCRepliesEchoRequestTerminator(t *testing.T) {
	queries := []string{"4;1;?", "4;200;?", "10;?", "11;?", "12;?", "10;?;?;?", "22;?pointer", "99;i=a:p=?;"}
	for _, q := range queries {
		for _, end := range []string{"\x07", "\x1b\\"} {
			term := NewTerminal(10, 2)
			got := reply(term, "\x1b]"+q+end)
			if got == "" {
				t.Errorf("%q: no reply", q)
				continue
			}
			parts := strings.SplitAfter(got, end)
			for _, p := range parts[:len(parts)-1] {
				if !strings.HasPrefix(p, "\x1b]") || strings.Count(p, "\x07")+strings.Count(p, "\x1b\\") != 1 {
					t.Errorf("%q with %q: bad reply %q", q, end, got)
				}
			}
			if parts[len(parts)-1] != "" {
				t.Errorf("%q with %q: reply %q does not end with the request terminator", q, end, got)
			}
		}
	}
}

func TestOSC52ReplyEchoesST(t *testing.T) {
	term := NewTerminal(10, 2)
	got := make(chan string, 1)
	term.SetResponseWriter(func(b []byte) { got <- string(b) })
	term.SetClipboardReader(func() string { return "hi" })
	feed(term, "\x1b]52;c;?\x1b\\")
	select {
	case s := <-got:
		if s != "\x1b]52;c;aGk=\x1b\\" {
			t.Fatalf("reply = %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reply")
	}
}
