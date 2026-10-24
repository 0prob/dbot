package main

import (
	"fyne.io/fyne/v2"
	"github.com/zalando/go-keyring"
)

// The API key lives in the OS keyring (Secret Service / KWallet on Linux,
// Credential Manager on Windows, Keychain on macOS). Only when no keyring
// is available does it fall back to Fyne preferences, which are plain text.
const (
	keyringService = "duckdice-bot"
	keyringUser    = "api_key"
)

// loadAPIKey returns the stored key and whether it is in the keyring. A key
// found in plain-text preferences (older versions) is moved to the keyring.
// It may block while the keyring asks the user to unlock it.
//
// With useKeyring false (phones, which have no Secret Service) the key is
// kept in preferences, i.e. the app's private storage.
func loadAPIKey(prefs fyne.Preferences, useKeyring bool) (key string, inKeyring bool) {
	if !useKeyring {
		return prefs.String(prefKey), false
	}
	if k, err := keyring.Get(keyringService, keyringUser); err == nil {
		prefs.RemoveValue(prefKey) // in case an old plain-text copy remains
		return k, true
	}
	old := prefs.String(prefKey)
	if old == "" {
		return "", false
	}
	if keyring.Set(keyringService, keyringUser, old) == nil {
		prefs.RemoveValue(prefKey)
		return old, true
	}
	return old, false
}

// saveAPIKey stores key (or removes it when empty), preferring the keyring.
func saveAPIKey(prefs fyne.Preferences, key string, useKeyring bool) (inKeyring bool) {
	if !useKeyring {
		if key == "" {
			prefs.RemoveValue(prefKey)
		} else {
			prefs.SetString(prefKey, key)
		}
		return false
	}
	if key == "" {
		_ = keyring.Delete(keyringService, keyringUser)
		prefs.RemoveValue(prefKey)
		return false
	}
	if keyring.Set(keyringService, keyringUser, key) == nil {
		prefs.RemoveValue(prefKey)
		return true
	}
	prefs.SetString(prefKey, key)
	return false
}
