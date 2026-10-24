package main

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

const (
	minScale = 0.3
	maxScale = 3.0
)

// designSize is the window content size the UI is designed for at scale 1.
// The whole UI is zoomed uniformly from this size, so it looks identical,
// only smaller or larger, at any window size.
var designSize = fyne.NewSize(1720, 1060)

// uiScale is the current UI scale factor. UI goroutine only.
var uiScale float32 = 1

// scaledTexts remembers each canvas.Text's unscaled size so it can be
// resized when the scale changes. canvas.Text does not follow the theme.
var scaledTexts = map[*canvas.Text]float32{}

var scaledSpacers = map[*canvas.Rectangle]float32{}

// scaledSize is a fixed design size multiplied by the current scale.
func scaledSize(w, h float32) fyne.Size { return fyne.NewSize(w*uiScale, h*uiScale) }

func scaledText(s string, c color.Color, size float32) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size * uiScale
	scaledTexts[t] = size
	return t
}

// spacer is a vertical gap of h design units that follows the scale.
func spacer(h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(scaledSize(0, h))
	scaledSpacers[r] = h
	return r
}

// fitToWindow zooms content uniformly: it is laid out at designSize × scale,
// centred, where scale is the largest that fits the window. Spare space on
// one axis (when the window's aspect ratio differs) is left as background.
func fitToWindow(a fyne.App, content fyne.CanvasObject) fyne.CanvasObject {
	f := &fitLayout{app: a, scale: 1}
	return &fyne.Container{Layout: f, Objects: []fyne.CanvasObject{content}}
}

type fitLayout struct {
	app   fyne.App
	scale float32
	want  float32
	timer *time.Timer
}

func (f *fitLayout) target(size fyne.Size) float32 {
	s := min(size.Width/designSize.Width, size.Height/designSize.Height)
	return max(minScale, min(maxScale, s))
}

func (f *fitLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(designSize.Width*minScale, designSize.Height*minScale)
}

func (f *fitLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	s := f.target(size)
	cs := fyne.NewSize(designSize.Width*s, designSize.Height*s)
	for _, o := range objs {
		o.Move(fyne.NewPos((size.Width-cs.Width)/2, (size.Height-cs.Height)/2))
		o.Resize(cs)
	}
	if math.Abs(float64(s-f.scale)) < 0.005 {
		return
	}
	// Debounce: re-theming refreshes every widget, so wait for the drag to settle.
	f.want = s
	if f.timer != nil {
		f.timer.Reset(60 * time.Millisecond)
		return
	}
	f.timer = time.AfterFunc(60*time.Millisecond, func() {
		fyne.Do(func() {
			f.timer = nil
			f.apply(f.want)
		})
	})
}

func (f *fitLayout) apply(s float32) {
	f.scale = s
	uiScale = s
	for t, base := range scaledTexts {
		t.TextSize = base * s
	}
	for r, h := range scaledSpacers {
		r.SetMinSize(fyne.NewSize(0, h*s))
	}
	f.app.Settings().SetTheme(duckTheme{scale: s}) // refreshes everything, texts included
}

// columns lays objects out side by side at fixed width ratios, full height.
type columns []float32

func (c columns) MinSize(objs []fyne.CanvasObject) fyne.Size {
	pad := theme.Padding()
	var w, h float32
	for i, o := range objs {
		m := o.MinSize()
		w = max(w, m.Width/c[i])
		h = max(h, m.Height)
	}
	return fyne.NewSize(w+pad*float32(len(objs)-1), h)
}

func (c columns) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	pad := theme.Padding()
	avail := size.Width - pad*float32(len(objs)-1)
	x := float32(0)
	for i, o := range objs {
		w := avail * c[i]
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(w, size.Height))
		x += w + pad
	}
}
