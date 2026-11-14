package main

import "errors"

// Validatable is implemented by values that can check their own fields.
// Validate returns nil if the value is valid and otherwise an error that
// describes the first problem found. It neither logs nor changes the value:
// the caller decides what to do with the error.
type Validatable interface {
	Validate() error
}

// Transactions and budgets check their fields before they are stored.
var (
	_ Validatable = Transaction{}
	_ Validatable = Budget{}
)

// ErrNothingToValidate is returned by CheckValid for a nil value.
var ErrNothingToValidate = errors.New("nothing to validate")

// CheckValid validates v through the Validatable interface, so it handles
// transactions, budgets and any other type with a Validate method the same
// way.
func CheckValid(v Validatable) error {
	if v == nil {
		return ErrNothingToValidate
	}
	return v.Validate()
}
