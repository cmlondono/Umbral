package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

/* Automatizaciones: «Cuando pase esto, si se cumple esto, haz esto».
 *
 * El motor no hace lecturas propias: se evalúa cada 15 s con el estado que el
 * servicio ya recoge (métricas, Docker, Nextcloud, red y PC). Cada regla
 * recuerda si su disparador está activo y desde cuándo, de modo que:
 *  - solo se dispara en el paso de «inactivo» a «activo» (no repite);
 *  - puede exigir una duración mínima («5 min por encima del 90 %»);
 *  - se rearma cuando la situación vuelve a la normalidad (con un margen en
 *    los umbrales, para no oscilar);
 *  - al arrancar el servicio no dispara por situaciones que ya existían.
 * Las acciones son una lista cerrada y segura: no hay órdenes libres. */

type Trigger struct {
	Kind      string  `json:"kind"` // schedule | container | app | metric | presence | pc | hook
	Days      []int   `json:"days,omitempty"`
	Time      string  `json:"time,omitempty"`
	Container string  `json:"container,omitempty"`
	Event     string  `json:"event,omitempty"` // container: stopped|unhealthy|running · presence: arrive|leave · pc: on|off
	Metric    string  `json:"metric,omitempty"` // cpu | mem | disk | temp
	Above     float64 `json:"above,omitempty"`
	ForMin    int     `json:"forMin,omitempty"`
	MAC       string  `json:"mac,omitempty"`
}

type Condition struct {
	Kind      string `json:"kind"` // time | days | device | pc | container
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Days      []int  `json:"days,omitempty"`
	MAC       string `json:"mac,omitempty"`
	Present   bool   `json:"present,omitempty"`
	PC        string `json:"pc,omitempty"` // on | off
	Container string `json:"container,omitempty"`
	Running   bool   `json:"running,omitempty"`
}

type Step struct {
	Kind      string `json:"kind"` // wake | container | backup | scan | wait | http | updates
	Container string `json:"container,omitempty"`
	Op        string `json:"op,omitempty"` // start | stop | restart
	WaitOn    bool   `json:"waitOn,omitempty"`
	Minutes   int    `json:"minutes,omitempty"`
	URL       string `json:"url,omitempty"`
	Method    string `json:"method,omitempty"` // GET | POST
}

type RunSummary struct {
	At     time.Time `json:"at"`
	Result string    `json:"result"` // ok | error | skipped
	Detail string    `json:"detail"`
}

type Rule struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Enabled     bool        `json:"enabled"`
	Trigger     Trigger     `json:"trigger"`
	Conditions  []Condition `json:"conditions"`
	Steps       []Step      `json:"steps"`
	CooldownMin int         `json:"cooldownMin"`
	Created     time.Time   `json:"created"`
	LastRun     *RunSummary `json:"lastRun"`
	HookHash    string      `json:"hookHash,omitempty"` // solo en disco
}

// RuleView es lo que ve el panel: sin el hash del enlace secreto.
type RuleView struct {
	Rule
	HasHook bool   `json:"hasHook"`
	Armed   string `json:"armed"` // waiting | active | done (estado del disparador)
}

type RunStep struct {
	Label  string `json:"label"`
	Result string `json:"result"` // ok | error | running | pending
	Detail string `json:"detail"`
	Ms     int64  `json:"ms"`
}

type Run struct {
	ID     string    `json:"id"`
	RuleID string    `json:"ruleId"`
	Name   string    `json:"name"`
	At     time.Time `json:"at"`
	Cause  string    `json:"cause"`
	Result string    `json:"result"` // running | ok | error | skipped
	Detail string    `json:"detail"`
	Steps  []RunStep `json:"steps"`
}

type ruleLive struct {
	seen     bool
	active   bool
	since    time.Time
	fired    bool
	lastFire time.Time
	running  bool
	lastHook time.Time
}

type Engine struct {
	a    *App
	mu   sync.Mutex
	rule []Rule
	runs []Run
	live map[string]*ruleLive
	rf   jsonFile[[]Rule]
	runf jsonFile[[]Run]
	sem  chan struct{}
}

