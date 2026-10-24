package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

var betLogHeader = []string{
	"time", "source", "game", "coin", "amount", "chance", "target", "roll",
	"result", "payout", "profit", "balance", "jackpot", "nonce", "hash",
}

// betLog appends every bet to a monthly CSV file (bets-2006-01.csv) so
// results can be reviewed after the app is closed. Safe for concurrent use.
type betLog struct {
	dir string
	mu  sync.Mutex
}

// openBetLog uses <root>/logs (./logs when root is ""), falling back to the
// user config dir when that is not writable (same rule as profiles).
func openBetLog(root string) *betLog {
	dir := filepath.Join(root, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		cfg, cerr := os.UserConfigDir()
		if cerr != nil {
			cfg = os.TempDir()
		}
		dir = filepath.Join(cfg, "duckdice-bot", "logs")
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		dir = abs
	}
	return &betLog{dir: dir}
}

// Log appends one bet. source says what placed it, e.g. "manual" or
// "auto: <profile>".
func (l *betLog) Log(source string, r *PlayResponse, when time.Time) error {
	b := r.Bet
	result := "loss"
	if b.Result {
		result = "win"
	}
	target := b.Choice
	if b.ChoiceOption != "" {
		target += " " + b.ChoiceOption
	}
	row := []string{
		when.Format(time.RFC3339), source, b.Game.Name, b.Symbol, b.BetAmount,
		strconv.FormatFloat(b.Chance, 'f', -1, 64), target, strconv.Itoa(b.Number),
		result, strconv.FormatFloat(b.Payout, 'f', -1, 64), b.Profit, r.User.Balance,
		strconv.FormatBool(r.IsJackpot), strconv.Itoa(b.Nonce), b.Hash,
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(l.dir, "bets-"+when.Format("2006-01")+".csv")
	_, statErr := os.Stat(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if errors.Is(statErr, os.ErrNotExist) {
		_ = w.Write(betLogHeader)
	}
	_ = w.Write(row)
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return fmt.Errorf("bet log: %w", err)
	}
	return f.Close()
}
