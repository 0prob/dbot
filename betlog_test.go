package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBetLog(t *testing.T) {
	l := &betLog{dir: filepath.Join(t.TempDir(), "logs")}
	when := time.Date(2026, 10, 23, 15, 4, 5, 0, time.UTC)
	r := &PlayResponse{Bet: Bet{Symbol: "DECOY", Result: true, Choice: ">5049", Number: 7777, Chance: 49.5,
		Payout: 2, BetAmount: "0.01", Profit: "0.01", Nonce: 3, Hash: "abc", Game: Game{Name: "Original Dice"}}}
	r.User.Balance = "100.01"
	for i := 0; i < 2; i++ {
		if err := l.Log("auto: test", r, when); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(filepath.Join(l.dir, "bets-2026-10.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0][0] != "time" {
		t.Fatalf("want header + 2 rows, got %v", rows)
	}
	if got := rows[1]; got[1] != "auto: test" || got[3] != "DECOY" || got[7] != "7777" || got[8] != "win" || got[11] != "100.01" {
		t.Fatalf("bad row %v", got)
	}
	if fi, _ := os.Stat(filepath.Join(l.dir, "bets-2026-10.csv")); fi.Mode().Perm() != 0o600 {
		t.Errorf("log should be private, mode %v", fi.Mode().Perm())
	}
}
