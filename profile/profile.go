// Package profile defines betting strategy profiles and stores them as JSON
// files on disk. It has no UI dependencies.
package profile

import (
	"math"
	"reflect"
	"slices"
)

// OnLossAction says how the next bet changes after a loss.
type OnLossAction string

const (
	OnLossReset    OnLossAction = "Reset"    // back to the base bet
	OnLossMultiply OnLossAction = "Multiply" // current bet × LossMultiplier
	OnLossAdd      OnLossAction = "Add"      // current bet + base bet × LossMultiplier
	OnLossKeep     OnLossAction = "Keep"     // same bet again
)

// OnWinAction says how the next bet changes after a win.
type OnWinAction string

const (
	OnWinReset OnWinAction = "Reset" // back to the base bet (also the zero value)
	OnWinKeep  OnWinAction = "Keep"  // same bet again
)

const (
	DefaultProfileID   = "default-martingale"
	DefaultProfileName = "Default Martingale"
)

// BettingProfile is a saved auto-bet strategy. Zero StopLoss, TakeProfit and
// MaxBet mean "off".
//
// When BankrollPercent > 0 the base bet is recomputed from the current
// balance every time the bet resets, so it grows and shrinks with the
// bankroll; BaseBet is then the minimum bet.
type BettingProfile struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Symbol          string       `json:"symbol,omitempty"` // coin the amounts are for; "" = any
	BaseBet         float64      `json:"baseBet"`
	Chance          float64      `json:"chance"`
	OnLossAction    OnLossAction `json:"onLossAction"`
	LossMultiplier  float64      `json:"lossMultiplier"`
	StopLoss        float64      `json:"stopLoss"`
	TakeProfit      float64      `json:"takeProfit"`
	StopLossPct     float64      `json:"stopLossPct,omitempty"`   // % of the balance at run start; 0 = off
	TakeProfitPct   float64      `json:"takeProfitPct,omitempty"` // % of the balance at run start; 0 = off
	MaxBet          float64      `json:"maxBet"`
	BankrollPercent float64      `json:"bankrollPercent,omitempty"`
	OnWinAction     OnWinAction  `json:"onWinAction,omitempty"`
	Conditions      []Condition  `json:"conditions,omitempty"`
}

// NewDefaultProfile returns the built-in fallback profile.
func NewDefaultProfile() *BettingProfile {
	return &BettingProfile{
		ID:             DefaultProfileID,
		Name:           DefaultProfileName,
		BaseBet:        0.00000001,
		Chance:         49.5,
		OnLossAction:   OnLossMultiply,
		LossMultiplier: 2,
	}
}

// Clone returns a deep copy of p.
func (p *BettingProfile) Clone() *BettingProfile {
	c := *p
	c.Conditions = slices.Clone(p.Conditions)
	return &c
}

// Equal reports whether p and q hold the same settings.
func (p *BettingProfile) Equal(q *BettingProfile) bool {
	a, b := *p, *q
	a.Conditions, b.Conditions = nil, nil
	return reflect.DeepEqual(a, b) && slices.Equal(p.Conditions, q.Conditions)
}

// Base is the bet to reset to for the given balance.
func (p *BettingProfile) Base(balance float64) float64 {
	b := p.BaseBet
	if p.BankrollPercent > 0 {
		b = math.Max(b, balance*p.BankrollPercent/100)
	}
	return p.capped(b)
}

// Next is the bet to place after a bet of current that won or lost, given
// the balance after that bet.
func (p *BettingProfile) Next(current float64, won bool, balance float64) float64 {
	if won {
		if p.OnWinAction == OnWinKeep {
			return p.capped(current)
		}
		return p.Base(balance)
	}
	switch p.OnLossAction {
	case OnLossKeep:
		return p.capped(current)
	case OnLossMultiply:
		return p.capped(current * p.LossMultiplier)
	case OnLossAdd:
		return p.capped(current + p.Base(balance)*p.LossMultiplier)
	}
	return p.Base(balance)
}

func (p *BettingProfile) capped(b float64) float64 {
	if p.MaxBet > 0 && b > p.MaxBet {
		return p.MaxBet
	}
	return b
}

// ShouldStop reports whether a run with the given profit should stop, and
// why, using only the fixed-amount limits.
func (p *BettingProfile) ShouldStop(profit float64) (bool, string) {
	return p.ShouldStopAt(profit, 0)
}

// Limits returns the stop-loss and take-profit amounts (0 = off) for a run
// that started with startBalance. When both a fixed amount and a percentage
// are set, the tighter one wins. Percentages are ignored if startBalance is 0.
func (p *BettingProfile) Limits(startBalance float64) (stopLoss, takeProfit float64) {
	tighter := func(fixed, pct float64) float64 {
		v := fixed
		if pct > 0 && startBalance > 0 {
			if byPct := startBalance * pct / 100; v == 0 || byPct < v {
				v = byPct
			}
		}
		return v
	}
	return tighter(p.StopLoss, p.StopLossPct), tighter(p.TakeProfit, p.TakeProfitPct)
}

// ShouldStopAt reports whether a run that started with startBalance and has
// made profit so far should stop, and why.
func (p *BettingProfile) ShouldStopAt(profit, startBalance float64) (bool, string) {
	sl, tp := p.Limits(startBalance)
	if tp > 0 && profit >= tp {
		return true, "take profit reached"
	}
	if sl > 0 && -profit >= sl {
		return true, "stop loss reached"
	}
	return false, ""
}

// UsesPercentLimits reports whether the profile needs the starting balance.
func (p *BettingProfile) UsesPercentLimits() bool {
	return p.StopLossPct > 0 || p.TakeProfitPct > 0
}
