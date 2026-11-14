package ledger

import (
	"errors"
	"reflect"
)

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
// way. A nil v, including a nil pointer such as (*Transaction)(nil), gives
// ErrNothingToValidate instead of a panic: Validate is never called for
// a nil pointer, even if the type could handle a nil receiver.
func CheckValid(v Validatable) error {
	if v == nil {
		return ErrNothingToValidate
	}
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
		return ErrNothingToValidate
	}
	return v.Validate()
}