func newEngine(a *App, dataDir string) *Engine {
	e := &Engine{a: a, live: map[string]*ruleLive{}, sem: make(chan struct{}, 2)}
	e.rf.path = filepath.Join(dataDir, "rules.json")
	e.runf.path = filepath.Join(dataDir, "runs.json")
	_ = e.runf.Load(&e.runs)
	if err := e.rf.Load(&e.rule); err != nil || e.rule == nil {
		e.rule = []Rule{}
	}
	return e
}

/* ---------- Vista y edición ---------- */

func (e *Engine) Views() ([]RuleView, []Run) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]RuleView, 0, len(e.rule))
	for _, r := range e.rule {
		v := RuleView{Rule: r, HasHook: r.HookHash != "", Armed: "waiting"}
		v.HookHash = ""
		if l := e.live[r.ID]; l != nil && l.active {
			v.Armed = "active"
			if l.fired {
				v.Armed = "done"
			}
		}
		out = append(out, v)
	}
	runs := append([]Run{}, e.runs...)
	return out, runs
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Save crea o actualiza una regla. Si el disparador es un enlace y aún no
// tiene, lo genera y lo devuelve una sola vez.
func (e *Engine) Save(r Rule) (Rule, string, error) {
	if err := e.validate(&r); err != nil {
		return r, "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	token := ""
	idx := -1
	for i := range e.rule {
		if e.rule[i].ID == r.ID && r.ID != "" {
			idx = i
		}
	}
	if idx >= 0 {
		old := e.rule[idx]
		r.Created, r.LastRun, r.HookHash = old.Created, old.LastRun, old.HookHash
		if fmt.Sprint(old.Trigger) != fmt.Sprint(r.Trigger) {
			delete(e.live, r.ID) // disparador nuevo: vuelve a tomar su estado de partida
		}
	} else {
		r.ID, r.Created = newID(), time.Now()
	}
	if r.Trigger.Kind == "hook" && r.HookHash == "" {
		token = RandomSecret()
		r.HookHash = hashToken(token)
	}
	if r.Trigger.Kind != "hook" {
		r.HookHash = ""
	}
	if idx >= 0 {
		e.rule[idx] = r
	} else {
		e.rule = append(e.rule, r)
	}
	e.saveLocked()
	return r, token, nil
}


func (e *Engine) NewHook(id string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.rule {
		if e.rule[i].ID == id {
			if e.rule[i].Trigger.Kind != "hook" {
				return "", errors.New("Esta automatización no se lanza con un enlace.")
			}
			t := RandomSecret()
			e.rule[i].HookHash = hashToken(t)
			e.saveLocked()
			return t, nil
		}
	}
	return "", errors.New("Automatización desconocida.")
}

func (e *Engine) SetEnabled(id string, on bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.rule {
		if e.rule[i].ID == id {
			e.rule[i].Enabled = on
			delete(e.live, id)
			e.saveLocked()
			return nil
		}
	}
	return errors.New("Automatización desconocida.")
}

func (e *Engine) Delete(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.rule {
		if e.rule[i].ID == id {
			e.rule = append(e.rule[:i], e.rule[i+1:]...)
			delete(e.live, id)
			e.saveLocked()
			return nil
		}
	}
	return errors.New("Automatización desconocida.")
}

func (e *Engine) saveLocked() { _ = e.rf.Save(append([]Rule{}, e.rule...)) }

/* ---------- Validación ---------- */

var (
	reHHMM  = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	reCName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
)

func validDays(d []int) bool {
	if len(d) == 0 {
		return false
	}
	for _, x := range d {
		if x < 0 || x > 6 {
			return false
		}
	}
	return true
}

func (e *Engine) validate(r *Rule) error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" || len([]rune(r.Name)) > 60 {
		return errors.New("Ponle un nombre de 1 a 60 caracteres.")
	}
	if r.CooldownMin < 0 || r.CooldownMin > 1440 {
		return errors.New("La pausa entre ejecuciones debe estar entre 0 y 1440 minutos.")
	}
	t := &r.Trigger
	if t.ForMin < 0 || t.ForMin > 1440 {
		return errors.New("La duración mínima debe estar entre 0 y 1440 minutos.")
	}
	switch t.Kind {
	case "schedule":
		if !reHHMM.MatchString(t.Time) || !validDays(t.Days) {
			return errors.New("Indica la hora (HH:MM) y al menos un día.")
		}
	case "container":
		if !reCName.MatchString(t.Container) {
			return errors.New("Elige un contenedor.")
		}
		if t.Event != "stopped" && t.Event != "unhealthy" && t.Event != "running" {
			return errors.New("Elige qué le tiene que pasar al contenedor.")
		}
	case "app":
		if t.ForMin < 1 {
			t.ForMin = 3
		}
	case "metric":
		lim := map[string][2]float64{"cpu": {1, 100}, "mem": {1, 100}, "disk": {1, 100}, "temp": {30, 110}}[t.Metric]
		if lim == [2]float64{} {
			return errors.New("Elige qué medir: procesador, memoria, disco o temperatura.")
		}
		if t.Above < lim[0] || t.Above > lim[1] {
			return fmt.Errorf("El umbral debe estar entre %g y %g.", lim[0], lim[1])
		}
	case "presence":
		if _, err := net.ParseMAC(t.MAC); err != nil {
			return errors.New("Elige un dispositivo de la red.")
		}
		t.MAC = strings.ToUpper(t.MAC)
		if t.Event != "arrive" && t.Event != "leave" {
			return errors.New("Elige si es al conectarse o al desconectarse.")
		}
		if t.Event == "leave" && t.ForMin < 10 {
			// Los móviles apagan el Wi‑Fi a ratos: sin margen, saltaría en falso.
			t.ForMin = 15
		}
	case "pc":
		if t.Event != "on" && t.Event != "off" {
			return errors.New("Elige si es al encenderse o al apagarse el PC.")
		}
		if e.a.Cfg.Get().PC.IP == "" {
			return errors.New("Primero configura el PC en Ajustes › Dispositivos registrados.")
		}
	case "hook":
	default:
		return errors.New("Elige cuándo se ejecuta.")
	}
	if len(r.Conditions) > 6 {
		return errors.New("Como mucho 6 condiciones.")
	}
	for i := range r.Conditions {
		c := &r.Conditions[i]
		switch c.Kind {
		case "time":
			if !reHHMM.MatchString(c.From) || !reHHMM.MatchString(c.To) || c.From == c.To {
				return errors.New("La franja horaria necesita dos horas distintas (HH:MM).")
			}
		case "days":
			if !validDays(c.Days) {
				return errors.New("Elige al menos un día en la condición.")
			}
		case "device":
			if _, err := net.ParseMAC(c.MAC); err != nil {
				return errors.New("Elige el dispositivo de la condición.")
			}
			c.MAC = strings.ToUpper(c.MAC)
		case "pc":
			if c.PC != "on" && c.PC != "off" {
				return errors.New("Elige si el PC debe estar encendido o apagado.")
			}
		case "container":
			if !reCName.MatchString(c.Container) {
				return errors.New("Elige el contenedor de la condición.")
			}
		default:
			return errors.New("Condición desconocida.")
		}
	}
	if len(r.Steps) == 0 || len(r.Steps) > 10 {
		return errors.New("Añade entre 1 y 10 acciones.")
	}
	cfg := e.a.Cfg.Get()
	for i := range r.Steps {
		s := &r.Steps[i]
		switch s.Kind {
		case "wake":
			if cfg.PC.MAC == "" {
				return errors.New("Para encender el PC, configúralo antes en Ajustes › Dispositivos registrados.")
			}
		case "container":
			if !reCName.MatchString(s.Container) {
				return errors.New("Elige el contenedor de la acción.")
			}
			if s.Op != "start" && s.Op != "stop" && s.Op != "restart" {
				return errors.New("Elige si iniciar, detener o reiniciar el contenedor.")
			}
		case "backup", "scan", "updates":
		case "wait":
			if s.Minutes < 1 || s.Minutes > 120 {
				return errors.New("La espera debe ser de 1 a 120 minutos.")
			}
		case "http":
			if s.Method != "GET" && s.Method != "POST" {
				s.Method = "POST"
			}
			if err := checkLocalURL(s.URL); err != nil {
				return err
			}
		default:
			return errors.New("Acción desconocida.")
		}
	}
	return nil
}

