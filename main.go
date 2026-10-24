package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"dbot/profile"
)

const (
	houseEdge  = 1.0 // DuckDice pays 99/chance
	maxHistory = 500
	prefKey    = "api_key"
	prefSymbol = "symbol"
)

func main() {
	a := app.NewWithID("io.dbot.duckdice")
	a.Settings().SetTheme(duckTheme{scale: 1})
	a.SetIcon(appIcon)
	w := a.NewWindow("DuckDice Bot")
	// DBOT_MOBILE=1 previews the phone layout on a desktop.
	mobile := fyne.CurrentDevice().IsMobile() || os.Getenv("DBOT_MOBILE") == "1"
	ui := newUI(a, w, mobile)
	content := ui.build()
	if mobile {
		w.SetContent(content) // full screen; no zoom-to-fit
		if !fyne.CurrentDevice().IsMobile() {
			w.Resize(fyne.NewSize(412, 880))
		}
	} else {
		w.SetContent(fitToWindow(a, content))
		w.Resize(fyne.NewSize(designSize.Width*0.8, designSize.Height*0.8))
	}
	w.SetOnClosed(ui.stopAuto)
	go ui.watchPrices(ui.priceKick)
	// Reading the keyring can block on an unlock prompt, so do it off the
	// UI goroutine.
	if !mobile {
		ui.setStatus("Reading API key from the system keyring…")
	}
	go func() {
		key, inKeyring := loadAPIKey(a.Preferences(), !mobile)
		fyne.Do(func() {
			ui.client.Key, ui.keyInKeyring = key, inKeyring
			if key == "" {
				ui.showSettings()
			} else {
				ui.refresh()
			}
		})
	}()
	w.ShowAndRun()
}

// betParams is a snapshot of the bet form, taken on the UI goroutine.
type betParams struct {
	symbol  string
	amount  float64
	rangeOn bool
	chance  string
	isHigh  bool
	from    int
	to      int
	isIn    bool
	bonus   string
	tle     string
}

type autoCfg struct {
	count int
	delay time.Duration
}

type ui struct {
	app    fyne.App
	win    fyne.Window
	client *Client
	info   *UserInfo

	// profile strategy
	profileMgr  *profile.ProfileManager
	profileView *ProfileView
	strategy    *profile.BettingProfile // applied profile that auto-bet runs
	strategyLbl *widget.Label

	// header
	userLbl    *widget.Label
	balanceTxt *canvas.Text
	symbolSel  *widget.Select
	acctLbl    *canvas.Text
	usdBalTxt  *canvas.Text
	usdBetTxt  *canvas.Text
	betHdr     *fyne.Container
	prices     *priceFeed

	// bet form
	tabs      *container.AppTabs
	amount    *widget.Entry
	chance    *widget.Entry
	chanceSl  *widget.Slider
	hiLo      *widget.RadioGroup
	rFrom     *widget.Entry
	rTo       *widget.Entry
	inOut     *widget.RadioGroup
	payoutLbl *widget.Label
	profitLbl *widget.Label
	bonusSel  *widget.Select
	tleSel    *widget.Select
	bonusMap  map[string]string
	tleMap    map[string]string
	betBtn    *widget.Button
	minBtn    *widget.Button

	// auto
	aCount, aDelay *widget.Entry
	autoBtn        *widget.Button
	cancel         context.CancelFunc

	// session
	sBets, sWins, sProfit, sWager *canvas.Text
	sProfitUSD, sWagerUSD         *canvas.Text
	bets, wins                    int
	profit, wagered               float64
	chart                         *profitChart
	history                       []Bet
	list                          *widget.List
	lastRoll                      *canvas.Text

	status       *widget.Label
	keyInKeyring bool
	log          *betLog
	priceKick    chan []string
	mobile       bool
	logErrShown  bool
}

func newUI(a fyne.App, w fyne.Window, mobile bool) *ui {
	// Desktop keeps profiles/ and logs/ next to where it was started; a phone
	// app has no such folder, so use the app's private storage there.
	root := ""
	if fyne.CurrentDevice().IsMobile() {
		root = a.Storage().RootURI().Path()
	}
	u := &ui{
		app:        a,
		win:        w,
		mobile:     mobile,
		client:     NewClient(""), // key loaded from the keyring in main
		log:        openBetLog(root),
		prices:     newPriceFeed(),
		priceKick:  make(chan []string, 1),
		profileMgr: openProfiles(root),
	}
	u.profileView = NewProfileView(u.profileMgr, w, u.applyProfile)
	u.profileView.Balance = u.currentBalance
	u.strategy = u.profileView.Current()
	if u.strategy == nil {
		u.strategy = profile.NewDefaultProfile()
	}
	return u
}

// openProfiles uses <root>/profiles (./profiles when root is ""), falling
// back to the user config dir when that is not writable.
func openProfiles(root string) *profile.ProfileManager {
	pm, err := profile.NewProfileManager(filepath.Join(root, "profiles"))
	if err == nil {
		return pm
	}
	dir, cerr := os.UserConfigDir()
	if cerr != nil {
		dir = os.TempDir()
	}
	fallback := filepath.Join(dir, "duckdice-bot", "profiles")
	fmt.Fprintf(os.Stderr, "profiles: %v; using %s\n", err, fallback)
	if pm, err = profile.NewProfileManager(fallback); err != nil {
		fmt.Fprintf(os.Stderr, "profiles: %v\n", err)
		pm, _ = profile.NewProfileManager(os.TempDir())
	}
	return pm
}

// ---------- layout ----------

