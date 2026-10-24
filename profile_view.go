package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"dbot/profile"
)

// ProfileView provides an editable, saveable GUI for betting strategy profiles.
type ProfileView struct {
	pm      *profile.ProfileManager
	win     fyne.Window
	onApply func(*profile.BettingProfile)

	// Balance returns the current balance of the selected coin (0 if
	// unknown). It seeds the strategy test's starting balance.
	Balance func() float64

	currentProfile *profile.BettingProfile
	cleanSnapshot  *profile.BettingProfile
	conds          []profile.Condition // working copy edited by condRows
	condBox        *fyne.Container
	condHeader     *fyne.Container
	top, bottom    *fyne.Container
	isSwitching    bool

	// UI widgets
	profileSelect *widget.Select
	labelOf       map[string]string // profile ID → dropdown label
	idOf          map[string]string // dropdown label → profile ID
	coinSelect    *widget.Select
	coins         []string
	form          *widget.Form
	nameEntry     *widget.Entry
	baseBetEntry  *widget.Entry
	chanceEntry   *widget.Entry
	lossMulEntry  *widget.Entry
	stopLossEntry *widget.Entry
	takeProfEntry *widget.Entry
	maxBetEntry   *widget.Entry
	bankrollEntry *widget.Entry
	onLossSelect  *widget.Select
	onWinSelect   *widget.Select

	saveBtn   *widget.Button
	saveAsBtn *widget.Button
	deleteBtn *widget.Button
	applyBtn  *widget.Button
	statusLbl *widget.Label

	root fyne.CanvasObject
}

// NewProfileView instantiates a new ProfileView.
func NewProfileView(pm *profile.ProfileManager, win fyne.Window, onApply func(*profile.BettingProfile)) *ProfileView {
	pv := &ProfileView{
		pm:      pm,
		win:     win,
		onApply: onApply,
	}
	pv.buildUI()
	pv.refreshProfilesList("")
	return pv
}

