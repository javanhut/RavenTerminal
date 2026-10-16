package main

import (
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/render"
)

// configDelta says which groups of settings changed between two configs.
// Settings read live every frame (bell, notifications, cursor blink)
// need no delta.
type configDelta struct {
	theme, font, textStyle, scrollback, panels, clipboard, motion, cursor bool
}

func allChanged() configDelta {
	return configDelta{true, true, true, true, true, true, true, true}
}

func diffConfig(old, cur *config.Config) configDelta {
	if old == nil {
		return allChanged()
	}
	oa, ca := old.Appearance, cur.Appearance
	return configDelta{
		theme: old.Theme != cur.Theme || old.Opacity() != cur.Opacity(),
		font:  old.FontSize != cur.FontSize,
		textStyle: oa.FauxBold != ca.FauxBold || oa.FauxItalic != ca.FauxItalic || oa.Undercurl != ca.Undercurl || oa.Ligatures != ca.Ligatures ||
			oa.TextGamma != ca.TextGamma || oa.TextContrast != ca.TextContrast,
		scrollback: old.Scrollback() != cur.Scrollback(),
		panels: old.WebSearch.Enabled != cur.WebSearch.Enabled || old.WebSearch.UseReaderProxy != cur.WebSearch.UseReaderProxy ||
			old.Ollama != cur.Ollama || oa.PanelWidthPercent != ca.PanelWidthPercent,
		clipboard: old.AllowClipboardRead != cur.AllowClipboardRead,
		motion:    oa.ReducedMotion != ca.ReducedMotion || oa.MarksGutter != ca.MarksGutter,
		cursor:    oa.CursorStyle != ca.CursorStyle,
	}
}

// applyConfig applies what changed from old (nil: everything) to cfg.
func (a *App) applyConfig(old, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	d := diffConfig(old, cfg)
	snapshot := *cfg
	a.applied = &snapshot
	if d.panels {
		a.searchPanel.SetEnabled(cfg.WebSearch.Enabled)
		a.aiPanel.SetEnabled(cfg.Ollama.Enabled)
		a.searchPanel.WidthPercent = cfg.Appearance.PanelWidthPercent
		a.aiPanel.WidthPercent = cfg.Appearance.PanelWidthPercent
		a.aiPanel.ShowThinking = cfg.Ollama.ShowThinking
		a.aiPanel.ThinkingMode = cfg.Ollama.ThinkingMode
		a.settingsMenu.OllamaModels = nil
		if a.aiPanel.LoadedURL != cfg.Ollama.URL || a.aiPanel.LoadedModel != cfg.Ollama.Model {
			a.aiPanel.ModelLoaded = false
			a.aiPanel.LoadedURL = cfg.Ollama.URL
			a.aiPanel.LoadedModel = cfg.Ollama.Model
		}
	}
	if d.theme {
		a.applyTheme(cfg.Theme)
	}
	if d.textStyle {
		a.renderer.SetTextStyleOptions(cfg.Appearance.FauxBold, cfg.Appearance.FauxItalic, cfg.Appearance.Undercurl)
		a.renderer.SetLigatures(cfg.Appearance.Ligatures)
		a.renderer.SetTextGamma(cfg.Appearance.TextGamma, cfg.Appearance.TextContrast)
	}
	if d.motion {
		a.applyEffects()
	}
	if d.clipboard {
		applyClipboardReadGate(cfg.AllowClipboardRead)
	}
	if d.scrollback || d.cursor {
		a.themeSerial++ // re-seeds every pane: scrollback and cursor style
	}
	var err error
	if d.font {
		err = a.renderer.SetDefaultFontSize(cfg.FontSize)
	}
	a.fitGrid()
	return err
}

// applyTheme switches the renderer's theme and re-seeds every pane's colour
// defaults and dark/light preference.
func (a *App) applyTheme(name string) {
	a.currentTheme = name
	a.renderer.SetThemeByName(name)
	opacity := float32(1)
	if a.applied != nil {
		opacity = a.applied.Opacity()
	}
	a.renderer.SetBackgroundOpacity(opacity)
	a.theme = render.ThemeByName(name)
	a.themeSerial++
}

// reloadConfigFromDisk applies config.toml after an outside edit. While the
// settings menu is open its unsaved edits win; the reload waits for close.
func (a *App) reloadConfigFromDisk() {
	if a.settingsMenu.IsOpen() {
		a.configStale = true
		return
	}
	a.configStale = false
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		a.showToast("config.toml not applied: " + errText(err))
		return
	}
	a.settingsMenu.Config = cfg
	if err := a.applyConfig(a.applied, cfg); err != nil {
		a.showToast("config.toml: " + err.Error())
	}
}

func errText(err error) string {
	if err == nil {
		return "unreadable"
	}
	return err.Error()
}
