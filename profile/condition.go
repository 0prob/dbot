package profile

import (
	"errors"
	"fmt"
)

// Trigger says when a Condition fires, counted over the current run.
type Trigger string

const (
	EveryN       Trigger = "Every"           // every Nth win/loss/bet
	EveryStreak  Trigger = "Every streak of" // streak length is a multiple of N
	StreakAbove  Trigger = "Streak above"    // streak length > N (fires on every such bet)
	FirstStreakN Trigger = "First streak of" // streak length == N
)

// Outcome is what a Condition counts.
type Outcome string

const (
	Win  Outcome = "Win"
	Lose Outcome = "Lose"
	Any  Outcome = "Bet" // any bet; only valid with EveryN
)

// Action is what a Condition does when it fires.
type Action string

const (
	IncreasePct Action = "Increase by %"
	DecreasePct Action = "Decrease by %"
	SetAmount   Action = "Set amount"
	ResetAmount Action = "Reset amount"
	SetChance   Action = "Set chance"
	SwitchSide  Action = "Switch side"
	Stop        Action = "Stop"
	SetMin      Action = "Set to minimum" // the coin's current minimum bet
)

var (
	Triggers = []Trigger{EveryN, EveryStreak, StreakAbove, FirstStreakN}
	Outcomes = []Outcome{Win, Lose, Any}
	Actions  = []Action{IncreasePct, DecreasePct, SetAmount, SetMin, ResetAmount, SetChance, SwitchSide, Stop}
)

// Condition is one "ON <trigger> <count> <outcome> DO <action> <value>" rule,
// modelled on DuckDice's advanced auto-bet conditions.
type Condition struct {
	Trigger Trigger `json:"trigger"`
	Count   int     `json:"count"`
	Outcome Outcome `json:"outcome"`
	Action  Action  `json:"action"`
	Value   float64 `json:"value,omitempty"`
}

// NeedsValue reports whether the action uses Value.
func (a Action) NeedsValue() bool {
	switch a {
	case IncreasePct, DecreasePct, SetAmount, SetChance:
		return true
	}
	return false
}

// Validate checks that c is well formed.
func (c Condition) Validate() error {
	switch c.Trigger {
	case EveryN, EveryStreak, StreakAbove, FirstStreakN:
	default:
		return fmt.Errorf("unknown trigger %q", c.Trigger)
	}
	switch c.Outcome {
	case Win, Lose:
	case Any:
		if c.Trigger != EveryN {
			return errors.New(`"Bet" can only be used with "Every"`)
		}
	default:
		return fmt.Errorf("unknown outcome %q", c.Outcome)
	}
	if c.Count < 1 && !(c.Trigger == StreakAbove && c.Count == 0) {
		return errors.New("count must be at least 1")
	}
	switch c.Action {
	case IncreasePct, SetAmount:
		if c.Value <= 0 {
			return fmt.Errorf("%s needs a value > 0", c.Action)
		}
	case DecreasePct:
		if c.Value <= 0 || c.Value >= 100 {
			return errors.New("decrease % must be between 0 and 100")
		}
	case SetChance:
		if c.Value < 0.01 || c.Value > 98 {
			return errors.New("win chance must be between 0.01 and 98")
		}
	case ResetAmount, SetMin, SwitchSide, Stop:
	default:
		return fmt.Errorf("unknown action %q", c.Action)
	}
	return nil
}

// String renders c the way DuckDice shows conditions.
func (c Condition) String() string {
	s := fmt.Sprintf("ON %s %d %s DO %s", c.Trigger, c.Count, c.Outcome, c.Action)
	if c.Action.NeedsValue() {
		s += fmt.Sprintf(" %g", c.Value)
	}
	return s
}

// Runner applies a profile bet by bet, tracking streaks over one run.
// It is not safe for concurrent use.
type Runner struct {
	P *BettingProfile

	// Next bet.
	Amount float64
	Chance float64
	High   bool

	Bets, Wins, Losses    int
	WinStreak, LoseStreak int
	Profit                float64
	StartBalance          float64

	// MinBet is the coin's current minimum bet, used by SetMin. The caller
	// keeps it up to date; when it is 0 SetMin falls back to BaseBet.
	MinBet float64
}

// NewRunner starts a run at the profile's base bet for balance.
func NewRunner(p *BettingProfile, balance float64, high bool) *Runner {
	return &Runner{P: p, Amount: p.Base(balance), Chance: p.Chance, High: high, StartBalance: balance}
}

func (r *Runner) fires(c Condition, won bool) bool {
	hit := (c.Outcome == Win && won) || (c.Outcome == Lose && !won) || c.Outcome == Any
	if !hit {
		return false
	}
	streak := r.WinStreak
	if !won {
		streak = r.LoseStreak
	}
	switch c.Trigger {
	case EveryN:
		n := r.Bets
		switch c.Outcome {
		case Win:
			n = r.Wins
		case Lose:
			n = r.Losses
		}
		return n%c.Count == 0
	case EveryStreak:
		return streak%c.Count == 0
	case StreakAbove:
		return streak > c.Count
	case FirstStreakN:
		return streak == c.Count
	}
	return false
}

// Record applies the result of the bet just placed (r.Amount) and sets up
// the next bet. balance is the balance after the bet. Conditions are applied
// in order; any amount action replaces the profile's on-win/on-loss rule.
func (r *Runner) Record(won bool, profit, balance float64) (stop bool, reason string) {
	r.Bets++
	r.Profit += profit
	if won {
		r.Wins++
		r.WinStreak++
		r.LoseStreak = 0
	} else {
		r.Losses++
		r.LoseStreak++
		r.WinStreak = 0
	}

	p := r.P
	next := p.Next(r.Amount, won, balance)
	amt, amtSet := r.Amount, false
	for i, c := range p.Conditions {
		if !r.fires(c, won) {
			continue
		}
		switch c.Action {
		case IncreasePct:
			amt, amtSet = amt*(1+c.Value/100), true
		case DecreasePct:
			amt, amtSet = amt*(1-c.Value/100), true
		case SetAmount:
			amt, amtSet = c.Value, true
		case SetMin:
			amt, amtSet = r.minBet(), true
		case ResetAmount:
			amt, amtSet = p.Base(balance), true
		case SetChance:
			r.Chance = c.Value
		case SwitchSide:
			r.High = !r.High
		case Stop:
			if !stop {
				stop, reason = true, fmt.Sprintf("condition %d (%s)", i+1, c)
			}
		}
	}
	if amtSet {
		next = p.capped(amt)
	}
	r.Amount = next
	if !stop {
		stop, reason = p.ShouldStopAt(r.Profit, r.StartBalance)
	}
	return stop, reason
}

func (r *Runner) minBet() float64 {
	if r.MinBet > 0 {
		return r.MinBet
	}
	return r.P.BaseBet
}

// UsesMinBet reports whether any condition needs the live minimum bet.
func (p *BettingProfile) UsesMinBet() bool {
	for _, c := range p.Conditions {
		if c.Action == SetMin {
			return true
		}
	}
	return false
}
