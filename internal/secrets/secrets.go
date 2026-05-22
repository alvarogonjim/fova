// Package secrets reads and writes fova's secrets (LLM API keys) in the OS
// keychain, under the same "fova" service the Adaptyv token uses.
package secrets

import "github.com/99designs/keyring"

const service = "fova"

// open returns the keyring fova stores secrets in. It is a package var so
// tests can swap in an in-memory keyring via UseInMemoryKeyring.
var open = func() (keyring.Keyring, error) {
	return keyring.Open(keyring.Config{ServiceName: service})
}

// APIKeyName is the keychain entry name for a provider's API key. The wizard
// (writer) and the llm package (reader) both derive the name this way.
func APIKeyName(provider string) string { return provider + "-api-key" }

// Get returns the secret stored under name, and whether it was found.
func Get(name string) (string, bool) {
	ring, err := open()
	if err != nil {
		return "", false
	}
	item, err := ring.Get(name)
	if err != nil {
		return "", false
	}
	return string(item.Data), true
}

// Set stores value under name in the OS keychain.
func Set(name, value string) error {
	ring, err := open()
	if err != nil {
		return err
	}
	return ring.Set(keyring.Item{Key: name, Data: []byte(value)})
}

// UseInMemoryKeyring swaps the keyring for an in-memory one and returns a
// function that restores the previous opener. For tests in this and other
// packages.
func UseInMemoryKeyring() func() {
	prev := open
	ring := keyring.NewArrayKeyring(nil)
	open = func() (keyring.Keyring, error) { return ring, nil }
	return func() { open = prev }
}