func (u *ui) build() fyne.CanvasObject {
	if u.mobile {
		return u.buildMobile()
	}
	return container.NewBorder(u.buildHeader(), u.buildStatus(), nil, nil,
		container.NewPadded(u.buildBody()))
}

// buildMobile is the phone layout: the same panels as the desktop's three
// columns, one per tab, each scrolling on its own.
func (u *ui) buildMobile() fyne.CanvasObject {
	header := u.buildHeader()
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Bet", theme.MediaPlayIcon(),
			container.NewVScroll(container.NewVBox(u.buildBetCard(), u.buildAutoCard()))),
		container.NewTabItemWithIcon("Strategy", theme.DocumentIcon(), u.profileView.MobileCanvasObject()),
		container.NewTabItemWithIcon("Session", theme.HistoryIcon(), container.NewPadded(u.buildSessionPane())),
	)
	tabs.SetTabLocation(container.TabLocationBottom)
	return container.NewBorder(header, u.buildStatus(), nil, nil, tabs)
}

// dialogSize is a design-size dialog, scaled, but never bigger than the window.
func dialogSize(win fyne.Window, w, h float32) fyne.Size {
	s, c := scaledSize(w, h), win.Canvas().Size()
	if c.Width > 0 && c.Height > 0 {
		s = fyne.NewSize(min(s.Width, c.Width*0.96), min(s.Height, c.Height*0.9))
	}
	return s
}

func (u *ui) buildHeader() fyne.CanvasObject {
	logo := scaledText("DuckDice", colAccent, 22)
	logo.TextStyle.Bold = true
	sub := scaledText("BOT", colMuted, 12)
	sub.TextStyle.Bold = true

	u.userLbl = widget.NewLabel("not connected")
	u.userLbl.Importance = widget.LowImportance

	u.balanceTxt = scaledText("—", color.White, 22)
	u.balanceTxt.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}

	u.symbolSel = widget.NewSelect(nil, func(s string) {
		u.app.Preferences().SetString(prefSymbol, s)
		u.updateBalance()
		u.updateBonusOptions()
		u.loadStats()
	})
	u.symbolSel.PlaceHolder = "Currency"

	u.acctLbl = scaledText("", colMuted, 11)
	u.acctLbl.Alignment = fyne.TextAlignTrailing
	u.usdBalTxt = scaledText("", colAccent, 14)
	u.usdBalTxt.TextStyle.Bold = true
	u.usdBalTxt.Alignment = fyne.TextAlignTrailing
	u.balanceTxt.Alignment = fyne.TextAlignTrailing

	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), u.refresh)
	settings := widget.NewButtonWithIcon("", theme.SettingsIcon(), u.showSettings)

	left := container.NewHBox(logo, container.NewVBox(layout.NewSpacer(), sub), u.userLbl)
	right := container.NewHBox(container.NewVBox(u.balanceTxt, u.usdBalTxt, u.acctLbl),
		container.NewCenter(u.symbolSel),
		container.NewCenter(refresh), container.NewCenter(settings))

	bar := container.NewBorder(nil, nil, left, right)
	if u.mobile {
		// Phone: controls on top, balance underneath, no account line.
		u.balanceTxt.TextSize, u.usdBalTxt.Alignment = 20, fyne.TextAlignLeading
		u.balanceTxt.Alignment = fyne.TextAlignLeading
		bar = container.NewVBox(
			container.NewBorder(nil, nil, container.NewHBox(logo, container.NewVBox(layout.NewSpacer(), sub)),
				container.NewHBox(container.NewCenter(u.symbolSel), container.NewCenter(refresh), container.NewCenter(settings))),
			u.balanceTxt, u.usdBalTxt)
	}
	bg := canvas.NewRectangle(colPanel)
	return container.NewStack(bg, container.NewPadded(bar))
}

func (u *ui) buildStatus() fyne.CanvasObject {
	u.status = widget.NewLabel("Ready")
	u.status.Importance = widget.LowImportance
	u.status.Truncation = fyne.TextTruncateEllipsis
	return container.NewStack(canvas.NewRectangle(colPanel), u.status)
}

func (u *ui) buildBody() fyne.CanvasObject {
	// Fixed column ratios (not draggable splits) so the layout keeps the
	// same proportions at every window size.
	left := container.NewVBox(u.buildBetCard(), u.buildAutoCard())
	return &fyne.Container{
		Layout:  columns{0.26, 0.30, 0.44},
		Objects: []fyne.CanvasObject{left, u.profileView.CanvasObject(), u.buildSessionPane()},
	}
}