func (pv *ProfileView) buildUI() {
	// Top profile dropdown
	pv.profileSelect = widget.NewSelect(nil, func(label string) {
		pv.onProfileSelected(pv.idOf[label])
	})
	pv.profileSelect.PlaceHolder = "Select Strategy Profile"

	// Form inputs with numeric validators
	pv.nameEntry = widget.NewEntry()
	pv.nameEntry.Validator = func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("name cannot be empty")
		}
		return nil
	}

	pv.baseBetEntry = widget.NewEntry()
	pv.baseBetEntry.Validator = func(s string) error {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || v <= 0 {
			return errors.New("must be a number > 0")
		}
		return nil
	}

	pv.chanceEntry = widget.NewEntry()
	pv.chanceEntry.Validator = func(s string) error {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || v < 0.01 || v > 98.0 {
			return errors.New("chance must be between 0.01% and 98%")
		}
		return nil
	}

	pv.lossMulEntry = widget.NewEntry()
	pv.lossMulEntry.Validator = func(s string) error {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || v < 0 {
			return errors.New("loss multiplier cannot be negative")
		}
		return nil
	}

	pv.stopLossEntry = widget.NewEntry()
	pv.stopLossEntry.Validator = func(s string) error {
		_, pct, err := parseLimit(s)
		if err == nil && pct > 100 {
			err = errors.New("stop loss % cannot exceed 100")
		}
		return err
	}

	pv.takeProfEntry = widget.NewEntry()
	pv.takeProfEntry.Validator = func(s string) error {
		_, _, err := parseLimit(s)
		return err
	}

	pv.maxBetEntry = widget.NewEntry()
	pv.maxBetEntry.Validator = func(s string) error {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || v < 0 {
			return errors.New("max bet cannot be negative")
		}
		return nil
	}

	pv.bankrollEntry = widget.NewEntry()
	pv.bankrollEntry.Validator = func(s string) error {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || v < 0 || v > 100 {
			return errors.New("must be between 0 and 100")
		}
		return nil
	}

	pv.onLossSelect = widget.NewSelect([]string{
		string(profile.OnLossReset),
		string(profile.OnLossMultiply),
		string(profile.OnLossAdd),
		string(profile.OnLossKeep),
	}, func(s string) {
		// The multiplier only matters for Multiply and Add
		if s == string(profile.OnLossReset) || s == string(profile.OnLossKeep) {
			pv.lossMulEntry.Disable()
		} else {
			pv.lossMulEntry.Enable()
		}
	})
	pv.onLossSelect.SetSelected(string(profile.OnLossMultiply))

	pv.onWinSelect = widget.NewSelect([]string{string(profile.OnWinReset), string(profile.OnWinKeep)}, nil)
	pv.onWinSelect.SetSelected(string(profile.OnWinReset))

	// Two-column widget.Form
	pv.coinSelect = widget.NewSelect([]string{anyCoin}, nil)
	pv.coinSelect.SetSelected(anyCoin)

	pair := func(a, b fyne.CanvasObject) fyne.CanvasObject { return container.NewGridWithColumns(2, a, b) }
	pv.maxBetEntry.SetPlaceHolder("0 = off")
	for _, e := range []*widget.Entry{pv.stopLossEntry, pv.takeProfEntry} {
		e.SetPlaceHolder("0 = off; 250 or 10%")
	}
	pv.bankrollEntry.SetPlaceHolder("0 = fixed base bet")
	pv.form = widget.NewForm(
		widget.NewFormItem("Name · Coin", pair(pv.nameEntry, pv.coinSelect)),
		widget.NewFormItem("Base Bet · Chance %", pair(pv.baseBetEntry, pv.chanceEntry)),
		widget.NewFormItem("On Win · On Loss", pair(pv.onWinSelect, pv.onLossSelect)),
		widget.NewFormItem("Loss Mult. · Max Bet", pair(pv.lossMulEntry, pv.maxBetEntry)),
		widget.NewFormItem("Stop Loss · Take Profit", pair(pv.stopLossEntry, pv.takeProfEntry)),
		widget.NewFormItem("Bankroll % (0 = fixed)", pv.bankrollEntry),
	)

	// Action buttons
	pv.saveBtn = widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), pv.saveCurrent)
	pv.saveBtn.Importance = widget.HighImportance

	pv.saveAsBtn = widget.NewButtonWithIcon("Save As New", theme.ContentAddIcon(), pv.saveAsNew)

	pv.deleteBtn = widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), pv.deleteCurrent)
	pv.deleteBtn.Importance = widget.DangerImportance

	pv.applyBtn = widget.NewButtonWithIcon("Apply to Active Bot", theme.MediaPlayIcon(), pv.applyCurrent)
	pv.applyBtn.Importance = widget.SuccessImportance

	pv.statusLbl = widget.NewLabel("Ready")
	pv.statusLbl.Importance = widget.LowImportance

	btnRow := container.NewGridWithColumns(3, pv.saveBtn, pv.saveAsBtn, pv.deleteBtn)

	// Streak / condition rules fill whatever height is left in the column.
	pv.condBox = container.NewVBox()
	addCond := widget.NewButtonWithIcon("Add condition", theme.ContentAddIcon(), func() {
		pv.conds = append(pv.conds, profile.Condition{
			Trigger: profile.StreakAbove, Count: 2, Outcome: profile.Lose, Action: profile.ResetAmount,
		})
		pv.rebuildConditions()
	})
	addCond.Importance = widget.LowImportance
	pv.condHeader = container.NewBorder(nil, nil, fieldLabel("Conditions (streaks), applied in order"), addCond)
	conds := container.NewBorder(pv.condHeader, nil, nil, nil, container.NewVScroll(pv.condBox))

	top := container.NewVBox(fieldLabel("Strategy Profile"), pv.profileSelect, spacer(4), pv.form, spacer(4))
	testBtn := widget.NewButtonWithIcon("Test strategy", theme.SearchIcon(), pv.showTest)
	bottom := container.NewVBox(spacer(6), btnRow,
		container.NewGridWithColumns(2, pv.applyBtn, testBtn), pv.statusLbl)
	pv.top, pv.bottom = top, bottom
	pv.root = widget.NewCard("Strategy", "", container.NewBorder(top, bottom, nil, nil, conds))
}

