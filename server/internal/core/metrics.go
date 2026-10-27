package core

import (
	"path/filepath"
	"sync"
	"time"
)

type ServerInfo struct {
	Hostname  string     `json:"hostname"`
	OS        string     `json:"os"`
	Kernel    string     `json:"kernel"`
	CPUModel  string     `json:"cpuModel"`
	Cores     int        `json:"cores"`
	UptimeSec int64      `json:"uptimeSec"`
	Load      [3]float64 `json:"load"`
	IP        string     `json:"ip"`
}

type Disk struct {
	Mount   string  `json:"mount"`
	Label   string  `json:"label"`
	Device  string  `json:"device"`
	TotalGB float64 `json:"totalGB"`
	UsedGB  float64 `json:"usedGB"`
}

type MemInfo struct {
	TotalMB int `json:"totalMB"`
	UsedMB  int `json:"usedMB"`
	CacheMB int `json:"cacheMB"`
	FreeMB  int `json:"freeMB"`
}

type NetRate struct {
	RxMbps float64 `json:"rxMbps"`
	TxMbps float64 `json:"txMbps"`
	Iface  string  `json:"iface"`
}

type Metrics struct {
	CPUPct float64  `json:"cpuPct"`
	Mem    MemInfo  `json:"mem"`
	Disks  []Disk   `json:"disks"`
	Net    NetRate  `json:"net"`
	TempC  *float64 `json:"tempC"` // temperatura del procesador; nil si no hay sensor
	TempOf string   `json:"tempOf"`
}

// Sampler toma una muestra cada vez que se llama a Sample. Guarda el estado
// anterior de los contadores para calcular porcentajes y velocidades.
type Sampler struct {
	prevCPU   cpuTimes
	prevNet   netCounters
	prevAt    time.Time
	hasPrev   bool
	routeHint string
	light     bool // sin discos: para la vista en vivo
}

type cpuTimes struct{ idle, total uint64 }
type netCounters struct{ rx, tx uint64 }

/* ---------- Historial: 1 h a 1 min, 24 h a 15 min, 7 d a 2 h ---------- */

type series struct {
	Step  int64       `json:"step"` // segundos por punto
	Size  int         `json:"size"`
	Start []int64     `json:"start"` // inicio de cada cubo (unix)
	Vals  [][]float64 `json:"vals"`  // cpu, rx, tx
	acc   [nVals]float64
	n     int
	cur   int64
}

func newSeries(step int64, size int) *series { return &series{Step: step, Size: size} }

func (s *series) add(t time.Time, v [nVals]float64) {
	b := t.Unix() / s.Step * s.Step
	if s.n > 0 && b != s.cur {
		s.flush()
	}
	s.cur = b
	for i := range v {
		s.acc[i] += v[i]
	}
	s.n++
}

func (s *series) flush() {
	if s.n == 0 {
		return
	}
	row := make([]float64, nVals)
	for i := range row {
		row[i] = s.acc[i] / float64(s.n)
	}
	s.Start = append(s.Start, s.cur)
	s.Vals = append(s.Vals, row)
	if len(s.Start) > s.Size {
		s.Start, s.Vals = s.Start[len(s.Start)-s.Size:], s.Vals[len(s.Vals)-s.Size:]
	}
	s.acc, s.n = [nVals]float64{}, 0
}

type History struct {
	Range string    `json:"range"`
	StepS int64     `json:"stepSec"`
	Times []string  `json:"times"`
	CPU   []float64 `json:"cpu"`
	Rx    []float64 `json:"rx"`
	Tx    []float64 `json:"tx"`
	Mem   []float64 `json:"mem"`  // % de memoria en uso
	Temp  []float64 `json:"temp"` // °C; 0 si no hay sensor
}

type MetricsHistory struct {
	mu   sync.Mutex
	S    map[string]*series `json:"s"`
	file jsonFile[map[string]*series]
}

func NewMetricsHistory(dataDir string) *MetricsHistory {
	h := &MetricsHistory{S: map[string]*series{"1h": newSeries(60, 60), "24h": newSeries(900, 96), "7d": newSeries(7200, 84)}}
	h.file.path = filepath.Join(dataDir, "metrics.json")
	saved := map[string]*series{}
	if h.file.Load(&saved) == nil {
		for k, s := range saved {
			if cur, ok := h.S[k]; ok && s != nil && s.Step == cur.Step {
				s.Size = cur.Size
				h.S[k] = s
			}
		}
	}
	return h
}

func (h *MetricsHistory) Add(t time.Time, m Metrics) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.S {
		memPct, temp := 0.0, 0.0
		if m.Mem.TotalMB > 0 {
			memPct = float64(m.Mem.UsedMB) * 100 / float64(m.Mem.TotalMB)
		}
		if m.TempC != nil {
			temp = *m.TempC
		}
		s.add(t, [nVals]float64{m.CPUPct, m.Net.RxMbps, m.Net.TxMbps, memPct, temp})
	}
}

func (h *MetricsHistory) Save() {
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = h.file.Save(h.S)
}

func (h *MetricsHistory) Get(r string) (History, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.S[r]
	if !ok {
		return History{}, false
	}
	out := History{Range: r, StepS: s.Step, Times: []string{}, CPU: []float64{}, Rx: []float64{}, Tx: []float64{}, Mem: []float64{}, Temp: []float64{}}
	// Descarta los cubos más antiguos que el periodo pedido.
	min := time.Now().Unix() - s.Step*int64(s.Size)
	for i, st := range s.Start {
		if st < min {
			continue
		}
		out.Times = append(out.Times, time.Unix(st, 0).UTC().Format(time.RFC3339))
		out.CPU = append(out.CPU, round1(s.Vals[i][0]))
		out.Rx = append(out.Rx, round1(s.Vals[i][1]))
		out.Tx = append(out.Tx, round1(s.Vals[i][2]))
		out.Mem = append(out.Mem, round1(at(s.Vals[i], 3)))
		out.Temp = append(out.Temp, round1(at(s.Vals[i], 4)))
	}
	return out, true
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// Valores por punto del historial: CPU, bajada, subida, memoria y temperatura.
const nVals = 5

// at devuelve el valor i de una fila, o 0 si la fila es de una versión
// anterior con menos columnas.
func at(row []float64, i int) float64 {
	if i < len(row) {
		return row[i]
	}
	return 0
}
