package core

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Job es una tarea larga (descarga, despliegue, actualización) cuyo progreso
// consulta el panel.
type Job struct {
	mu       sync.Mutex
	ID       string
	Kind     string
	Target   string
	State    string
	Progress int
	Steps    []JobStep
	Message  string
	Result   map[string]string
	started  time.Time
}

// JobView es la copia que se envía al panel.
type JobView struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Target   string            `json:"target"`
	State    string            `json:"state"`
	Progress int               `json:"progress"`
	Steps    []JobStep         `json:"steps"`
	Message  string            `json:"message"`
	Result   map[string]string `json:"result"`
}

type JobStep struct {
	Label string `json:"label"`
	State string `json:"state"`
}

func (j *Job) copy() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobView{ID: j.ID, Kind: j.Kind, Target: j.Target, State: j.State, Progress: j.Progress, Steps: append([]JobStep{}, j.Steps...), Message: j.Message, Result: j.Result}
}

func (j *Job) set(f func(j *Job)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	f(j)
}

// step marca el paso i como en curso y los anteriores como hechos.
func (j *Job) step(i int) {
	j.set(func(j *Job) {
		for k := range j.Steps {
			switch {
			case k < i:
				j.Steps[k].State = "done"
			case k == i:
				j.Steps[k].State = "running"
			}
		}
	})
}

func (j *Job) fail(msg string) {
	j.set(func(j *Job) {
		j.State, j.Message = "error", msg
		for k := range j.Steps {
			if j.Steps[k].State == "running" {
				j.Steps[k].State = "error"
			}
		}
	})
}

func (j *Job) finish(msg string, result map[string]string) {
	j.set(func(j *Job) {
		j.State, j.Message, j.Result, j.Progress = "done", msg, result, 100
		for k := range j.Steps {
			j.Steps[k].State = "done"
		}
	})
}

func (a *App) newJob(kind, target string, steps ...string) *Job {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	j := &Job{ID: hex.EncodeToString(b), Kind: kind, Target: target, State: "running", started: time.Now()}
	for _, s := range steps {
		j.Steps = append(j.Steps, JobStep{Label: s, State: "pending"})
	}
	a.mu.Lock()
	// Conserva solo los trabajos en curso y los de los últimos 10 minutos.
	keep := a.jobs[:0]
	for _, o := range a.jobs {
		if o.copy().State == "running" || time.Since(o.started) < 10*time.Minute {
			keep = append(keep, o)
		}
	}
	a.jobs = append(keep, j)
	a.mu.Unlock()
	return j
}

func (a *App) Job(id string) (JobView, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, j := range a.jobs {
		if j.ID == id {
			return j.copy(), true
		}
	}
	return JobView{}, false
}

// busyTargets devuelve los elementos con una tarea en curso.
func (a *App) busyTargets() map[string]bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := map[string]bool{}
	for _, j := range a.jobs {
		if c := j.copy(); c.State == "running" {
			out[c.Target] = true
		}
	}
	return out
}

func (a *App) busy(kind, target string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, j := range a.jobs {
		c := j.copy()
		if c.State == "running" && c.Target == target && (kind == "" || c.Kind == kind) {
			return true
		}
	}
	return false
}
