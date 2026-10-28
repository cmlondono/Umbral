package core

import (
	"path/filepath"
	"sort"
	"sync"
	"time"
)

/* Disponibilidad de los servicios: cada 15 s se anota si cada contenedor (y
 * la aplicación Nextcloud) funciona. Se guardan cubos de una hora durante 7
 * días y las últimas caídas, en avail.json. Sin contenedores extra. */

type availBucket struct {
	H  int64 `json:"h"` // inicio de la hora (unix)
	Up int   `json:"up"`
	N  int   `json:"n"`
}

type Incident struct {
	Target string     `json:"target"`
	From   time.Time  `json:"from"`
	To     *time.Time `json:"to"` // nil: sigue caído
}

type availData struct {
	Buckets   map[string][]availBucket `json:"buckets"`
	Incidents []Incident               `json:"incidents"`
}

type AvailView struct {
	Target string     `json:"target"`
	Pct24h *float64   `json:"pct24h"`
	Pct7d  *float64   `json:"pct7d"`
	Hours  int        `json:"hours"` // horas con datos en los últimos 7 días
	Down   *time.Time `json:"down"`  // caído desde
}

type Avail struct {
	mu sync.Mutex
	d  availData
	f  jsonFile[availData]
}

// AppTarget es el nombre con el que se anota la aplicación Nextcloud (no el contenedor).
const AppTarget = "app:nextcloud"

func newAvail(dataDir string) *Avail {
	v := &Avail{}
	v.f.path = filepath.Join(dataDir, "avail.json")
	_ = v.f.Load(&v.d)
	if v.d.Buckets == nil {
		v.d.Buckets = map[string][]availBucket{}
	}
	return v
}

// Record anota el estado de cada servicio en este instante.
func (v *Avail) Record(now time.Time, up map[string]bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	h := now.Truncate(time.Hour).Unix()
	cut := now.Add(-7 * 24 * time.Hour).Unix()
	for t, ok := range up {
		bs := v.d.Buckets[t]
		if n := len(bs); n == 0 || bs[n-1].H != h {
			bs = append(bs, availBucket{H: h})
		}
		b := &bs[len(bs)-1]
		b.N++
		if ok {
			b.Up++
		}
		v.d.Buckets[t] = bs
		v.incident(t, ok, now)
	}
	for t, bs := range v.d.Buckets {
		i := 0
		for i < len(bs) && bs[i].H < cut {
			i++
		}
		if i == len(bs) {
			delete(v.d.Buckets, t)
		} else if i > 0 {
			v.d.Buckets[t] = bs[i:]
		}
	}
}

// incident abre una caída al pasar a «no funciona» y la cierra al volver.
// Una caída de menos de 30 s (un solo instante) no cuenta como incidencia.
func (v *Avail) incident(t string, ok bool, now time.Time) {
	var open *Incident
	for i := range v.d.Incidents {
		if v.d.Incidents[i].Target == t && v.d.Incidents[i].To == nil {
			open = &v.d.Incidents[i]
			break
		}
	}
	switch {
	case !ok && open == nil:
		v.d.Incidents = append([]Incident{{Target: t, From: now}}, v.d.Incidents...)
		if len(v.d.Incidents) > 100 {
			v.d.Incidents = v.d.Incidents[:100]
		}
	case ok && open != nil:
		if now.Sub(open.From) < 30*time.Second {
			for i := range v.d.Incidents {
				if &v.d.Incidents[i] == open {
					v.d.Incidents = append(v.d.Incidents[:i], v.d.Incidents[i+1:]...)
					break
				}
			}
			return
		}
		end := now
		open.To = &end
	}
}

// Forget cierra las caídas abiertas de servicios que ya no existen.
func (v *Avail) Forget(exists map[string]bool, now time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for i := range v.d.Incidents {
		if in := &v.d.Incidents[i]; in.To == nil && !exists[in.Target] {
			end := now
			in.To = &end
		}
	}
}

func (v *Avail) Views(now time.Time) ([]AvailView, []Incident) {
	v.mu.Lock()
	defer v.mu.Unlock()
	day := now.Add(-24 * time.Hour).Unix()
	out := []AvailView{}
	for t, bs := range v.d.Buckets {
		var u24, n24, u7, n7 int
		for _, b := range bs {
			u7, n7 = u7+b.Up, n7+b.N
			if b.H >= day {
				u24, n24 = u24+b.Up, n24+b.N
			}
		}
		av := AvailView{Target: t, Hours: len(bs)}
		if n24 > 0 {
			p := float64(u24) * 100 / float64(n24)
			av.Pct24h = &p
		}
		if n7 > 0 {
			p := float64(u7) * 100 / float64(n7)
			av.Pct7d = &p
		}
		for _, in := range v.d.Incidents {
			if in.Target == t && in.To == nil {
				f := in.From
				av.Down = &f
			}
		}
		out = append(out, av)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	inc := append([]Incident{}, v.d.Incidents...)
	return out, inc
}

func (v *Avail) Save() {
	v.mu.Lock()
	d := availData{Buckets: map[string][]availBucket{}, Incidents: append([]Incident{}, v.d.Incidents...)}
	for k, b := range v.d.Buckets {
		d.Buckets[k] = append([]availBucket{}, b...)
	}
	v.mu.Unlock()
	_ = v.f.Save(d)
}

// recordAvail anota el estado actual de los contenedores y de Nextcloud.
func (a *App) recordAvail() {
	a.mu.RLock()
	if !a.docker.Available {
		a.mu.RUnlock()
		return
	}
	up := map[string]bool{}
	for _, c := range a.docker.Containers {
		up[c.Name] = c.State == "running" && c.Health != "unhealthy"
	}
	nc := a.nextcloud
	a.mu.RUnlock()
	// El modo de mantenimiento es una parada prevista: no cuenta.
	if nc.Configured && !(nc.Maintenance != nil && *nc.Maintenance) {
		up[AppTarget] = nc.Reachable && nc.HTTPStatus != nil && *nc.HTTPStatus == 200
	}
	now := time.Now()
	a.Avail.Record(now, up)
	a.Avail.Forget(up, now)
}