// checkLocalURL solo admite direcciones de la red de casa o de Tailscale:
// una automatización no puede enviar datos a Internet.
func checkLocalURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("La dirección debe empezar por http:// o https://.")
	}
	host := u.Hostname()
	if strings.HasSuffix(host, ".ts.net") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".lan") || !strings.Contains(host, ".") {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return errors.New("No se encuentra " + host + ".")
	}
	for _, ip := range ips {
		if !(ip.IsPrivate() || ip.IsLoopback() || cgnat.Contains(ip)) {
			return errors.New("Solo se puede llamar a equipos de tu red local o de Tailscale.")
		}
	}
	return nil
}

var _, cgnat, _ = net.ParseCIDR("100.64.0.0/10")

/* ---------- Evaluación ---------- */

// Evaluate se llama cada 15 s desde el bucle principal.
func (e *Engine) Evaluate() {
	s := e.a.Snapshot()
	busy := e.a.busyTargets()
	now := time.Now()
	e.mu.Lock()
	var fire []Rule
	var causes []string
	for _, r := range e.rule {
		if !r.Enabled || r.Trigger.Kind == "hook" {
			continue
		}
		l := e.live[r.ID]
		if l == nil {
			l = &ruleLive{}
			e.live[r.ID] = l
		}
		if r.Trigger.Kind == "schedule" {
			if r.Trigger.Time == now.Format("15:04") && hasDay(r.Trigger.Days, now) && now.Sub(l.lastFire) > 90*time.Second {
				l.lastFire = now
				fire, causes = append(fire, r), append(causes, "Hora programada ("+r.Trigger.Time+")")
			}
			continue
		}
		state, cause := triggerState(r.Trigger, s, busy)
		if !l.seen {
			// Lo que ya pasaba al arrancar no cuenta como novedad.
			l.seen, l.active, l.fired = true, state == 1, state == 1
			if state == 1 {
				l.since = now
			}
			continue
		}
		switch state {
		case 1:
			if !l.active {
				l.active, l.since = true, now
			}
			if !l.fired && now.Sub(l.since) >= time.Duration(r.Trigger.ForMin)*time.Minute {
				l.fired = true
				fire, causes = append(fire, r), append(causes, cause)
			}
		case 0:
			l.active, l.fired = false, false
		}
		// -1: zona de margen; se mantiene como estaba.
	}
	e.mu.Unlock()
	for i, r := range fire {
		e.fire(r, causes[i], false)
	}
}

