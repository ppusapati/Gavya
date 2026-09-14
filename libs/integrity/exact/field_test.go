package exact

import (
	"errors"
	"testing"
)

func TestFieldNamesTheValueThatWasRefused(t *testing.T) {
	err := Field("tax_rate", ErrTooPrecise)
	if !errors.Is(err, ErrTooPrecise) {
		t.Errorf("the cause was lost: %v", err)
	}
	if err.Error() != "tax_rate: value is finer than this field is recorded to" {
		t.Errorf("message = %q", err.Error())
	}
	if Field("tax_rate", nil) != nil {
		t.Error("a nil error was wrapped into a non-nil one")
	}
}
