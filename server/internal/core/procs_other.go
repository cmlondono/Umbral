//go:build !linux

package core

import "errors"

type procReader struct{}

func (r *procReader) read(names func(string) string) (CPUNow, bool, error) {
	return CPUNow{}, false, errors.New("Los procesos solo se pueden leer en el servidor Linux.")
}
