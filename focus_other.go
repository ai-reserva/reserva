//go:build !linux && !darwin

package main

import "errors"

func focusWindowOf(int) error {
	return errors.New("window focusing is not supported on this platform")
}