func (u *ui) buildBetCard() fyne.CanvasObject {
	// amount row
	u.amount = widget.NewEntry()
	u.amount.SetText("0.00000001")
	u.amount.OnChanged = func(string) { u.updatePayout() }
	u.minBtn = widget.NewButton("Min", u.setMinBet)
	half := widget.NewButton("½", func() { u.scaleAmount(0.5) })
	dbl := widget.NewButton("2×", func() { u.scaleAmount(2) })
	amountRow := container.NewBorder(nil, nil, nil, container.NewHBox(u.minBtn, half, dbl), u.amount)

	// dice
	u.chance = widget.NewEntry()
	u.chance.SetText("49.5")
	u.chanceSl = widget.NewSlider(0.01, 98)
	u.chanceSl.Step = 0.01
	u.chanceSl.SetValue(49.5)
	syncing := false
	u.chance.OnChanged = func(s string) {
		if syncing {
			return
		}
		if v, err := strconv.ParseFloat(s, 64); err == nil && v >= 0.01 && v <= 98 {
			syncing = true
			u.chanceSl.SetValue(v)
			syncing = false
		}
		u.updatePayout()
	}
	u.chanceSl.OnChanged = func(v float64) {
		if syncing {
			return
		}
		syncing = true
		u.chance.SetText(trimNum(strconv.FormatFloat(v, 'f', 2, 64)))
		syncing = false
		u.updatePayout()
	}
	u.hiLo = widget.NewRadioGroup([]string{"Low", "High"}, func(string) { u.updatePayout() })
	u.hiLo.Horizontal = true
	u.hiLo.Required = true
	u.hiLo.SetSelected("High")
	quick := container.NewGridWithColumns(5)
	for _, c := range []float64{2, 10, 33, 49.5, 90} {
		c := c
		quick.Add(widget.NewButton(trimNum(fmt.Sprint(c))+"%", func() {
			u.chance.SetText(trimNum(fmt.Sprint(c)))
		}))
	}
	dice := container.NewVBox(
		fieldLabel("Win chance %"), u.chance, u.chanceSl, quick,
		fieldLabel("Roll"), u.hiLo,
	)

	// range dice
	u.rFrom = widget.NewEntry()
	u.rFrom.SetText("0")
	u.rTo = widget.NewEntry()
	u.rTo.SetText("4949")
	u.rFrom.OnChanged = func(string) { u.updatePayout() }
	u.rTo.OnChanged = func(string) { u.updatePayout() }
	u.inOut = widget.NewRadioGroup([]string{"In", "Out"}, func(string) { u.updatePayout() })
	u.inOut.Horizontal = true
	u.inOut.Required = true
	u.inOut.SetSelected("In")
	rng := container.NewVBox(
		fieldLabel("Range (0 – 9999)"),
		container.NewGridWithColumns(2, u.rFrom, u.rTo),
		fieldLabel("Win when roll is"), u.inOut,
	)

	u.tabs = container.NewAppTabs(
		container.NewTabItem("Original Dice", dice),
		container.NewTabItem("Range Dice", rng),
	)
	u.tabs.OnSelected = func(*container.TabItem) { u.updatePayout() }

	u.payoutLbl = widget.NewLabel("")
	u.payoutLbl.TextStyle.Monospace = true
	u.profitLbl = widget.NewLabel("")
	u.profitLbl.TextStyle.Monospace = true
	u.usdBetTxt = scaledText("", colAccent, 12)
	u.usdBetTxt.TextStyle.Bold = true
	u.betHdr = container.NewHBox(fieldLabel("Bet amount"), layout.NewSpacer(), u.usdBetTxt)
	info := container.NewGridWithColumns(2,
		container.NewVBox(fieldLabel("Payout"), u.payoutLbl),
		container.NewVBox(fieldLabel("Profit on win"), u.profitLbl))

	u.bonusSel = widget.NewSelect([]string{"None"}, nil)
	u.bonusSel.SetSelected("None")
	u.tleSel = widget.NewSelect([]string{"None"}, nil)
	u.tleSel.SetSelected("None")
	adv := widget.NewAccordion(widget.NewAccordionItem("Bonus / TLE",
		container.NewVBox(fieldLabel("Wagering bonus"), u.bonusSel, fieldLabel("Time-limited event"), u.tleSel)))

	u.betBtn = widget.NewButtonWithIcon("BET", theme.MediaPlayIcon(), u.manualBet)
	u.betBtn.Importance = widget.HighImportance

	u.lastRoll = scaledText("", colMuted, 28)
	u.lastRoll.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	u.lastRoll.Alignment = fyne.TextAlignCenter

	u.updatePayout()
	return widget.NewCard("", "", container.NewVBox(
		u.betHdr, amountRow,
		u.tabs, info, adv,
		spacer(4),
		u.betBtn,
		u.lastRoll,
	))
}

func (u *ui) buildAutoCard() fyne.CanvasObject {
	num := func(v string) *widget.Entry { e := widget.NewEntry(); e.SetText(v); return e }
	u.aCount = num("0")
	u.aDelay = num("250")

	u.strategyLbl = widget.NewLabel("")
	u.strategyLbl.Wrapping = fyne.TextWrapWord
	u.updateStrategyLabel()

	u.autoBtn = widget.NewButtonWithIcon("Start auto-bet", theme.MediaFastForwardIcon(), u.toggleAuto)
	u.autoBtn.Importance = widget.SuccessImportance

	grid := container.NewGridWithColumns(2,
		container.NewVBox(fieldLabel("Number of bets (0 = ∞)"), u.aCount),
		container.NewVBox(fieldLabel("Delay (ms)"), u.aDelay),
	)
	return widget.NewCard("Auto-bet", "", container.NewVBox(
		fieldLabel("Strategy (Original Dice, Roll Low/High above)"),
		u.strategyLbl,
		grid, u.autoBtn))
}

// applyProfile makes p the strategy auto-bet runs and loads its chance and
// base bet into the bet form.
func (u *ui) applyProfile(p *profile.BettingProfile) {
	if u.cancel != nil {
		dialog.ShowInformation("Auto-bet running", "Stop auto-bet before applying another profile.", u.win)
		return
	}
	if p.Symbol != "" && p.Symbol != u.symbolSel.Selected {
		if !contains(u.symbolSel.Options, p.Symbol) {
			dialog.ShowInformation("No balance", fmt.Sprintf("Profile %q is for %s, which this account has no balance in.", p.Name, p.Symbol), u.win)
			return
		}
		u.symbolSel.SetSelected(p.Symbol)
	}
	u.strategy = p.Clone()
	u.updateStrategyLabel()
	u.tabs.SelectIndex(0)
	u.chance.SetText(trimNum(strconv.FormatFloat(p.Chance, 'f', 2, 64)))
	u.amount.SetText(fmtAmt(p.Base(u.currentBalance())))
	u.setStatus("Applied strategy " + p.Name)
}

