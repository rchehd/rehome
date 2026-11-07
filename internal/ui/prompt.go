package ui

import (
	"errors"

	"charm.land/huh/v2"
)

// Choice is one answer to Choose: Key comes back, Label is shown.
type Choice struct {
	Key   string
	Label string
}

// Confirm asks a yes/no question; cancelling counts as no.
func Confirm(question string, def bool) (bool, error) {
	ok := def
	err := huh.NewConfirm().Title(question).Value(&ok).Run()

	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}

	return ok, err
}

// Choose asks for one of choices; cancelling returns "".
func Choose(question string, choices ...Choice) (string, error) {
	var key string
	options := make([]huh.Option[string], 0, len(choices))

	for _, c := range choices {
		options = append(options, huh.NewOption(c.Label, c.Key))
	}

	err := huh.NewSelect[string]().Title(question).Options(options...).Value(&key).Run()

	if errors.Is(err, huh.ErrUserAborted) {
		return "", nil
	}

	return key, err
}