// MobileCanvasObject lays the same widgets out as one scrolling page for
// phones, where the column is too short for an inner scroll area. Use it
// instead of CanvasObject, not alongside it.
func (pv *ProfileView) MobileCanvasObject() fyne.CanvasObject {
	pv.form.Orientation = widget.Vertical // labels above fields: fits a phone's width
	return container.NewVScroll(container.NewPadded(
		container.NewVBox(pv.top, pv.condHeader, pv.condBox, pv.bottom)))
}

// CanvasObject returns the renderable CanvasObject for embedding in the UI.
func (pv *ProfileView) CanvasObject() fyne.CanvasObject {
	return pv.root
}

// isDirty checks if the user has made unsaved modifications to the active profile.
func (pv *ProfileView) isDirty() bool {
	if pv.currentProfile == nil {
		return false
	}

	p, err := pv.readForm()
	if err != nil {
		return true // If form has changed into something currently invalid, it's dirty
	}

	return pv.cleanSnapshot == nil || !p.Equal(pv.cleanSnapshot)
}

// onProfileSelected handles dropdown changes and warns on unsaved edits.
func (pv *ProfileView) onProfileSelected(selectedID string) {
	if pv.isSwitching || strings.TrimSpace(selectedID) == "" {
		return
	}

	// If the user picked the already active profile, do nothing
	if pv.currentProfile != nil && pv.currentProfile.ID == selectedID {
		return
	}

	if pv.isDirty() {
		// Warn user about unsaved changes before switching
		d := dialog.NewConfirm("Unsaved Changes",
			fmt.Sprintf("You have unsaved changes in profile %q. Discard them?", pv.currentProfile.Name),
			func(discard bool) {
				if discard {
					pv.loadProfile(selectedID)
				} else {
					// Revert dropdown selection safely without re-triggering callback
					pv.isSwitching = true
					if pv.currentProfile != nil {
						pv.profileSelect.SetSelected(pv.labelOf[pv.currentProfile.ID])
					}
					pv.isSwitching = false
				}
			}, pv.win)
		d.Show()
		return
	}

	pv.loadProfile(selectedID)
}

// loadProfile loads a profile from disk and populates all form fields.
func (pv *ProfileView) loadProfile(id string) {
	p, err := pv.pm.LoadProfile(id)
	if err != nil {
		pv.setStatus(fmt.Sprintf("Error loading profile: %v", err))
		return
	}

	pv.currentProfile = p
	pv.cleanSnapshot = p.Clone()

	pv.isSwitching = true
	pv.profileSelect.SetSelected(pv.labelOf[p.ID])
	pv.isSwitching = false

	// Populate form fields
	pv.nameEntry.SetText(p.Name)
	pv.baseBetEntry.SetText(fmtAmt(p.BaseBet))
	pv.chanceEntry.SetText(trimNum(strconv.FormatFloat(p.Chance, 'f', 2, 64)))
	pv.lossMulEntry.SetText(trimNum(strconv.FormatFloat(p.LossMultiplier, 'f', 4, 64)))
	pv.stopLossEntry.SetText(fmtLimit(p.StopLoss, p.StopLossPct))
	pv.takeProfEntry.SetText(fmtLimit(p.TakeProfit, p.TakeProfitPct))
	pv.maxBetEntry.SetText(fmtAmt(p.MaxBet))
	pv.bankrollEntry.SetText(trimNum(strconv.FormatFloat(p.BankrollPercent, 'f', 4, 64)))

	pv.onLossSelect.SetSelected(string(p.OnLossAction))
	if p.OnWinAction == "" {
		pv.onWinSelect.SetSelected(string(profile.OnWinReset))
	} else {
		pv.onWinSelect.SetSelected(string(p.OnWinAction))
	}
	pv.setCoin(p.Symbol)
	pv.conds = slices.Clone(p.Conditions)
	pv.rebuildConditions()
	if p.OnLossAction == profile.OnLossReset || p.OnLossAction == profile.OnLossKeep {
		pv.lossMulEntry.Disable()
	} else {
		pv.lossMulEntry.Enable()
	}

	// Disable delete for default profile
	if p.ID == profile.DefaultProfileID || strings.EqualFold(p.Name, profile.DefaultProfileName) {
		pv.deleteBtn.Disable()
	} else {
		pv.deleteBtn.Enable()
	}

	pv.setStatus(fmt.Sprintf("Loaded profile: %s", p.Name))
}

