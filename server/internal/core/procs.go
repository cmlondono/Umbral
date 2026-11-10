package core

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

/* Qué usa el procesador: cada 15 s se leen los contadores de /proc/<pid>/stat
 * y se agrupan por programa y origen (contenedor, servicio o núcleo). Cuando
 * el uso supera el umbral se abre un pico, que guarda la media de cada
 * programa mientras dura. Sin órdenes externas: solo /proc. */

// ProcUse es un programa (o un grupo de procesos con el mismo nombre y origen).
type ProcUse struct {
	Name  string  `json:"name"`
	Group string  `json:"group"` // contenedor o servicio; vacío si no tiene
	Kind  string  `json:"kind"`  // container, service, user, kernel o system
	CPU   float64 `json:"cpu"`   // % del equipo entero (como la gráfica)
	MemMB int     `json:"memMB"`
	Count int     `json:"count"` // procesos que forman el grupo
}

// CPUSplit reparte el uso del procesador. IOWait no cuenta en la gráfica:
// es tiempo esperando al disco con el procesador libre.
type CPUSplit struct {
	User   float64 `json:"user"`
	System float64 `json:"system"`
	IOWait float64 `json:"iowait"`
	Steal  float64 `json:"steal"`
}

type CPUNow struct {
	At    time.Time `json:"at"`
	Total float64   `json:"total"`
	Split CPUSplit  `json:"split"`
	Procs []ProcUse `json:"procs"`
	Tasks int       `json:"tasks"` // procesos en total
}

type CPUPeak struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Open  bool      `json:"open"`
	Max   float64   `json:"max"`
	Avg   float64   `json:"avg"`
	Split CPUSplit  `json:"split"`
	Top   []ProcUse `json:"top"`
	N     int       `json:"n"`
}

type CPUView struct {
	Now       *CPUNow   `json:"now"`
	Peaks     []CPUPeak `json:"peaks"`
	Threshold int       `json:"threshold"`
	Error     string    `json:"error"`
}

/* ---------- Lectura ---------- */

type procStat struct {
	pid    int
	ppid   int
	comm   string
	ticks  uint64 // utime + stime
	child  uint64 // cutime + cstime: hijos ya terminados
	start  uint64
	rssPg  int64
	kernel bool
}

// parseProcStat lee /proc/<pid>/stat. El nombre va entre paréntesis y puede
// tener espacios, así que se corta por el último «)».
func parseProcStat(s string) (procStat, bool) {
	i, j := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if i < 0 || j < i {
		return procStat{}, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(s[:i]))
	if err != nil {
		return procStat{}, false
	}
	f := strings.Fields(s[j+1:])
	if len(f) < 22 {
		return procStat{}, false
	}
	n := func(k int) uint64 { v, _ := strconv.ParseUint(f[k], 10, 64); return v }
	rss, _ := strconv.ParseInt(f[21], 10, 64)
	return procStat{pid: pid, ppid: int(n(1)), comm: s[i+1 : j], ticks: n(11) + n(12), child: n(13) + n(14), start: n(19), rssPg: rss, kernel: n(6)&0x00200000 != 0}, true
}

// parseCgroup deduce el origen de un proceso a partir de /proc/<pid>/cgroup.
// Devuelve el tipo y el identificador: id de contenedor, nombre de servicio…
func parseCgroup(s string) (kind, id string) {
	for _, l := range strings.Split(s, "\n") {
		parts := strings.SplitN(l, ":", 3)
		if len(parts) < 3 {
			continue
		}
		p := parts[2]
		for _, seg := range strings.Split(p, "/") {
			// cgroup v2: docker-<id>.scope; cgroup v1: /docker/<id>
			if c := strings.TrimSuffix(strings.TrimPrefix(seg, "docker-"), ".scope"); c != seg && isHexID(c) {
				return "container", c
			}
			if isHexID(seg) && strings.Contains(p, "docker") {
				return "container", seg
			}
		}
		if strings.HasPrefix(p, "/user.slice") {
			return "user", ""
		}
		if k := strings.LastIndex(p, "/"); k >= 0 && strings.HasSuffix(p, ".service") {
			return "service", strings.TrimSuffix(p[k+1:], ".service")
		}
	}
	return "system", ""
}

