package app

import (
	"time"

	"github.com/google/uuid"
)

// Config uses types that the type map replaces.
type Config struct {
	// The uuid package is not loaded, but the type map knows its UUID.
	ID     uuid.UUID   `json:"id"`
	Parent *uuid.UUID  `json:"parent,omitempty"`
	Owners []uuid.UUID `json:"owners"`

	// The alias keeps the union whole when the pointer makes it nullable.
	Port *IntOrString `json:"port"`

	Ports   Range[uint16] `json:"ports"`
	Timeout time.Duration `json:"timeout"`

	// String hides the built-in type at module level, so the mapped types
	// reference it through pkl:base.
	String string
}

// IntOrString holds an integer or a string. Its marshaler alone would make
// it Any.
type IntOrString struct {
	Int int
	Str string
}

func (v IntOrString) MarshalJSON() ([]byte, error) {
	return nil, nil
}

// Range is written as "min-max".
type Range[T any] struct {
	Min, Max T
}
