// Package lab integrates Proteus with Adaptyv Bio's Foundry wet-lab API:
// the HTTP client, the agent-facing lab.* tools, and the result webhook.
package lab

import (
	"errors"
	"os"

	"github.com/99designs/keyring"
)

// keychainService is the OS-keychain service name Proteus stores secrets under.
const keychainService = "proteus"

// adaptyvTokenKey is the keychain key holding the Adaptyv API token.
const adaptyvTokenKey = "adaptyv"

// Token returns the Adaptyv API token: $ADAPTYV_API_TOKEN when set, otherwise
// the value stored in the OS keychain. It errors when neither is available.
func Token() (string, error) {
	if t := os.Getenv("ADAPTYV_API_TOKEN"); t != "" {
		return t, nil
	}
	ring, err := openKeyring()
	if err != nil {
		return "", errors.New("no Adaptyv token: set $ADAPTYV_API_TOKEN or run /auth adaptyv")
	}
	item, err := ring.Get(adaptyvTokenKey)
	if err != nil {
		return "", errors.New("no Adaptyv token: set $ADAPTYV_API_TOKEN or run /auth adaptyv")
	}
	return string(item.Data), nil
}

// StoreToken writes the Adaptyv API token to the OS keychain.
func StoreToken(token string) error {
	ring, err := openKeyring()
	if err != nil {
		return err
	}
	return ring.Set(keyring.Item{Key: adaptyvTokenKey, Data: []byte(token)})
}

// openKeyring opens the Proteus keychain.
func openKeyring() (keyring.Keyring, error) {
	return keyring.Open(keyring.Config{ServiceName: keychainService})
}