// readForm parses current widget entries into a BettingProfile.
func (pv *ProfileView) readForm() (*profile.BettingProfile, error) {
	name := strings.TrimSpace(pv.nameEntry.Text)
	if name == "" {
		return nil, errors.New("profile name cannot be empty")
	}

	baseBet, err := strconv.ParseFloat(strings.TrimSpace(pv.baseBetEntry.Text), 64)
	if err != nil || baseBet <= 0 {
		return nil, errors.New("base bet must be a number > 0")
	}

	chance, err := strconv.ParseFloat(strings.TrimSpace(pv.chanceEntry.Text), 64)
	if err != nil || chance < 0.01 || chance > 98.0 {
		return nil, errors.New("chance must be between 0.01% and 98%")
	}

	lossMul, err := strconv.ParseFloat(strings.TrimSpace(pv.lossMulEntry.Text), 64)
	if err != nil || lossMul < 0 {
		return nil, errors.New("loss multiplier cannot be negative")
	}

	stopLoss, stopLossPct, err := parseLimit(pv.stopLossEntry.Text)
	if err != nil {
		return nil, fmt.Errorf("stop loss: %w", err)
	}
	if stopLossPct > 100 {
		return nil, errors.New("stop loss % cannot exceed 100")
	}

	takeProfit, takeProfitPct, err := parseLimit(pv.takeProfEntry.Text)
	if err != nil {
		return nil, fmt.Errorf("take profit: %w", err)
	}

	maxBet, err := strconv.ParseFloat(strings.TrimSpace(pv.maxBetEntry.Text), 64)
	if err != nil || maxBet < 0 {
		return nil, errors.New("max bet cannot be negative")
	}

	bankroll, err := strconv.ParseFloat(strings.TrimSpace(pv.bankrollEntry.Text), 64)
	if err != nil || bankroll < 0 || bankroll > 100 {
		return nil, errors.New("bankroll % must be between 0 and 100")
	}

	for i, c := range pv.conds {
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("condition %d: %w", i+1, err)
		}
	}
	onWin := profile.OnWinAction(pv.onWinSelect.Selected)
	if onWin == profile.OnWinReset {
		onWin = "" // the default; keeps older profile files unchanged
	}

	id := ""
	if pv.currentProfile != nil {
		id = pv.currentProfile.ID
	}

	return &profile.BettingProfile{
		ID:              id,
		Name:            name,
		BaseBet:         baseBet,
		Chance:          chance,
		OnLossAction:    profile.OnLossAction(pv.onLossSelect.Selected),
		LossMultiplier:  lossMul,
		StopLoss:        stopLoss,
		TakeProfit:      takeProfit,
		StopLossPct:     stopLossPct,
		TakeProfitPct:   takeProfitPct,
		MaxBet:          maxBet,
		BankrollPercent: bankroll,
		OnWinAction:     onWin,
		Symbol:          pv.coin(),
		Conditions:      slices.Clone(pv.conds),
	}, nil
}

// saveCurrent validates inputs, persists changes to disk, and updates active state.
func (pv *ProfileView) saveCurrent() {
	p, err := pv.readForm()
	if err != nil {
		dialog.ShowError(err, pv.win)
		return
	}

	if err := pv.pm.Validate(p); err != nil {
		dialog.ShowError(err, pv.win)
		return
	}

	if err := pv.pm.SaveProfile(p); err != nil {
		dialog.ShowError(fmt.Errorf("failed to save profile: %w", err), pv.win)
		return
	}

	pv.currentProfile = p
	pv.cleanSnapshot = p.Clone()
	pv.refreshProfilesList(p.ID)
	pv.setStatus(fmt.Sprintf("Saved profile %q successfully", p.Name))
}