func (u *ui) updateStrategyLabel() {
	if u.strategyLbl == nil || u.strategy == nil {
		return
	}
	p := u.strategy
	base := fmtAmt(p.BaseBet)
	if p.BankrollPercent > 0 {
		base = trimNum(strconv.FormatFloat(p.BankrollPercent, 'f', 4, 64)) + "% of balance (min " + base + ")"
	}
	onLoss := "reset"
	switch p.OnLossAction {
	case profile.OnLossMultiply:
		onLoss = "×" + trimNum(fmt.Sprint(p.LossMultiplier))
	case profile.OnLossAdd:
		onLoss = "+" + trimNum(fmt.Sprint(p.LossMultiplier)) + "× base"
	}
	limits := []string{}
	if p.TakeProfit > 0 || p.TakeProfitPct > 0 {
		limits = append(limits, "TP "+fmtLimit(p.TakeProfit, p.TakeProfitPct))
	}
	if p.StopLoss > 0 || p.StopLossPct > 0 {
		limits = append(limits, "SL "+fmtLimit(p.StopLoss, p.StopLossPct))
	}
	if p.MaxBet > 0 {
		limits = append(limits, "max "+fmtAmt(p.MaxBet))
	}
	switch p.OnLossAction {
	case profile.OnLossKeep:
		onLoss = "keep"
	}
	onWin := "reset"
	if p.OnWinAction == profile.OnWinKeep {
		onWin = "keep"
	}
	name := p.Name
	if p.Symbol != "" {
		name += " (" + p.Symbol + ")"
	}
	txt := fmt.Sprintf("%s — base %s, %s%% chance, on win %s, on loss %s", name, base, trimNum(fmt.Sprint(p.Chance)), onWin, onLoss)
	if n := len(p.Conditions); n > 0 {
		limits = append(limits, fmt.Sprintf("%d condition(s)", n))
	}
	if len(limits) > 0 {
		txt += ", " + strings.Join(limits, ", ")
	}
	u.strategyLbl.SetText(txt)
}

// currentBalance is the main balance of the selected currency.
func (u *ui) currentBalance() float64 {
	if u.info == nil || u.symbolSel == nil {
		return 0
	}
	for _, b := range u.info.Balances {
		if b.Currency == u.symbolSel.Selected {
			return parseF(b.Main)
		}
	}
	return 0
}

func (u *ui) buildSessionPane() fyne.CanvasObject {
	// Every tile has a third (USD) line so the four stay the same height.
	tile := func(title string) (fyne.CanvasObject, *canvas.Text, *canvas.Text) {
		v := scaledText("0", color.White, 20)
		v.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
		t := scaledText(strings.ToUpper(title), colMuted, 11)
		t.TextStyle.Bold = true
		usd := scaledText("", colAccent, 12)
		usd.TextStyle.Bold = true
		bg := canvas.NewRectangle(colPanel)
		bg.CornerRadius = 8
		return container.NewStack(bg, container.NewPadded(container.NewVBox(t, v, usd))), v, usd
	}
	t1, b, _ := tile("Bets")
	t2, wn, _ := tile("Wins")
	t3, p, pUSD := tile("Profit")
	t4, wg, wgUSD := tile("Wagered")
	u.sBets, u.sWins, u.sProfit, u.sWager = b, wn, p, wg
	u.sProfitUSD, u.sWagerUSD = pUSD, wgUSD
	reset := widget.NewButtonWithIcon("Reset session", theme.DeleteIcon(), u.resetSession)
	logBtn := widget.NewButtonWithIcon("Bet log", theme.FolderOpenIcon(), func() {
		if u.mobile {
			// Android cannot open a file:// folder; say where the log is.
			dialog.ShowInformation("Bet log", "Every bet is saved as CSV in the app's private storage:\n"+u.log.dir, u.win)
			return
		}
		if err := u.app.OpenURL(&url.URL{Scheme: "file", Path: u.log.dir}); err != nil {
			u.setStatus("Bet log folder: " + u.log.dir)
		}
	})
	logBtn.Importance = widget.LowImportance
	reset.Importance = widget.LowImportance

	u.chart = newProfitChart()

	hdr := historyRow()
	for i, s := range []string{"TIME", "TARGET", "ROLL", "BET", "PAYOUT", "PROFIT"} {
		setCell(hdr, i, s, colMuted)
	}
	u.list = widget.NewList(
		func() int { return len(u.history) },
		func() fyne.CanvasObject { return historyRow() },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			b := u.history[id]
			c := colLoss
			if b.Result {
				c = colWin
			}
			target := b.Choice
			if b.ChoiceOption != "" {
				target += " " + b.ChoiceOption
			}
			setCell(o, 0, time.Unix(b.Created, 0).Format("15:04:05"), colMuted)
			setCell(o, 1, target, color.White)
			setCell(o, 2, fmt.Sprintf("%04d", b.Number), c)
			setCell(o, 3, b.BetAmount, color.White)
			setCell(o, 4, fmt.Sprintf("%.4g×", b.Payout), colMuted)
			setCell(o, 5, signed(b.Profit), c)
		})

	top := container.NewVBox(
		container.NewBorder(nil, nil, nil, container.NewVBox(reset, logBtn), container.NewGridWithColumns(4, t1, t2, t3, t4)),
		spacer(6),
	)
	if u.mobile {
		top = container.NewVBox(container.NewGridWithColumns(2, t1, t2, t3, t4),
			container.NewGridWithColumns(2, reset, logBtn), spacer(6))
	}
	histCard := widget.NewCard("", "", container.NewBorder(hdr, nil, nil, nil, u.list))
	body := container.NewVSplit(u.chart, histCard)
	body.Offset = 0.38
	return container.NewBorder(top, nil, nil, nil, body)
}

