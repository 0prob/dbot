package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

const chartMaxPoints = 600

// profitChart draws cumulative session profit as a line graph.
// It must only be touched from the UI goroutine.
type profitChart struct {
	widget.BaseWidget
	pts []float64
}

func newProfitChart() *profitChart {
	c := &profitChart{pts: []float64{0}}
	c.ExtendBaseWidget(c)
	return c
}

func (c *profitChart) Add(v float64) {
	c.pts = append(c.pts, v)
	if len(c.pts) > chartMaxPoints {
		c.pts = c.pts[len(c.pts)-chartMaxPoints:]
	}
	c.Refresh()
}

func (c *profitChart) Reset() {
	c.pts = []float64{0}
	c.Refresh()
}

func (c *profitChart) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(colPanel)
	bg.CornerRadius = 8
	zero := canvas.NewLine(colLine)
	zero.StrokeWidth = 1
	return &chartRenderer{c: c, bg: bg, zero: zero}
}

type chartRenderer struct {
	c     *profitChart
	bg    *canvas.Rectangle
	zero  *canvas.Line
	lines []fyne.CanvasObject
	size  fyne.Size
}

func (r *chartRenderer) Layout(s fyne.Size) {
	r.size = s
	r.bg.Resize(s)
	r.rebuild()
}

func (r *chartRenderer) MinSize() fyne.Size { return scaledSize(240, 140) }

func (r *chartRenderer) Refresh() {
	r.rebuild()
	canvas.Refresh(r.c)
}

func (r *chartRenderer) Objects() []fyne.CanvasObject {
	return append([]fyne.CanvasObject{r.bg, r.zero}, r.lines...)
}

func (r *chartRenderer) Destroy() {}

func (r *chartRenderer) rebuild() {
	pts := r.c.pts
	pad := 10 * uiScale
	w, h := r.size.Width-2*pad, r.size.Height-2*pad
	if w <= 0 || h <= 0 {
		return
	}
	lo, hi := 0.0, 0.0
	for _, v := range pts {
		lo, hi = min(lo, v), max(hi, v)
	}
	span := hi - lo
	if span == 0 {
		span = 1
	}
	y := func(v float64) float32 { return pad + h - float32((v-lo)/span)*h }
	x := func(i int) float32 {
		if len(pts) < 2 {
			return pad
		}
		return pad + float32(i)/float32(len(pts)-1)*w
	}

	zy := y(0)
	r.zero.Position1 = fyne.NewPos(pad, zy)
	r.zero.Position2 = fyne.NewPos(pad+w, zy)

	n := max(len(pts)-1, 0)
	for len(r.lines) < n {
		l := canvas.NewLine(colWin)
		l.StrokeWidth = 2
		r.lines = append(r.lines, l)
	}
	r.lines = r.lines[:n]
	for i := 0; i < n; i++ {
		l := r.lines[i].(*canvas.Line)
		l.Position1 = fyne.NewPos(x(i), y(pts[i]))
		l.Position2 = fyne.NewPos(x(i+1), y(pts[i+1]))
		if pts[i+1] >= 0 {
			l.StrokeColor = colWin
		} else {
			l.StrokeColor = colLoss
		}
		l.Refresh()
	}
	r.zero.Refresh()
}