func isHexID(s string) bool {
	if len(s) < 12 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type cpuStat [8]uint64 // user nice system idle iowait irq softirq steal

func parseCPUStat(stat string) (cpuStat, bool) {
	for _, l := range strings.Split(stat, "\n") {
		f := strings.Fields(l)
		if len(f) < 9 || f[0] != "cpu" {
			continue
		}
		var c cpuStat
		for i := range c {
			c[i], _ = strconv.ParseUint(f[i+1], 10, 64)
		}
		return c, true
	}
	return cpuStat{}, false
}

func (c cpuStat) sum() (t uint64) {
	for _, v := range c {
		t += v
	}
	return t
}

func splitOf(prev, cur cpuStat) (float64, CPUSplit) {
	var d cpuStat
	for i := range d {
		if cur[i] >= prev[i] {
			d[i] = cur[i] - prev[i]
		}
	}
	t := float64(d.sum())
	if t <= 0 {
		return 0, CPUSplit{}
	}
	p := func(v uint64) float64 { return round1(float64(v) * 100 / t) }
	s := CPUSplit{User: p(d[0] + d[1]), System: p(d[2] + d[5] + d[6]), IOWait: p(d[4]), Steal: p(d[7])}
	return round1(float64(t-float64(d[3])-float64(d[4])) * 100 / t), s
}

type prevProc struct {
	ticks, child uint64
	ppid         int
}

type procMeta struct {
	start uint64
	kind  string
	id    string
}

// procSampler guarda los contadores anteriores para calcular porcentajes.
type procSampler struct {
	prevCPU cpuStat
	prev    map[int]prevProc
	meta    map[int]procMeta
	has     bool
}

// sampleFrom calcula el uso desde la lectura anterior. names traduce el id
// de un contenedor a su nombre y cgroup lee el cgroup de un proceso.
//
// Un proceso que nace y muere entre dos lecturas (una tarea de cron, por
// ejemplo) no se llega a ver, pero su tiempo pasa al padre que lo espera
// (cutime + cstime). Ese tiempo, menos lo ya contado de los hijos que se
// vieron, se apunta al padre.
func (p *procSampler) sampleFrom(stat string, procs []procStat, cgroup func(pid int) string, names func(id string) string, pageKB int64) (CPUNow, bool) {
	cs, ok := parseCPUStat(stat)
	if !ok {
		return CPUNow{}, false
	}
	if p.meta == nil {
		p.meta = map[int]procMeta{}
	}
	dt := float64(cs.sum() - p.prevCPU.sum())
	now := CPUNow{At: time.Now(), Procs: []ProcUse{}, Tasks: len(procs)}
	now.Total, now.Split = splitOf(p.prevCPU, cs)
	type key struct{ name, group, kind string }
	groups := map[key]*ProcUse{}
	cur := make(map[int]prevProc, len(procs))
	for _, ps := range procs {
		cur[ps.pid] = prevProc{ps.ticks, ps.child, ps.ppid}
	}
	// Lo ya contado de los hijos que han terminado, por padre.
	gone := map[int]uint64{}
	for pid, pp := range p.prev {
		if _, ok := cur[pid]; !ok {
			gone[pp.ppid] += pp.ticks + pp.child
		}
	}
	for _, ps := range procs {
		m, ok := p.meta[ps.pid]
		if !ok || m.start != ps.start {
			m = procMeta{start: ps.start}
			if ps.kernel {
				m.kind = "kernel"
			} else {
				m.kind, m.id = parseCgroup(cgroup(ps.pid))
			}
			p.meta[ps.pid] = m
		}
		var d uint64
		if prev, ok := p.prev[ps.pid]; ok && ps.ticks >= prev.ticks {
			d = ps.ticks - prev.ticks
			if ps.child > prev.child+gone[ps.pid] {
				d += ps.child - prev.child - gone[ps.pid]
			}
		} else if p.has {
			d = ps.ticks + ps.child // nació después de la lectura anterior
		}
		group := m.id
		if m.kind == "container" {
			group = names(m.id)
		}
		name := ps.comm
		if m.kind == "kernel" {
			name = kernelName(name)
		}
		k := key{name, group, m.kind}
		g := groups[k]
		if g == nil {
			g = &ProcUse{Name: name, Group: group, Kind: m.kind}
			groups[k] = g
		}
		g.Count++
		g.MemMB += int(ps.rssPg * pageKB / 1024)
		if dt > 0 {
			g.CPU += float64(d) * 100 / dt
		}
	}
	for pid := range p.meta {
		if _, ok := cur[pid]; !ok {
			delete(p.meta, pid)
		}
	}
	had := p.has
	p.prevCPU, p.prev, p.has = cs, cur, true
	if !had {
		return now, false
	}
	for _, g := range groups {
		g.CPU = round1(g.CPU)
		now.Procs = append(now.Procs, *g)
	}
	sortProcs(now.Procs)
	return now, true
}

// kernelName junta los hilos del núcleo que solo cambian en el número
// (kworker/3:1, ksoftirqd/2…) para que se vea cuál pesa.
func kernelName(n string) string {
	if i := strings.IndexByte(n, '/'); i > 0 {
		return n[:i]
	}
	return n
}

func sortProcs(ps []ProcUse) {
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].CPU != ps[j].CPU {
			return ps[i].CPU > ps[j].CPU
		}
		return ps[i].MemMB > ps[j].MemMB
	})
}