func hasDay(days []int, t time.Time) bool {
	for _, d := range days {
		if d == int(t.Weekday()) {
			return true
		}
	}
	return false
}

// triggerState devuelve 1 si el disparador está activo, 0 si no, y -1 si está
// en la zona de margen de un umbral (no cambia nada).
func triggerState(t Trigger, s Snapshot, busy map[string]bool) (int, string) {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	switch t.Kind {
	case "container":
		if !s.Docker.Available || busy[t.Container] {
			return -1, ""
		}
		for _, c := range s.Docker.Containers {
			if c.Name != t.Container {
				continue
			}
			switch t.Event {
			case "stopped":
				return b(c.State != "running" && c.State != "restarting"), t.Container + " se detuvo (" + c.Status + ")"
			case "unhealthy":
				return b(c.Health == "unhealthy" || c.State == "restarting" || c.State == "dead"), t.Container + " tiene un problema (" + c.Status + ")"
			case "running":
				return b(c.State == "running"), t.Container + " se puso en marcha"
			}
		}
		return b(t.Event == "stopped"), t.Container + " ya no existe"
	case "app":
		n := s.Nextcloud
		if !n.Configured || busy[n.Container] {
			return -1, ""
		}
		ok := n.Reachable && n.HTTPStatus != nil && *n.HTTPStatus == 200 && (n.Maintenance == nil || !*n.Maintenance)
		why := "no responde"
		if n.Maintenance != nil && *n.Maintenance {
			why = "está en modo de mantenimiento"
		} else if n.HTTPStatus != nil {
			why = fmt.Sprintf("responde con HTTP %d", *n.HTTPStatus)
		}
		return b(!ok), "Nextcloud " + why + " desde hace " + fmt.Sprint(t.ForMin) + " min"
	case "metric":
		if s.Metrics == nil {
			return -1, ""
		}
		m := s.Metrics
		var v float64
		unit, what, margin := " %", "", 5.0
		switch t.Metric {
		case "cpu":
			v, what = m.CPUPct, "El procesador"
		case "mem":
			v, what = float64(m.Mem.UsedMB)*100/float64(max(1, m.Mem.TotalMB)), "La memoria"
		case "disk":
			what = "El disco"
			for _, d := range m.Disks {
				if d.TotalGB > 0 {
					v = max(v, d.UsedGB*100/d.TotalGB)
				}
			}
		case "temp":
			if m.TempC == nil {
				return -1, ""
			}
			v, unit, what = *m.TempC, " °C", "La temperatura"
		}
		cause := fmt.Sprintf("%s está en %.0f%s (umbral %g%s)", what, v, unit, t.Above, unit)
		switch {
		case v > t.Above:
			return 1, cause
		case v <= t.Above-margin:
			return 0, cause
		}
		return -1, cause
	case "presence":
		if s.Network.LastScan == nil {
			return -1, ""
		}
		name := t.MAC
		for _, d := range s.Network.Devices {
			if strings.EqualFold(d.MAC, t.MAC) {
				if d.Name != "" {
					name = d.Name
				}
				if t.Event == "arrive" {
					return b(d.Online), name + " se conectó a la red"
				}
				return b(!d.Online), name + " se desconectó de la red"
			}
		}
		return b(t.Event == "leave"), name + " no está en la red"
	case "pc":
		switch s.PC.State {
		case "on":
			return b(t.Event == "on"), "El PC se encendió"
		case "off":
			return b(t.Event == "off"), "El PC se apagó"
		}
		return -1, ""
	}
	return -1, ""
}