func historyRow() *fyne.Container {
	g := container.NewGridWithColumns(6)
	for i := 0; i < 6; i++ {
		t := scaledText("", color.White, 13)
		t.TextStyle.Monospace = true
		g.Add(t)
	}
	return g
}

func setCell(o fyne.CanvasObject, i int, s string, c color.Color) {
	t := o.(*fyne.Container).Objects[i].(*canvas.Text)
	t.Text, t.Color = s, c
	t.Refresh()
}

func fieldLabel(s string) fyne.CanvasObject {
	t := scaledText(s, colMuted, 12)
	return t
}

// ---------- form logic ----------

func (u *ui) isRange() bool { return u.tabs.SelectedIndex() == 1 }

// winChance returns the current win chance in percent.
func (u *ui) winChance() (float64, error) {
	if !u.isRange() {
		v, err := strconv.ParseFloat(strings.TrimSpace(u.chance.Text), 64)
		if err != nil || v < 0.01 || v > 98 {
			return 0, errors.New("chance must be between 0.01 and 98")
		}
		return v, nil
	}
	from, to, err := u.rangeVals()
	if err != nil {
		return 0, err
	}
	c := float64(to-from+1) / 100
	if u.inOut.Selected == "Out" {
		c = 100 - c
	}
	if c < 0.01 || c > 98 {
		return 0, errors.New("range chance must be between 0.01% and 98%")
	}
	return c, nil
}

func (u *ui) rangeVals() (int, int, error) {
	from, e1 := strconv.Atoi(strings.TrimSpace(u.rFrom.Text))
	to, e2 := strconv.Atoi(strings.TrimSpace(u.rTo.Text))
	if e1 != nil || e2 != nil || from < 0 || to > 9999 || from > to {
		return 0, 0, errors.New("range must be 0 ≤ from ≤ to ≤ 9999")
	}
	return from, to, nil
}

func (u *ui) updatePayout() {
	if u.payoutLbl == nil || u.profitLbl == nil {
		return
	}
	c, err := u.winChance()
	if err != nil {
		u.payoutLbl.SetText("—")
		u.profitLbl.SetText("—")
		return
	}
	mult := (100 - houseEdge) / c
	u.payoutLbl.SetText(fmt.Sprintf("%.4f×", mult))
	amt, _ := strconv.ParseFloat(strings.TrimSpace(u.amount.Text), 64)
	txt := fmtAmt(amt * (mult - 1))
	if usd, ok := u.toUSD(amt * (mult - 1)); ok {
		txt += "  ≈ " + usd
	}
	u.profitLbl.SetText(txt)
	u.updateUSD()
}

// refreshMinBet asks the server for the current minimum bet and stores it
// in run. Called from the auto-bet goroutine.
func (u *ui) refreshMinBet(ctx context.Context, sym string, run *profile.Runner) {
	m, accepted, err := u.client.ProbeMinBet(ctx, sym)
	if err != nil {
		fyne.Do(func() {
			u.setStatus("Could not read the minimum bet (" + err.Error() + "); using Base Bet for \"Set to minimum\"")
		})
		return
	}
	run.MinBet = m
	fyne.Do(func() {
		if accepted != nil {
			u.record(accepted, "min-probe")
		}
		u.setStatus(fmt.Sprintf("Minimum %s bet is %s", sym, fmtAmt(m)))
	})
}

// setMinBet fills the amount with the selected currency's current minimum
// bet, asked from the server with a zero-amount bet (no funds at risk).
func (u *ui) setMinBet() {
	sym := u.symbolSel.Selected
	if sym == "" {
		u.setStatus("select a currency first")
		return
	}
	u.minBtn.Disable()
	u.setStatus("Asking DuckDice for the " + sym + " minimum bet…")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		m, accepted, err := u.client.ProbeMinBet(ctx, sym)
		fyne.Do(func() {
			u.minBtn.Enable()
			switch {
			case err != nil:
				u.setStatus("Could not find the minimum bet: " + err.Error())
			case accepted != nil:
				u.record(accepted, "min-probe")
				u.amount.SetText("0")
				u.setStatus("DuckDice accepted a zero bet for " + sym + ", so there is no minimum")
			default:
				u.amount.SetText(fmtAmt(m))
				u.setStatus(fmt.Sprintf("Minimum %s bet is %s", sym, fmtAmt(m)))
			}
		})
	}()
}

func (u *ui) scaleAmount(f float64) {
	v, err := strconv.ParseFloat(strings.TrimSpace(u.amount.Text), 64)
	if err != nil {
		return
	}
	u.amount.SetText(fmtAmt(v * f))
}

func (u *ui) snapshot() (betParams, error) {
	p := betParams{symbol: u.symbolSel.Selected, rangeOn: u.isRange()}
	if p.symbol == "" {
		return p, errors.New("select a currency first")
	}
	amt, err := strconv.ParseFloat(strings.TrimSpace(u.amount.Text), 64)
	if err != nil || amt < 0 {
		return p, errors.New("invalid bet amount")
	}
	p.amount = amt
	if _, err := u.winChance(); err != nil {
		return p, err
	}
	if p.rangeOn {
		p.from, p.to, _ = u.rangeVals()
		p.isIn = u.inOut.Selected == "In"
	} else {
		p.chance = strings.TrimSpace(u.chance.Text)
		p.isHigh = u.hiLo.Selected == "High"
	}
	p.bonus = u.bonusMap[u.bonusSel.Selected]
	p.tle = u.tleMap[u.tleSel.Selected]
	return p, nil
}

