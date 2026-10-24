package profile

import (
	"math"
	"math/rand"
	"sort"
)

// HouseEdge is DuckDice's edge in percent: a win pays (100-HouseEdge)/chance.
const HouseEdge = 1.0

// SimConfig describes a Monte Carlo test of a profile.
type SimConfig struct {
	StartBalance float64
	Sessions     int   // independent runs
	MaxBets      int   // bets per run unless a stop rule ends it first
	Seed         int64 // same seed, same results
}

// SimResult summarises Simulate. Percentages are of StartBalance.
type SimResult struct {
	Sessions int

	MeanPct, MedianPct float64
	P10Pct, P90Pct     float64
	WorstPct, BestPct  float64

	ProfitableShare float64 // sessions that ended up
	TakeProfitShare float64 // sessions ended by take profit
	StopLossShare   float64 // sessions ended by stop loss
	BustShare       float64 // sessions that could not cover the next bet
	ConditionShare  float64 // sessions ended by a Stop condition

	AvgBets        float64
	AvgWageredX    float64 // total bet per session ÷ start balance
	MaxDrawdownPct float64 // average worst dip below the start, per session
}

// Simulate runs p for cfg.Sessions independent sessions on random rolls
// with DuckDice's payout. It ignores the minimum bet and network effects.
func Simulate(p *BettingProfile, cfg SimConfig) SimResult {
	rng := rand.New(rand.NewSource(cfg.Seed))
	res := SimResult{Sessions: cfg.Sessions}
	if cfg.Sessions <= 0 || cfg.StartBalance <= 0 {
		return res
	}
	pcts := make([]float64, 0, cfg.Sessions)
	var sumBets, sumWager, sumDD float64
	var tp, sl, bust, cond, up int

	for s := 0; s < cfg.Sessions; s++ {
		bal := cfg.StartBalance
		r := NewRunner(p, bal, true)
		var wagered, low float64
		bets := 0
		for ; bets < cfg.MaxBets; bets++ {
			amt := r.Amount
			if amt > bal || amt <= 0 || math.IsNaN(amt) {
				bust++
				break
			}
			won := rng.Float64()*100 < r.Chance
			profit := -amt
			if won {
				profit = amt * ((100-HouseEdge)/r.Chance - 1)
			}
			bal += profit
			wagered += amt
			low = min(low, bal-cfg.StartBalance)
			if stop, why := r.Record(won, profit, bal); stop {
				bets++
				switch why {
				case "take profit reached":
					tp++
				case "stop loss reached":
					sl++
				default:
					cond++
				}
				break
			}
		}
		pct := (bal - cfg.StartBalance) / cfg.StartBalance * 100
		pcts = append(pcts, pct)
		if pct > 0 {
			up++
		}
		sumBets += float64(bets)
		sumWager += wagered / cfg.StartBalance
		sumDD += -low / cfg.StartBalance * 100
	}

	n := float64(cfg.Sessions)
	sort.Float64s(pcts)
	at := func(q float64) float64 { return pcts[int(q*float64(len(pcts)-1))] }
	var sum float64
	for _, v := range pcts {
		sum += v
	}
	res.MeanPct = sum / n
	res.MedianPct = at(0.5)
	res.P10Pct, res.P90Pct = at(0.1), at(0.9)
	res.WorstPct, res.BestPct = pcts[0], pcts[len(pcts)-1]
	res.ProfitableShare = float64(up) / n
	res.TakeProfitShare = float64(tp) / n
	res.StopLossShare = float64(sl) / n
	res.BustShare = float64(bust) / n
	res.ConditionShare = float64(cond) / n
	res.AvgBets = sumBets / n
	res.AvgWageredX = sumWager / n
	res.MaxDrawdownPct = sumDD / n
	return res
}