func (e *Engine) conditionsOK(r Rule) (bool, string) {
	s := e.a.Snapshot()
	now := time.Now()
	for _, c := range r.Conditions {
		switch c.Kind {
		case "time":
			hm := now.Format("15:04")
			in := hm >= c.From && hm < c.To
			if c.From > c.To { // franja que cruza la medianoche
				in = hm >= c.From || hm < c.To
			}
			if !in {
				return false, "fuera de la franja " + c.From + "–" + c.To
			}
		case "days":
			if !hasDay(c.Days, now) {
				return false, "hoy no es uno de los días elegidos"
			}
		case "device":
			online, name := false, c.MAC
			for _, d := range s.Network.Devices {
				if strings.EqualFold(d.MAC, c.MAC) {
					online = d.Online
					if d.Name != "" {
						name = d.Name
					}
				}
			}
			if online != c.Present {
				if c.Present {
					return false, name + " no está en casa"
				}
				return false, name + " está en casa"
			}
		case "pc":
			if s.PC.State != c.PC {
				return false, "el PC no está " + map[string]string{"on": "encendido", "off": "apagado"}[c.PC]
			}
		case "container":
			running := false
			for _, k := range s.Docker.Containers {
				if k.Name == c.Container {
					running = k.State == "running"
				}
			}
			if running != c.Running {
				return false, c.Container + map[bool]string{true: " no está en marcha", false: " está en marcha"}[c.Running]
			}
		}
	}
	return true, ""
}

/* ---------- Ejecución ---------- */

// Hook busca la regla del enlace secreto y la lanza.
func (e *Engine) Hook(token string) (string, error) {
	h := hashToken(token)
	e.mu.Lock()
	var r *Rule
	for i := range e.rule {
		if e.rule[i].HookHash != "" && e.rule[i].HookHash == h {
			c := e.rule[i]
			r = &c
		}
	}
	if r == nil {
		e.mu.Unlock()
		return "", errors.New("Enlace no válido.")
	}
	l := e.live[r.ID]
	if l == nil {
		l = &ruleLive{}
		e.live[r.ID] = l
	}
	if time.Since(l.lastHook) < 10*time.Second {
		e.mu.Unlock()
		return r.Name, errors.New("Espera unos segundos antes de volver a lanzarla.")
	}
	l.lastHook = time.Now()
	e.mu.Unlock()
	if !r.Enabled {
		return r.Name, errors.New("La automatización está desactivada.")
	}
	return r.Name, e.fire(*r, "Enlace (Atajo o NFC)", false)
}