// ---------- betting ----------

// place sends one bet. Safe to call from any goroutine.
func (u *ui) place(ctx context.Context, p betParams) (*PlayResponse, error) {
	amt := fmtAmt(p.amount)
	if p.rangeOn {
		return u.client.PlayRange(ctx, RangeRequest{
			Symbol: p.symbol, Range: [2]int{p.from, p.to}, IsIn: p.isIn, Amount: amt,
			UserWageringBonusHash: p.bonus, TLEHash: p.tle,
		})
	}
	return u.client.PlayDice(ctx, DiceRequest{
		Symbol: p.symbol, Chance: p.chance, IsHigh: p.isHigh, Amount: amt,
		UserWageringBonusHash: p.bonus, TLEHash: p.tle,
	})
}

// record applies a bet result to the UI. UI goroutine only.
func (u *ui) record(r *PlayResponse, source string) {
	if err := u.log.Log(source, r, time.Now()); err != nil && !u.logErrShown {
		u.logErrShown = true
		dialog.ShowError(fmt.Errorf("could not write the bet log: %w", err), u.win)
	}
	b := r.Bet
	u.bets++
	if b.Result {
		u.wins++
	}
	u.profit += parseF(b.Profit)
	u.wagered += parseF(b.BetAmount)
	u.sBets.Text = strconv.Itoa(u.bets)
	u.sWins.Text = fmt.Sprintf("%d  (%.1f%%)", u.wins, 100*float64(u.wins)/float64(u.bets))
	u.sProfit.Text = signed(fmtAmt(u.profit))
	u.sProfit.Color = colWin
	if u.profit < 0 {
		u.sProfit.Color = colLoss
	}
	u.sWager.Text = fmtAmt(u.wagered)
	for _, t := range []*canvas.Text{u.sBets, u.sWins, u.sProfit, u.sWager} {
		t.Refresh()
	}
	u.updateUSD()

	u.lastRoll.Text = fmt.Sprintf("%04d", b.Number)
	u.lastRoll.Color = colLoss
	if b.Result {
		u.lastRoll.Color = colWin
	}
	u.lastRoll.Refresh()

	u.history = append([]Bet{b}, u.history...)
	if len(u.history) > maxHistory {
		u.history = u.history[:maxHistory]
	}
	u.list.Refresh()
	u.chart.Add(u.profit)

	if r.User.Balance != "" {
		u.setBalance(b.Symbol, r.User.Balance)
	}
	if r.IsJackpot && r.Jackpot != nil {
		u.setStatus("JACKPOT! " + r.Jackpot.Amount + " " + b.Symbol)
	}
}

func (u *ui) manualBet() {
	p, err := u.snapshot()
	if err != nil {
		u.setStatus(err.Error())
		return
	}
	u.betBtn.Disable()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		r, err := u.place(ctx, p)
		fyne.Do(func() {
			u.betBtn.Enable()
			if err != nil {
				msg := "Bet failed: " + err.Error()
				if m, ok := MinBetFromError(err); ok {
					msg += fmt.Sprintf(" (minimum %s %s — press Min)", fmtAmt(m), p.symbol)
				}
				u.setStatus(msg)
				return
			}
			u.record(r, "manual")
			u.setStatus(resultText(r.Bet))
		})
	}()
}

