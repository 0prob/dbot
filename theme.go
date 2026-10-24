package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var (
	colAccent = color.NRGBA{0xF5, 0xB3, 0x01, 0xFF}
	colWin    = color.NRGBA{0x22, 0xC5, 0x5E, 0xFF}
	colLoss   = color.NRGBA{0xEF, 0x44, 0x44, 0xFF}
	colMuted  = color.NRGBA{0x8B, 0x92, 0xA5, 0xFF}
	colBg     = color.NRGBA{0x0F, 0x11, 0x17, 0xFF}
	colPanel  = color.NRGBA{0x17, 0x1A, 0x23, 0xFF}
	colInput  = color.NRGBA{0x20, 0x24, 0x30, 0xFF}
	colLine   = color.NRGBA{0x2A, 0x2F, 0x3D, 0xFF}
)

// duckTheme is a fixed dark theme with an amber accent. All sizes are
// multiplied by scale so the UI can grow and shrink with the window.
type duckTheme struct{ scale float32 }

func (duckTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return colBg
	case theme.ColorNameHeaderBackground, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return colPanel
	case theme.ColorNameInputBackground, theme.ColorNameButton:
		return colInput
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return colLine
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		return colAccent
	case theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnWarning:
		return colBg
	case theme.ColorNameSuccess:
		return colWin
	case theme.ColorNameError:
		return colLoss
	case theme.ColorNameSelection:
		return color.NRGBA{0xF5, 0xB3, 0x01, 0x40}
	case theme.ColorNamePlaceHolder:
		return colMuted
	}
	return theme.DefaultTheme().Color(n, theme.VariantDark)
}

func (duckTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (duckTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (t duckTheme) Size(n fyne.ThemeSizeName) float32 {
	sc := t.scale
	if sc == 0 {
		sc = 1
	}
	switch n {
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 6 * sc
	}
	return theme.DefaultTheme().Size(n) * sc
}