// RunNow la ejecuta a mano: sin disparador, sin condiciones y sin pausa.
func (e *Engine) RunNow(id string) error {
	e.mu.Lock()
	var r *Rule
	for i := range e.rule {
		if e.rule[i].ID == id {
			c := e.rule[i]
			r = &c
		}
	}
	e.mu.Unlock()
	if r == nil {
		return errors.New("Automatización desconocida.")
	}
	return e.fire(*r, "Ejecutada a mano", true)
}

func (e *Engine) fire(r Rule, cause string, manual bool) error {
	e.mu.Lock()
	l := e.live[r.ID]
	if l == nil {
		l = &ruleLive{}
		e.live[r.ID] = l
	}
	if l.running {
		e.mu.Unlock()
		return errors.New("Ya se está ejecutando.")
	}
	if !manual && r.CooldownMin > 0 && time.Since(l.lastFire) < time.Duration(r.CooldownMin)*time.Minute && r.Trigger.Kind != "schedule" {
		e.mu.Unlock()
		return errors.New("Está en pausa tras la última ejecución.")
	}
	l.running, l.lastFire = true, time.Now()
	e.mu.Unlock()

	run := Run{ID: newID(), RuleID: r.ID, Name: r.Name, At: time.Now(), Cause: cause, Result: "running"}
	if !manual {
		if ok, why := e.conditionsOK(r); !ok {
			run.Result, run.Detail = "skipped", "No se ejecutó: "+why+"."
			e.finish(r.ID, run)
			return nil
		}
	}
	for _, s := range r.Steps {
		run.Steps = append(run.Steps, RunStep{Label: stepLabel(s), Result: "pending"})
	}
	e.record(run)
	go func() {
		e.sem <- struct{}{}
		defer func() { <-e.sem }()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
		defer cancel()
		run.Result = "ok"
		for i, s := range r.Steps {
			run.Steps[i].Result = "running"
			e.record(run)
			t0 := time.Now()
			detail, err := e.step(ctx, r, s)
			run.Steps[i].Ms = time.Since(t0).Milliseconds()
			run.Steps[i].Detail = detail
			if err != nil {
				run.Steps[i].Result, run.Steps[i].Detail = "error", err.Error()
				run.Result, run.Detail = "error", "Se detuvo en «"+run.Steps[i].Label+"»: "+err.Error()
				break
			}
			run.Steps[i].Result = "ok"
		}
		if run.Result == "ok" {
			run.Detail = "Todas las acciones se completaron."
		}
		e.a.Log("automatización", r.Name, "Automatización", map[bool]error{true: nil, false: errors.New(run.Detail)}[run.Result == "ok"], cause)
		e.finish(r.ID, run)
		e.a.Refresh("docker")
	}()
	return nil
}

func (e *Engine) record(run Run) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.runs {
		if e.runs[i].ID == run.ID {
			e.runs[i] = run
			return
		}
	}
	e.runs = append([]Run{run}, e.runs...)
	if len(e.runs) > 100 {
		e.runs = e.runs[:100]
	}
}

func (e *Engine) finish(ruleID string, run Run) {
	e.record(run)
	e.mu.Lock()
	if l := e.live[ruleID]; l != nil {
		l.running = false
	}
	for i := range e.rule {
		if e.rule[i].ID == ruleID {
			e.rule[i].LastRun = &RunSummary{At: run.At, Result: run.Result, Detail: run.Detail}
		}
	}
	e.saveLocked()
	runs := append([]Run{}, e.runs...)
	e.mu.Unlock()
	_ = e.runf.Save(runs)
}

func stepLabel(s Step) string {
	switch s.Kind {
	case "wake":
		if s.WaitOn {
			return "Encender el PC y esperar a que responda"
		}
		return "Encender el PC"
	case "container":
		return map[string]string{"start": "Iniciar ", "stop": "Detener ", "restart": "Reiniciar "}[s.Op] + s.Container
	case "backup":
		return "Copia de seguridad de la base de datos de Nextcloud"
	case "scan":
		return "Escanear la red"
	case "wait":
		return fmt.Sprintf("Esperar %d min", s.Minutes)
	case "http":
		return "Llamar a " + s.URL
	case "updates":
		return "Buscar actualizaciones de imágenes"
	}
	return s.Kind
}

