package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed assets/icon.png
var iconPNG []byte

// appIcon is the window/taskbar icon (a die on the DuckDice amber).
var appIcon = fyne.NewStaticResource("icon.png", iconPNG)