/* ---------- Picos ---------- */

type peakAcc struct {
	cpu   float64
	mem   int
	count int
	p     ProcUse
}

type peakData struct {
	Threshold int       `json:"threshold"`
	Peaks     []CPUPeak `json:"peaks"`
}

type CPUWatch struct {
	mu    sync.Mutex
	d     peakData
	f     jsonFile[peakData]
	now   *CPUNow
	err   string
	acc   map[string]*peakAcc
	split CPUSplit
	sum   float64
	below int
	last  time.Time
}

const (
	maxPeaks     = 100
	keepPeaks    = 30 * 24 * time.Hour
	peakTop      = 6
	defaultPeak  = 70
	peakNowProcs = 40
)

var peakThresholds = []int{50, 70, 90}

func newCPUWatch(dataDir string) *CPUWatch {
	w := &CPUWatch{}
	w.f.path = filepath.Join(dataDir, "cpupeaks.json")
	_ = w.f.Load(&w.d)
	if !validThreshold(w.d.Threshold) {
		w.d.Threshold = defaultPeak
	}
	// Un pico abierto al apagar el servicio se da por terminado entonces.
	for i := range w.d.Peaks {
		w.d.Peaks[i].Open = false
	}
	return w
}

func validThreshold(t int) bool {
	for _, v := range peakThresholds {
		if v == t {
			return true
		}
	}
	return false
}

func (w *CPUWatch) SetThreshold(t int) bool {
	if !validThreshold(t) {
		return false
	}
	w.mu.Lock()
	w.d.Threshold = t
	w.mu.Unlock()
	w.Save()
	return true
}

func (w *CPUWatch) Fail(err string) {
	w.mu.Lock()
	w.err = err
	w.mu.Unlock()
}

