# DuckDice Bot

Single-binary desktop client (Go + Fyne) for the [DuckDice Bot API](https://duckdice.io/bot-api).

- Original Dice and Range Dice bets
- Auto-bet driven by the applied strategy profile, with bet count and delay
- Live balance, session stats, profit chart, and the last 500 bets
- Wagering bonus and time-limited-event hashes picked from your account
- Strategy profiles (`./profiles/*.json`) with on-win/on-loss rules, streak conditions
  (including "Set to minimum", which follows DuckDice's live minimum bet), bankroll-% sizing,
  fixed or % stop loss / take profit, and an optional coin
- Min button: asks DuckDice for the coin's current minimum bet (via a free zero-amount bet)
- "Test strategy": Monte Carlo simulation of the current profile before betting real coins
- USD values next to the balance, bet amount, profit on win and session totals (prices from
  Coinbase, then CoinGecko, refreshed every minute; DuckDice's DECOY token has no market price)
- Every bet is appended to `./logs/bets-YYYY-MM.csv` (the "Bet log" button opens the folder)

## Where data is kept

| What | Where |
|---|---|
| API key | System keyring (KWallet / GNOME Keyring, Windows Credential Manager, macOS Keychain). Plain-text Fyne preferences only if no keyring is available. |
| Profiles | `./profiles/` (falls back to `~/.config/duckdice-bot/profiles`) |
| Bet log | `./logs/` (falls back to `~/.config/duckdice-bot/logs`) |

`.gitignore` excludes profiles, logs, CSVs, build outputs and common secret files, so none of these
can be committed by accident.

## Android

```bash
go install fyne.io/tools/cmd/fyne@latest
ANDROID_NDK_HOME=~/a/sdk/ndk/30.0.16138531 fyne package -os android/arm64
adb install -r DuckDice_Bot.apk
```

The same code runs as a phone app with Bet / Strategy / Session tabs. Profiles, bet logs and the API
key are kept in the app's private storage (Android has no desktop keyring). This is a debug-signed
APK for sideloading; Play Store release would need `fyne release` with your own keystore.
`DBOT_MOBILE=1 ./duckdice-bot` previews the phone layout on the desktop.

## Desktop shortcut

```bash
./install-shortcut.sh
```

Adds "DuckDice Bot" to the application menu and the desktop, starting in this folder so
`profiles/` and `logs/` stay here. Re-run it if you move the folder.

## Build

```bash
go build -trimpath -ldflags "-s -w" -o duckdice-bot .
```

Linux build needs `gcc` and the X11/GL dev headers (`libgl1-mesa-dev xorg-dev`). At runtime the binary only uses the system's libGL/X11, which every desktop already has.

Cross-compile for Windows/macOS with [fyne-cross](https://github.com/fyne-io/fyne-cross):

```bash
fyne-cross windows -arch=amd64
```