// saveAsNew prompts for a new profile name and creates a new BettingProfile.
func (pv *ProfileView) saveAsNew() {
	p, err := pv.readForm()
	if err != nil {
		dialog.ShowError(err, pv.win)
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetText(p.Name + " copy")
	nameEntry.Validator = func(s string) error {
		if profile.Slugify(s) == "" {
			return errors.New("enter a name with letters or digits")
		}
		return nil
	}
	d := dialog.NewForm("Save As New Profile", "Create", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Name", nameEntry)}, func(ok bool) {
			if !ok {
				return
			}
			newProfile := p.Clone()
			newProfile.Name = strings.TrimSpace(nameEntry.Text)
			newProfile.ID = pv.uniqueID(newProfile.Name)

			if err := pv.pm.SaveProfile(newProfile); err != nil {
				dialog.ShowError(fmt.Errorf("failed to save new profile: %w", err), pv.win)
				return
			}

			pv.refreshProfilesList(newProfile.ID)
			pv.setStatus(fmt.Sprintf("Created new profile %q", newProfile.Name))
		}, pv.win)
	d.Resize(fyne.NewSize(380, 160))
	d.Show()
}

// deleteCurrent removes the active non-default profile after user confirmation.
func (pv *ProfileView) deleteCurrent() {
	if pv.currentProfile == nil {
		return
	}

	if pv.currentProfile.ID == profile.DefaultProfileID || strings.EqualFold(pv.currentProfile.Name, profile.DefaultProfileName) {
		dialog.ShowInformation("Protected Profile", "The default profile cannot be deleted.", pv.win)
		return
	}

	d := dialog.NewConfirm("Delete Profile",
		fmt.Sprintf("Are you sure you want to permanently delete profile %q?", pv.currentProfile.Name),
		func(ok bool) {
			if !ok {
				return
			}
			if err := pv.pm.DeleteProfile(pv.currentProfile.ID); err != nil {
				dialog.ShowError(err, pv.win)
				return
			}
			pv.refreshProfilesList(profile.DefaultProfileID)
			pv.setStatus("Profile deleted")
		}, pv.win)
	d.Show()
}

// applyCurrent invokes the onApply callback to set active bot settings.
func (pv *ProfileView) applyCurrent() {
	p, err := pv.readForm()
	if err != nil {
		dialog.ShowError(err, pv.win)
		return
	}
	if err := pv.pm.Validate(p); err != nil {
		dialog.ShowError(err, pv.win)
		return
	}

	if pv.onApply != nil {
		pv.onApply(p)
		pv.setStatus(fmt.Sprintf("Applied profile %q to bot controls", p.Name))
	}
}

func (pv *ProfileView) refreshProfilesList(selectID string) {
	list, err := pv.pm.ListProfiles()
	if err != nil {
		pv.setStatus(fmt.Sprintf("Error listing profiles: %v", err))
		return
	}

	// Show names (and coin) rather than file IDs; keep labels unique.
	pv.labelOf, pv.idOf = map[string]string{}, map[string]string{}
	labels := make([]string, 0, len(list))
	for _, id := range list {
		label := id
		if p, err := pv.pm.LoadProfile(id); err == nil {
			label = p.Name
			if p.Symbol != "" {
				label += " · " + p.Symbol
			}
		}
		if _, dup := pv.idOf[label]; dup {
			label += " (" + id + ")"
		}
		pv.labelOf[id], pv.idOf[label] = label, id
		labels = append(labels, label)
	}

	pv.isSwitching = true
	pv.profileSelect.Options = labels
	pv.profileSelect.Refresh()
	pv.isSwitching = false

	target := selectID
	if target == "" {
		if pv.currentProfile != nil && contains(list, pv.currentProfile.ID) {
			target = pv.currentProfile.ID
		} else if len(list) > 0 {
			target = list[0]
		}
	}

	if target != "" {
		pv.loadProfile(target)
	}
}

// uniqueID derives an ID from name that no saved profile uses yet, so
// "Save As New" never overwrites an existing file.
func (pv *ProfileView) uniqueID(name string) string {
	ids, _ := pv.pm.ListProfiles()
	base := profile.Slugify(name)
	id := base
	for i := 2; contains(ids, id); i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return id
}

// Current returns a copy of the last loaded or saved profile.
func (pv *ProfileView) Current() *profile.BettingProfile {
	if pv.currentProfile == nil {
		return nil
	}
	return pv.currentProfile.Clone()
}

func (pv *ProfileView) setStatus(s string) {
	pv.statusLbl.SetText(s)
}

