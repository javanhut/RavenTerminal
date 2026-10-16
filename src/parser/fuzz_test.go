package parser

import "testing"

// FuzzProcess feeds hostile PTY output to the parser, split at an arbitrary
// point, with a resize in between, and renders a frame: none may panic or hang.
func FuzzProcess(f *testing.F) {
	for _, s := range []string{
		"\x1b[31mhi\x1b[0m\r\n", "\x1b]52;c;?\x07", "\x1b]8;;http://x\x07l\x1b]8;;\x07",
		"\x1b_Ga=T,f=24,s=1,v=1;AAAA\x1b\\", "\x1b_Ga=T,t=f;L2V0Yy9wYXNzd2Q=\x1b\\",
		"\x1bPq#0;2;0;0;0#0!10~-\x1b\\", "\x1b[?1049h\x1b[2;5r\x1b[5L\x1b[?1049l",
		"\x1b[>4;2m\x1b[=1u\x1b[?u", "\x1b]1337;SetUserVar=a=Yg==\x07", "\x1b]777;notify;t;b\x07",
		"\x1b]99;i=1:d=0;x\x1b\\", "\x1b[99999999999;99999999999H\x1b[99999@", "é\U0001F600‍",
		"\x1bP+q544e\x1b\\", "\x1b[3J\x1b#8\x1b[?3h", "\x1b]133;A\x07\x1b]133;D;0\x07",
		"\x1b[9223372036854775807S", "\x1b[300000000T", "x\x1b[2000000000b",
		"\x1b_Ga=T,f=24,s=1,v=1,r=2000000000;AAAA\x1b\\", "\x1b_Ga=T,f=24,s=1,v=1,h=2000000000;AAAA\x1b\\",
		"\x1b_Ga=T,f=24,s=1,v=1,c=9223372036854775807;AAAA\x1b\\", "\x1bPq\"1;1;8192;8192#0~\x1b\\",
		"\x1bP+q\r\x15$'\\164'\x1b\\", "\x1b]99;i=\r\x15x:p=?;\x07", "\x1bPzz\x1b[31mHello",
	} {
		f.Add([]byte(s), uint16(3), uint8(10), uint8(5))
	}
	f.Fuzz(func(t *testing.T, data []byte, split uint16, cols, rows uint8) {
		term := NewTerminal(20, 6)
		term.SetResponseWriter(func([]byte) {})
		term.SetClipboardWriter(func(string) {})
		term.SetClipboardReader(func() string { return "secret" })
		k := int(split) % (len(data) + 1)
		term.Process(data[:k])
		term.Resize(int(cols%200)+1, int(rows%100)+1)
		term.Process(data[k:])
		term.Process(data)
		_ = term.Snapshot()
		var fr RenderFrame
		term.RenderFrame(&fr)
		term.DrainEvents()
	})
}