func (u *ui) toggleAuto() {
	if u.cancel != nil {
		u.stopAuto()
		return
	}
	strat := u.strategy.Clone()
	if err := u.profileMgr.Validate(strat); err != nil {
		u.setStatus("Strategy invalid: " + err.Error())
		return
	}
	if u.symbolSel.Selected == "" {
		u.setStatus("select a currency first")
		return
	}
	if strat.Symbol != "" && strat.Symbol != u.symbolSel.Selected {
		u.setStatus(fmt.Sprintf("Strategy %q is for %s but %s is selected; switch coin or re-apply the profile",
			strat.Name, strat.Symbol, u.symbolSel.Selected))
		return
	}
	if strat.UsesPercentLimits() && u.currentBalance() <= 0 {
		u.setStatus("Stop loss / take profit % needs your balance: press refresh first")
		return
	}
	run := profile.NewRunner(strat, u.currentBalance(), u.hiLo.Selected == "High")
	p := betParams{
		symbol: u.symbolSel.Selected,
		bonus:  u.bonusMap[u.bonusSel.Selected],
		tle:    u.tleMap[u.tleSel.Selected],
	}
	cfg, err := u.autoConfig()
	if err != nil {
		u.setStatus(err.Error())
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.autoBtn.SetText("Stop auto-bet")
	u.autoBtn.SetIcon(theme.MediaStopIcon())
	u.autoBtn.Importance = widget.DangerImportance
	u.autoBtn.Refresh()
	u.betBtn.Disable()
	if sl, tp := strat.Limits(run.StartBalance); sl > 0 || tp > 0 {
		u.setStatus(fmt.Sprintf("Auto-bet started: stop loss %s, take profit %s %s",
			fmtLimitAmt(sl), fmtLimitAmt(tp), p.symbol))
	}
	go u.runAuto(ctx, p, run, cfg)
}

func (u *ui) stopAuto() {
	if u.cancel != nil {
		u.cancel()
	}
}

func (u *ui) autoDone(reason string) {
	u.cancel = nil
	u.autoBtn.SetText("Start auto-bet")
	u.autoBtn.SetIcon(theme.MediaFastForwardIcon())
	u.autoBtn.Importance = widget.SuccessImportance
	u.autoBtn.Refresh()
	u.betBtn.Enable()
	u.setStatus("Auto-bet stopped: " + reason)
}

func (u *ui) autoConfig() (autoCfg, error) {
	var c autoCfg
	var err error
	f := func(e *widget.Entry, name string) float64 {
		v, perr := strconv.ParseFloat(strings.TrimSpace(e.Text), 64)
		if perr != nil || v < 0 {
			err = fmt.Errorf("invalid %s", name)
		}
		return v
	}
	c.count = int(f(u.aCount, "number of bets"))
	c.delay = time.Duration(f(u.aDelay, "delay")) * time.Millisecond
	return c, err
}

func (u *ui) runAuto(ctx context.Context, p betParams, run *profile.Runner, c autoCfg) {
	reason := "cancelled"
	defer func() { fyne.Do(func() { u.autoDone(reason) }) }()

	usesMin := run.P.UsesMinBet()
	if usesMin {
		u.refreshMinBet(ctx, p.symbol, run)
	}
	retries := 0
	for i := 0; c.count == 0 || i < c.count; i++ {
		if ctx.Err() != nil {
			return
		}
		p.amount = run.Amount
		p.chance = strconv.FormatFloat(run.Chance, 'f', -1, 64)
		p.isHigh = run.High
		r, err := u.place(ctx, p)
		if err != nil {
			// "Set to minimum" follows the live minimum: if the server says
			// the minimum went up, adopt it and retry this bet.
			if m, ok := MinBetFromError(err); ok && usesMin && retries < 3 && ctx.Err() == nil {
				retries++
				run.MinBet = m
				run.Amount = max(run.Amount, m)
				fyne.Do(func() { u.setStatus(fmt.Sprintf("Minimum %s bet is now %s; retrying", p.symbol, fmtAmt(m))) })
				i--
				continue
			}
			if ctx.Err() == nil {
				reason = err.Error()
			}
			return
		}
		retries = 0
		stop, why := run.Record(r.Bet.Result, parseF(r.Bet.Profit), parseF(r.User.Balance))
		next, high := run.Amount, run.High
		streak := fmt.Sprintf("W%d", run.WinStreak)
		if run.LoseStreak > 0 {
			streak = fmt.Sprintf("L%d", run.LoseStreak)
		}
		fyne.Do(func() {
			u.record(r, "auto: "+run.P.Name)
			side := "Low"
			if high {
				side = "High"
			}
			u.setStatus(fmt.Sprintf("Auto #%d  %s  streak %s  next %s %s", i+1, resultText(r.Bet), streak, fmtAmt(next), side))
		})

		if stop {
			reason = why + " (" + signed(fmtAmt(run.Profit)) + ")"
			return
		}
		if c.delay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(c.delay):
			}
		}
	}
	reason = fmt.Sprintf("completed %d bets (%s)", c.count, signed(fmtAmt(run.Profit)))
}

func (u *ui) resetSession() {
	u.bets, u.wins, u.profit, u.wagered = 0, 0, 0, 0
	u.history = nil
	u.list.Refresh()
	u.chart.Reset()
	for _, t := range []*canvas.Text{u.sBets, u.sWins, u.sProfit, u.sWager} {
		t.Text, t.Color = "0", color.White
		t.Refresh()
	}
	u.updateUSD()
	u.lastRoll.Text = ""
	u.lastRoll.Refresh()
}

// ---------- account ----------

func (u *ui) refresh() {
	if u.client.Key == "" {
		u.setStatus("Set your API key in settings")
		return
	}
	u.setStatus("Loading account…")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		info, err := u.client.UserInfo(ctx)
		fyne.Do(func() {
			if err != nil {
				u.setStatus("Failed to load account: " + err.Error())
				return
			}
			u.info = info
			u.userLbl.SetText(info.Username)
			var syms []string
			for _, b := range info.Balances {
				syms = append(syms, b.Currency)
			}
			sort.Strings(syms)
			u.symbolSel.Options = syms
			u.profileView.SetCoins(syms)
			select {
			case u.priceKick <- syms:
			default:
			}
			want := u.symbolSel.Selected
			if want == "" {
				want = u.app.Preferences().String(prefSymbol)
			}
			if !contains(syms, want) && len(syms) > 0 {
				want = syms[0]
			}
			u.symbolSel.Refresh()
			if want != "" {
				u.symbolSel.SetSelected(want) // triggers balance/bonus/stats update
			}
			u.updateBalance()
			u.setStatus("Connected as " + info.Username)
		})
	}()
}

func (u *ui) loadStats() {
	sym := u.symbolSel.Selected
	if sym == "" || u.client.Key == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s, err := u.client.Stats(ctx, sym)
		fyne.Do(func() {
			if err != nil || u.symbolSel.Selected != sym {
				return
			}
			u.acctLbl.Text = fmt.Sprintf("all-time %d bets · profit %s · wagered %s",
				s.Bets, signed(s.Profit), s.Volume)
			u.acctLbl.Refresh()
			if s.Balances.Main != "" {
				u.setBalance(sym, s.Balances.Main)
			}
		})
	}()
}

func (u *ui) setBalance(sym, bal string) {
	if u.info == nil {
		return
	}
	for i := range u.info.Balances {
		if u.info.Balances[i].Currency == sym {
			u.info.Balances[i].Main = bal
		}
	}
	u.updateBalance()
}

func (u *ui) updateBalance() {
	sym := u.symbolSel.Selected
	u.balanceTxt.Text = "—"
	if u.info != nil {
		for _, b := range u.info.Balances {
			if b.Currency == sym {
				main := b.Main
				if main == "" {
					main = "0"
				}
				u.balanceTxt.Text = main + " " + sym
			}
		}
	}
	u.balanceTxt.Refresh()
	u.updateUSD()
}

