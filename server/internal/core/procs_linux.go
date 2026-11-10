//go:build linux

package core

import (
	"errors"
	"os"
	"strconv"
	"sync"
)

type procReader struct {
	mu sync.Mutex
	s  procSampler
}

// read recorre /proc. Devuelve ok=false en la primera lectura, que solo
// sirve de referencia para la siguiente.
func (r *procReader) read(names func(string) string) (CPUNow, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return CPUNow{}, false, errors.New("No se pudo leer /proc: " + err.Error())
	}
	procs := make([]procStat, 0, len(ents))
	for _, e := range ents {
		if c := e.Name()[0]; c < '0' || c > '9' {
			continue
		}
		if ps, ok := parseProcStat(readStr("/proc/" + e.Name() + "/stat")); ok {
			procs = append(procs, ps)
		}
	}
	cg := func(pid int) string { return readStr("/proc/" + strconv.Itoa(pid) + "/cgroup") }
	n, ok := r.s.sampleFrom(readStr("/proc/stat"), procs, cg, names, int64(os.Getpagesize()/1024))
	return n, ok, nil
}
