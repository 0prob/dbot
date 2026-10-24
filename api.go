package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://duckdice.io/api"

// Client is a minimal DuckDice Bot API client (https://duckdice.io/bot-api).
type Client struct {
	Key  string
	HTTP *http.Client
}

func NewClient(key string) *Client {
	return &Client{Key: key, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

type Level struct {
	Level  int `json:"level"`
	XP     int `json:"xp"`
	XPNext int `json:"xpNext"`
	XPPrev int `json:"xpPrev"`
}

type User struct {
	Hash          string  `json:"hash"`
	Username      string  `json:"username"`
	Bets          int     `json:"bets"`
	Nonce         int     `json:"nonce"`
	Wins          int     `json:"wins"`
	Luck          float64 `json:"luck"`
	Balance       string  `json:"balance"`
	Profit        string  `json:"profit"`
	Volume        string  `json:"volume"`
	AbsoluteLevel Level   `json:"absoluteLevel"`
}

type Game struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Bet struct {
	Hash         string  `json:"hash"`
	Symbol       string  `json:"symbol"`
	Result       bool    `json:"result"`
	Choice       string  `json:"choice"`
	ChoiceOption string  `json:"choiceOption"`
	Number       int     `json:"number"`
	Chance       float64 `json:"chance"`
	Payout       float64 `json:"payout"`
	BetAmount    string  `json:"betAmount"`
	WinAmount    string  `json:"winAmount"`
	Profit       string  `json:"profit"`
	Mined        string  `json:"mined"`
	Nonce        int     `json:"nonce"`
	Created      int64   `json:"created"`
	GameMode     string  `json:"gameMode"`
	Game         Game    `json:"game"`
}

type PlayResponse struct {
	Bet           Bet   `json:"bet"`
	IsJackpot     bool  `json:"isJackpot"`
	JackpotStatus *bool `json:"jackpotStatus"`
	Jackpot       *struct {
		Amount string `json:"amount"`
	} `json:"jackpot"`
	User User `json:"user"`
}

type DiceRequest struct {
	Symbol                string `json:"symbol"`
	Chance                string `json:"chance"`
	IsHigh                bool   `json:"isHigh"`
	Amount                string `json:"amount"`
	UserWageringBonusHash string `json:"userWageringBonusHash,omitempty"`
	TLEHash               string `json:"tleHash,omitempty"`
}

type RangeRequest struct {
	Symbol                string `json:"symbol"`
	Range                 [2]int `json:"range"`
	IsIn                  bool   `json:"isIn"`
	Amount                string `json:"amount"`
	UserWageringBonusHash string `json:"userWageringBonusHash,omitempty"`
	TLEHash               string `json:"tleHash,omitempty"`
}

type Stats struct {
	Bets     int    `json:"bets"`
	Wins     int    `json:"wins"`
	Profit   string `json:"profit"`
	Volume   string `json:"volume"`
	Balances struct {
		Main string `json:"main"`
	} `json:"balances"`
}

type Balance struct {
	Currency  string `json:"currency"`
	Main      string `json:"main"`
	Affiliate string `json:"affiliate"`
}

type WageringBonus struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Hash   string `json:"hash"`
	Status string `json:"status"`
	Symbol string `json:"symbol"`
	Margin string `json:"margin"`
}

type TLE struct {
	Hash   string `json:"hash"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type UserInfo struct {
	Hash        string `json:"hash"`
	Username    string `json:"username"`
	CreatedAt   int64  `json:"createdAt"`
	Campaign    string `json:"campaign"`
	Affiliate   string `json:"affiliate"`
	LastDeposit *struct {
		CreatedAt int64  `json:"createdAt"`
		Currency  string `json:"currency"`
		Amount    string `json:"amount"`
	} `json:"lastDeposit"`
	Wagered []struct {
		Currency string `json:"currency"`
		Amount   string `json:"amount"`
	} `json:"wagered"`
	Balances        []Balance       `json:"balances"`
	WageringBonuses []WageringBonus `json:"wageringBonuses"`
	TLE             []TLE           `json:"tle"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	u := apiBase + path + "?api_key=" + url.QueryEscape(c.Key)
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "DuckDiceBot/1.0.0")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return apiError(resp.StatusCode, data)
	}
	return json.Unmarshal(data, out)
}

// APIError is a non-2xx response. Body is kept so callers can dig out
// details such as the minimum bet.
type APIError struct {
	Code int
	Msg  string
	Body string
}

func (e *APIError) Error() string { return e.Msg }

// apiError extracts a readable message from an error response body.
func apiError(code int, data []byte) error {
	return &APIError{Code: code, Msg: apiMessage(code, data).Error(), Body: string(data)}
}

func apiMessage(code int, data []byte) error {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &e) == nil {
		if e.Message != "" {
			return fmt.Errorf("%d: %s", code, e.Message)
		}
		if e.Error != "" {
			return fmt.Errorf("%d: %s", code, e.Error)
		}
	}
	s := string(data)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return fmt.Errorf("HTTP %d: %s", code, s)
}

func (c *Client) PlayDice(ctx context.Context, r DiceRequest) (*PlayResponse, error) {
	var out PlayResponse
	return &out, c.do(ctx, http.MethodPost, "/dice/play", r, &out)
}

func (c *Client) PlayRange(ctx context.Context, r RangeRequest) (*PlayResponse, error) {
	var out PlayResponse
	return &out, c.do(ctx, http.MethodPost, "/range-dice/play", r, &out)
}

func (c *Client) Stats(ctx context.Context, symbol string) (*Stats, error) {
	var out Stats
	return &out, c.do(ctx, http.MethodGet, "/bot/stats/"+url.PathEscape(symbol), nil, &out)
}

func (c *Client) UserInfo(ctx context.Context) (*UserInfo, error) {
	var out UserInfo
	return &out, c.do(ctx, http.MethodGet, "/bot/user-info", nil, &out)
}

var numRe = regexp.MustCompile(`\d+(?:\.\d+)?(?:[eE]-?\d+)?`)

// MinBetFromError returns the minimum bet mentioned in a bet error, if any,
// e.g. "The minimum bet amount is 0.00001 BTC" or "must be at least 0.01".
// The exact wording isn't documented, so it takes the first positive number
// after a minimum-like phrase.
func MinBetFromError(err error) (float64, bool) {
	var ae *APIError
	if !errors.As(err, &ae) {
		return 0, false
	}
	body := strings.ToLower(ae.Body)
	i := -1
	for _, kw := range []string{"min", "at least", "less than", "lower than", "too small", "too low"} {
		if j := strings.Index(body, kw); j >= 0 && (i < 0 || j < i) {
			i = j
		}
	}
	if i < 0 {
		return 0, false
	}
	for _, m := range numRe.FindAllString(body[i:], -1) {
		if v, perr := strconv.ParseFloat(m, 64); perr == nil && v > 0 {
			return v, true
		}
	}
	return 0, false
}

// ProbeMinBet asks the server for symbol's current minimum bet by placing a
// zero-amount bet, which the server rejects with the minimum in the error.
// No funds are at risk. If the server accepts the zero bet, min is 0 and
// the (free) bet is returned.
func (c *Client) ProbeMinBet(ctx context.Context, symbol string) (min float64, accepted *PlayResponse, err error) {
	r, err := c.PlayDice(ctx, DiceRequest{Symbol: symbol, Chance: "50", IsHigh: true, Amount: "0"})
	if err == nil {
		return 0, r, nil
	}
	if v, ok := MinBetFromError(err); ok {
		return v, nil, nil
	}
	return 0, nil, err
}