// toUSD converts an amount of the selected coin to dollars, if a price is known.
func (u *ui) toUSD(amount float64) (string, bool) {
	if u.prices == nil || u.symbolSel == nil {
		return "", false
	}
	p, ok := u.prices.USD(u.symbolSel.Selected)
	if !ok {
		return "", false
	}
	return fmtUSD(amount * p), true
}

// updateUSD refreshes every dollar figure for the selected coin's price.
func (u *ui) updateUSD() {
	if u.usdBalTxt == nil || u.usdBetTxt == nil || u.sProfitUSD == nil || u.symbolSel == nil {
		return
	}
	sym := u.symbolSel.Selected
	set := func(t *canvas.Text, s string) {
		if t.Text != s {
			t.Text = s
			t.Refresh()
		}
	}
	price, ok := u.prices.USD(sym)
	switch {
	case sym == "":
		set(u.usdBalTxt, "")
	case !ok:
		set(u.usdBalTxt, "no USD price for "+sym)
	default:
		set(u.usdBalTxt, fmt.Sprintf("≈ %s   ·   1 %s = %s", fmtUSD(u.currentBalance()*price), sym, fmtUSD(price)))
	}
	conv := func(v float64) string {
		if !ok {
			return ""
		}
		return "≈ " + fmtUSD(v*price)
	}
	amt, _ := strconv.ParseFloat(strings.TrimSpace(u.amount.Text), 64)
	if s := conv(amt); s != u.usdBetTxt.Text {
		set(u.usdBetTxt, s)
		u.betHdr.Refresh() // canvas.Text width changed; re-lay out the row
	}
	set(u.sProfitUSD, conv(u.profit))
	set(u.sWagerUSD, conv(u.wagered))
}

// watchPrices refreshes USD prices for the account's coins every minute,
// and right away when kicked (e.g. after the account loads).
func (u *ui) watchPrices(kick <-chan []string) {
	var syms []string
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case s := <-kick:
			syms = s
		case <-tick.C:
		}
		if len(syms) == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := u.prices.Update(ctx, syms)
		cancel()
		fyne.Do(func() {
			if err != nil {
				u.setStatus("USD prices: " + err.Error())
			}
			u.updatePayout() // also refreshes every USD figure
		})
	}
}

func (u *ui) updateBonusOptions() {
	u.bonusMap = map[string]string{}
	u.tleMap = map[string]string{}
	bonus := []string{"None"}
	tle := []string{"None"}
	if u.info != nil {
		for _, b := range u.info.WageringBonuses {
			if b.Status == "active" && b.Symbol == u.symbolSel.Selected {
				k := fmt.Sprintf("%s (%s)", b.Name, b.Hash)
				bonus = append(bonus, k)
				u.bonusMap[k] = b.Hash
			}
		}
		for _, t := range u.info.TLE {
			if t.Status == "active" {
				k := fmt.Sprintf("%s (%s)", t.Name, t.Hash)
				tle = append(tle, k)
				u.tleMap[k] = t.Hash
			}
		}
	}
	u.bonusSel.Options, u.tleSel.Options = bonus, tle
	u.bonusSel.SetSelected("None")
	u.tleSel.SetSelected("None")
}

func (u *ui) showSettings() {
	key := widget.NewPasswordEntry()
	key.SetText(u.client.Key)
	key.SetPlaceHolder("00000000-0000-0000-0000-000000000000")
	link := widget.NewHyperlink("Get your API key on duckdice.io", nil)
	_ = link.SetURLFromString("https://duckdice.io/bot-api")
	where := "Saved in your system keyring (e.g. KWallet)."
	if u.mobile {
		where = "Saved in this app's private storage on the device."
	} else if u.client.Key != "" && !u.keyInKeyring {
		where = "No system keyring available: the key is saved in plain text in the app's settings file."
	}
	whereLbl := widget.NewLabel(where)
	whereLbl.Wrapping = fyne.TextWrapWord
	whereLbl.Importance = widget.LowImportance
	items := []*widget.FormItem{
		widget.NewFormItem("API key", key),
		widget.NewFormItem("", link),
		widget.NewFormItem("", whereLbl),
	}
	d := dialog.NewForm("Settings", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		k := strings.TrimSpace(key.Text)
		u.client.Key = k
		u.keyInKeyring = saveAPIKey(u.app.Preferences(), k, !u.mobile)
		if k != "" && !u.keyInKeyring && !u.mobile {
			dialog.ShowInformation("Key stored in plain text",
				"No system keyring was available, so the API key was saved in the app's settings file.", u.win)
		}
		u.refresh()
	}, u.win)
	d.Resize(dialogSize(u.win, 520, 260))
	d.Show()
}

func (u *ui) setStatus(s string) { u.status.SetText(s) }

// ---------- helpers ----------

func resultText(b Bet) string {
	w := "LOSS"
	if b.Result {
		w = "WIN"
	}
	return fmt.Sprintf("%s  roll %04d  %s %s", w, b.Number, signed(b.Profit), b.Symbol)
}

func parseF(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func fmtAmt(v float64) string { return trimNum(strconv.FormatFloat(v, 'f', 8, 64)) }

func trimNum(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

func signed(s string) string {
	if s != "" && s[0] != '-' && s[0] != '+' && parseF(s) != 0 {
		return "+" + s
	}
	return s
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func fmtLimitAmt(v float64) string {
	if v == 0 {
		return "off"
	}
	return fmtAmt(v)
}
