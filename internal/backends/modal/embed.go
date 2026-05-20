// Package modal is Proteus's cloud (Modal) compute backend: the deployable
// Modal app and the Go client that invokes it.
package modal

import _ "embed"

// FunctionsPy is the Modal app source, embedded so `proteus modal deploy` can
// write it out and deploy it.
//
//go:embed functions.py
var FunctionsPy string