// Record anota una lectura. Devuelve true si se acaba de cerrar un pico.
func (w *CPUWatch) Record(n CPUNow, step time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.err = ""
	top := n
	if len(top.Procs) > peakNowProcs {
		top.Procs = top.Procs[:peakNowProcs]
	}
	w.now = &top
	open := len(w.d.Peaks) > 0 && w.d.Peaks[0].Open
	high := n.Total >= float64(w.d.Threshold)
	closed := false
	switch {
	case high && !open:
		w.d.Peaks = append([]CPUPeak{{From: n.At.Add(-step), Open: true}}, w.d.Peaks...)
		w.acc, w.split, w.sum, w.below = map[string]*peakAcc{}, CPUSplit{}, 0, 0
		w.trim(n.At)
		fallthrough
	case high:
		w.add(n)
	case open:
		// Dos lecturas seguidas por debajo (30 s) cierran el pico: así un
		// bajón momentáneo no lo parte en dos.
		w.below++
		if w.below >= 2 || n.At.Sub(w.last) > 3*step {
			w.d.Peaks[0].Open = false
			closed = true
		}
	}
	return closed
}

func (w *CPUWatch) add(n CPUNow) {
	pk := &w.d.Peaks[0]
	if w.acc == nil { // pico que venía de antes de reiniciar el servicio
		w.acc = map[string]*peakAcc{}
	}
	w.below, w.last = 0, n.At
	pk.N++
	pk.To = n.At
	w.sum += n.Total
	if n.Total > pk.Max {
		pk.Max = n.Total
	}
	pk.Avg = round1(w.sum / float64(pk.N))
	w.split.User += n.Split.User
	w.split.System += n.Split.System
	w.split.IOWait += n.Split.IOWait
	w.split.Steal += n.Split.Steal
	k := float64(pk.N)
	pk.Split = CPUSplit{round1(w.split.User / k), round1(w.split.System / k), round1(w.split.IOWait / k), round1(w.split.Steal / k)}
	for _, p := range n.Procs {
		if p.CPU <= 0 {
			continue
		}
		id := p.Kind + "\x00" + p.Group + "\x00" + p.Name
		a := w.acc[id]
		if a == nil {
			a = &peakAcc{p: p}
			w.acc[id] = a
		}
		a.cpu += p.CPU
		a.mem = max(a.mem, p.MemMB)
		a.count = max(a.count, p.Count)
	}
	top := make([]ProcUse, 0, len(w.acc))
	for _, a := range w.acc {
		p := a.p
		p.CPU, p.MemMB, p.Count = round1(a.cpu/k), a.mem, a.count
		top = append(top, p)
	}
	sortProcs(top)
	if len(top) > peakTop {
		top = top[:peakTop]
	}
	pk.Top = top
}

// trim quita los picos de hace más de 30 días y deja como mucho 100.
func (w *CPUWatch) trim(now time.Time) {
	cut := now.Add(-keepPeaks)
	ps := w.d.Peaks[:0]
	for i, pk := range w.d.Peaks {
		if i == 0 || pk.To.After(cut) {
			ps = append(ps, pk)
		}
	}
	if len(ps) > maxPeaks {
		ps = ps[:maxPeaks]
	}
	w.d.Peaks = ps
}

func (w *CPUWatch) View() CPUView {
	w.mu.Lock()
	defer w.mu.Unlock()
	return CPUView{Now: w.now, Peaks: append([]CPUPeak{}, w.d.Peaks...), Threshold: w.d.Threshold, Error: w.err}
}

func (w *CPUWatch) Save() {
	w.mu.Lock()
	d := peakData{Threshold: w.d.Threshold, Peaks: append([]CPUPeak{}, w.d.Peaks...)}
	w.mu.Unlock()
	_ = w.f.Save(d)
}

// sampleProcs toma la lectura de procesos del ciclo de 15 s.
func (a *App) sampleProcs() {
	n, ok, err := a.procs.read(a.containerName)
	if err != nil {
		a.CPU.Fail(err.Error())
		return
	}
	if ok && a.CPU.Record(n, 15*time.Second) {
		a.CPU.Save()
	}
}

// containerName traduce el id completo de un contenedor a su nombre.
func (a *App) containerName(id string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, c := range a.docker.Containers {
		if c.ID != "" && (strings.HasPrefix(c.ID, id) || strings.HasPrefix(id, c.ID)) {
			return c.Name
		}
	}
	return id[:12]
}
