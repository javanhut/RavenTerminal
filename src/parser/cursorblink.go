package parser

// EffectiveBlink decides whether the cursor should currently be drawn as blinking
// (i.e. toggled off on the dark phase). It is a pure function so it can be unit
// tested without a window/GL context. The cursor is solid (never blinks) unless
// every condition holds:
//   - configEnabled: the user's cursor_blink setting is on
//   - styleBlinks:   the active DECSCUSR style requests blinking
//   - focused:       the window has focus
//   - !recentlyTyped: the user hasn't typed within the last blink interval
//     (typing forces the cursor solid for responsiveness)
func EffectiveBlink(configEnabled, styleBlinks, focused, recentlyTyped bool) bool {
	return configEnabled && styleBlinks && focused && !recentlyTyped
}