// rebuildConditions redraws the condition rows from pv.conds.
func (pv *ProfileView) rebuildConditions() {
	pv.condBox.RemoveAll()
	if len(pv.conds) == 0 {
		pv.condBox.Add(fieldLabel("None: only the on win / on loss actions apply."))
	}
	for i := range pv.conds {
		pv.condBox.Add(pv.conditionRow(i))
	}
	pv.condBox.Refresh()
}

// conditionRow is an editor for pv.conds[i], laid out like DuckDice's
// "ON <trigger> <n> <outcome> DO <action> <value>".
func (pv *ProfileView) conditionRow(i int) fyne.CanvasObject {
	c := &pv.conds[i]

	trigger := widget.NewSelect(nil, func(s string) { c.Trigger = profile.Trigger(s) })
	for _, t := range profile.Triggers {
		trigger.Options = append(trigger.Options, string(t))
	}
	trigger.SetSelected(string(c.Trigger))

	count := widget.NewEntry()
	count.SetText(strconv.Itoa(c.Count))
	count.OnChanged = func(s string) { c.Count, _ = strconv.Atoi(strings.TrimSpace(s)) }

	outcome := widget.NewSelect(nil, func(s string) { c.Outcome = profile.Outcome(s) })
	for _, o := range profile.Outcomes {
		outcome.Options = append(outcome.Options, string(o))
	}
	outcome.SetSelected(string(c.Outcome))

	value := widget.NewEntry()
	value.SetText(trimNum(strconv.FormatFloat(c.Value, 'f', 8, 64)))
	value.OnChanged = func(s string) { c.Value, _ = strconv.ParseFloat(strings.TrimSpace(s), 64) }

	action := widget.NewSelect(nil, func(s string) {
		c.Action = profile.Action(s)
		if c.Action.NeedsValue() {
			value.Enable()
		} else {
			value.Disable()
		}
	})
	for _, a := range profile.Actions {
		action.Options = append(action.Options, string(a))
	}
	action.SetSelected(string(c.Action))

	del := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		pv.conds = slices.Delete(pv.conds, i, i+1)
		pv.rebuildConditions()
	})
	del.Importance = widget.LowImportance

	label := func(s string) fyne.CanvasObject { return container.NewCenter(fieldLabel(s)) }
	on := container.NewBorder(nil, nil, label(fmt.Sprintf("%d ON", i+1)), nil,
		container.NewGridWithColumns(3, trigger, count, outcome))
	do := container.NewBorder(nil, nil, label("   DO"), del,
		container.NewGridWithColumns(2, action, value))
	bg := canvas.NewRectangle(colInput)
	bg.CornerRadius = 6
	return container.NewStack(bg, container.NewPadded(container.NewVBox(on, do)))
}

// parseLimit reads a stop-loss / take-profit field: a coin amount ("250")
// or a percentage of the balance at run start ("10%"). Empty means off.
func parseLimit(s string) (amount, pct float64, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, nil
	}
	isPct := strings.HasSuffix(s, "%")
	v, perr := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, "%")), 64)
	if perr != nil || v < 0 {
		return 0, 0, errors.New("enter an amount like 250 or a percentage like 10%")
	}
	if isPct {
		return 0, v, nil
	}
	return v, 0, nil
}

// fmtLimit is the inverse of parseLimit.
func fmtLimit(amount, pct float64) string {
	if pct > 0 {
		return trimNum(strconv.FormatFloat(pct, 'f', 4, 64)) + "%"
	}
	return fmtAmt(amount)
}

