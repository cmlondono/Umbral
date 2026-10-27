//go:build !linux

package core

import (
	"errors"
	"net"
	"os"
	"runtime"
)

// Fuera de Linux el panel arranca para poder desarrollarlo, pero no inventa
// métricas: informa de que no están disponibles.

func (s *Sampler) Sample() (Metrics, error) {
	return Metrics{}, errors.New("Las métricas solo están disponibles en el servidor Linux.")
}

func ReadServerInfo() ServerInfo {
	host, _ := os.Hostname()
	return ServerInfo{Hostname: host, OS: runtime.GOOS + " (desarrollo)", Cores: runtime.NumCPU()}
}

func defaultRoute() (string, net.IP) { return "", nil }

func readARP() []arpEntry { return nil }