func (e *Engine) step(ctx context.Context, r Rule, s Step) (string, error) {
	a := e.a
	switch s.Kind {
	case "wake":
		if err := a.Wake("automatización", " ("+r.Name+")"); err != nil {
			return "", err
		}
		if !s.WaitOn {
			return "Paquete de encendido enviado.", nil
		}
		pc := a.Cfg.Get().PC
		t0 := time.Now()
		for time.Since(t0) < 3*time.Minute {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(10 * time.Second):
			}
			if Reachable(pc.IP, pc.CheckPort, 1500*time.Millisecond) {
				a.Refresh("pc")
				return "El PC respondió a los " + time.Since(t0).Round(time.Second).String() + ".", nil
			}
		}
		return "", errors.New("el PC no respondió en 3 minutos")
	case "container":
		if a.busy("", s.Container) {
			return "", errors.New("hay otra acción en curso sobre " + s.Container)
		}
		state := a.containerState(s.Container)
		if state == "" {
			return "", errors.New("no existe el contenedor " + s.Container)
		}
		if s.Op == "start" && state == "running" {
			return "Ya estaba en marcha.", nil
		}
		if s.Op == "stop" && state != "running" {
			return "Ya estaba detenido.", nil
		}
		err := a.Docker.ContainerAction(ctx, s.Container, s.Op)
		a.Log("automatización", s.Container, map[string]string{"start": "Inicio", "stop": "Detención", "restart": "Reinicio"}[s.Op], err, r.Name)
		if err != nil {
			return "", err
		}
		return "Hecho.", nil
	case "backup":
		ctr := a.nextcloudName()
		raw, err := a.Docker.Inspect(ctx, ctr)
		if err != nil {
			return "", errors.New("no se encuentra el contenedor de Nextcloud")
		}
		db, kind := a.findDatabase(raw)
		if db == "" {
			return "", errors.New("no se encuentra la base de datos de Nextcloud en marcha")
		}
		path, err := a.backupDatabase(ctx, db, kind)
		a.Log("automatización", db, "Copia de seguridad", err, path)
		if err != nil {
			return "", err
		}
		return "Guardada en " + path + ".", nil
	case "scan":
		if err := a.Net.Scan(a.Cfg.Get()); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d dispositivos detectados.", len(a.Net.State().Devices)), nil
	case "wait":
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(s.Minutes) * time.Minute):
		}
		return "", nil
	case "http":
		if err := checkLocalURL(s.URL); err != nil {
			return "", err
		}
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(c, s.Method, s.URL, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("no responde: %v", err)
		}
		res.Body.Close()
		if res.StatusCode >= 400 {
			return "", fmt.Errorf("respondió %s", res.Status)
		}
		return "Respondió " + res.Status + ".", nil
	case "updates":
		a.CheckUpdates()
		a.mu.RLock()
		n := 0
		for _, u := range a.updates.Items {
			if u.State == "available" {
				n++
			}
		}
		a.mu.RUnlock()
		return fmt.Sprintf("%d actualizaciones disponibles.", n), nil
	}
	return "", errors.New("acción desconocida")
}

func (a *App) nextcloudName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.nextcloud.Container != "" {
		return a.nextcloud.Container
	}
	return nextcloudContainer(a.Cfg.Get())
}

/* ---------- Migración desde la versión anterior ---------- */

// migrateOldAutomation convierte el encendido programado de la versión
// anterior (automations.json) en una regla, la primera vez.
func (e *Engine) migrateOld(dataDir string) {
	if len(e.rule) > 0 {
		return
	}
	var old []struct {
		ID       string
		Enabled  bool
		Schedule *struct {
			Days []int
			Time string
		}
	}
	f := jsonFile[[]struct {
		ID       string
		Enabled  bool
		Schedule *struct {
			Days []int
			Time string
		}
	}]{path: filepath.Join(dataDir, "automations.json")}
	_ = f.Load(&old)
	for _, o := range old {
		if o.ID == "workday-wake" && o.Schedule != nil && (o.Enabled || o.Schedule.Time != "07:30") {
			e.rule = append(e.rule, Rule{ID: newID(), Name: "Encender el PC los días laborables", Enabled: o.Enabled, Created: time.Now(),
				Trigger: Trigger{Kind: "schedule", Days: o.Schedule.Days, Time: o.Schedule.Time}, Conditions: []Condition{},
				Steps: []Step{{Kind: "wake", WaitOn: true}}})
			e.saveLocked()
		}
	}
}