// showTest runs the profile as currently shown in the form (saved or not)
// through simulated sessions and shows what it tends to win or lose.
func (pv *ProfileView) showTest() {
	p, err := pv.readForm()
	if err == nil {
		err = pv.pm.Validate(p)
	}
	if err != nil {
		dialog.ShowError(err, pv.win)
		return
	}

	start := 1000.0
	if pv.Balance != nil && pv.Balance() > 0 {
		start = pv.Balance()
	}
	balEntry, sessEntry, betsEntry := widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	balEntry.SetText(fmtAmt(start))
	sessEntry.SetText("2000")
	betsEntry.SetText("500")
	inputs := widget.NewForm(
		widget.NewFormItem("Starting balance", balEntry),
		widget.NewFormItem("Sessions", sessEntry),
		widget.NewFormItem("Max bets per session", betsEntry),
	)

	names := []string{
		"Average result", "Median result", "10% / 90% of sessions", "Worst / best session",
		"Sessions ending up", "Ended by take profit", "Ended by stop loss", "Ended by a Stop condition",
		"Lost everything", "Bets per session", "Total bet per session", "Average deepest dip",
	}
	vals := make([]*widget.Label, len(names))
	results := widget.NewForm()
	for i, n := range names {
		vals[i] = widget.NewLabel("—")
		vals[i].TextStyle.Monospace = true
		results.Append(n, vals[i])
	}

	pctf := func(v float64) string { return fmt.Sprintf("%+.2f%%", v) }
	share := func(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	var run *widget.Button
	run = widget.NewButtonWithIcon("Run test", theme.MediaPlayIcon(), func() {
		bal, e1 := strconv.ParseFloat(strings.TrimSpace(balEntry.Text), 64)
		n, e2 := strconv.Atoi(strings.TrimSpace(sessEntry.Text))
		m, e3 := strconv.Atoi(strings.TrimSpace(betsEntry.Text))
		if e1 != nil || e2 != nil || e3 != nil || bal <= 0 || n < 1 || m < 1 || n > 100000 || m > 100000 {
			status.SetText("Enter a balance > 0 and between 1 and 100000 sessions and bets.")
			return
		}
		run.Disable()
		status.SetText("Running…")
		go func() {
			r := profile.Simulate(p, profile.SimConfig{StartBalance: bal, Sessions: n, MaxBets: m, Seed: 1})
			fyne.Do(func() {
				run.Enable()
				out := []string{
					pctf(r.MeanPct) + "  (" + fmtAmt(bal*r.MeanPct/100) + ")",
					pctf(r.MedianPct),
					pctf(r.P10Pct) + " / " + pctf(r.P90Pct),
					pctf(r.WorstPct) + " / " + pctf(r.BestPct),
					share(r.ProfitableShare),
					share(r.TakeProfitShare),
					share(r.StopLossShare),
					share(r.ConditionShare),
					share(r.BustShare),
					fmt.Sprintf("%.0f", r.AvgBets),
					fmt.Sprintf("%.2f×", r.AvgWageredX),
					fmt.Sprintf("−%.2f%%", r.MaxDrawdownPct),
				}
				for i, v := range out {
					vals[i].SetText(v)
				}
				status.SetText(fmt.Sprintf("Tested %q over %d sessions.", p.Name, n))
			})
		}()
	})
	run.Importance = widget.HighImportance

	note := widget.NewLabel("Results are % of the starting balance. Median: half of sessions did better. " +
		"10%/90%: one session in ten ended below/above. Total bet is × the starting balance; on average " +
		"about 1% of it is lost. Simulated rolls with DuckDice's payout (99 ÷ chance); minimum-bet limits " +
		"and network errors are not modelled, and the same inputs always give the same numbers.")
	note.Wrapping = fyne.TextWrapWord
	note.Importance = widget.LowImportance
	content := container.NewBorder(
		container.NewVBox(inputs, run, status),
		note, nil, nil,
		container.NewVScroll(results),
	)
	d := dialog.NewCustom("Test strategy: "+p.Name, "Close", content, pv.win)
	d.Resize(dialogSize(pv.win, 640, 930))
	d.Show()
}

const anyCoin = "Any coin"

// SetCoins sets the coins offered in the Coin field (the account's balances).
func (pv *ProfileView) SetCoins(coins []string) {
	pv.coins = slices.Clone(coins)
	cur := pv.coin()
	pv.setCoin(cur)
}

// setCoin selects sym ("" = any) in the Coin field, adding it to the options
// if the account has no balance in it.
func (pv *ProfileView) setCoin(sym string) {
	opts := append([]string{anyCoin}, pv.coins...)
	if sym != "" && !contains(opts, sym) {
		opts = append(opts, sym)
	}
	pv.coinSelect.Options = opts
	if sym == "" {
		sym = anyCoin
	}
	pv.coinSelect.SetSelected(sym)
}

func (pv *ProfileView) coin() string {
	if s := pv.coinSelect.Selected; s != anyCoin {
		return s
	}
	return ""
}
