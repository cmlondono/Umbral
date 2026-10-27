package core

import (
	"context"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// App reúne el estado del servidor. Cada fuente se refresca en su propio
// ritmo y la API sirve una copia: abrir el panel no lanza trabajo extra,
// salvo las lecturas de CPU de los contenedores, que solo se hacen mientras
// alguien está mirando.
type App struct {
	DataDir string
	Cfg     *ConfigStore
	Auth    *Auth
	Docker  *Docker
	Hist    *MetricsHistory
	Net     *Scanner
	Engine  *Engine

	mu        sync.RWMutex
	server    ServerInfo
	metrics   *Metrics
	metricErr *string
	docker    DockerState
	nextcloud NextcloudState
	pc        PCState
	tailscale TailscaleState
	updates   UpdatesState
	history   []HistoryItem
	jobs      []*Job
	internet  *bool
	started   time.Time

	lastViewer time.Time
	wakeAt     time.Time
	histFile   jsonFile[[]HistoryItem]
	updFile    jsonFile[UpdatesState]
	kick       chan string
	sampler    Sampler
	sampMu     sync.Mutex
	live       liveState
}

type PCState struct {
	Configured bool       `json:"configured"`
	Name       string     `json:"name"`
	IP         string     `json:"ip"`
	MAC        string     `json:"mac"`
	State      string     `json:"state"`
	LastSeen   *time.Time `json:"lastSeen"`
	LastWake   *time.Time `json:"lastWake"`
}

type HistoryItem struct {
	At     time.Time `json:"at"`
	Target string    `json:"target"`
	Action string    `json:"action"`
	Result string    `json:"result"`
	Detail string    `json:"detail"`
	User   string    `json:"user"`
}

func NewApp(dataDir string) (*App, error) {
	cfg, err := LoadConfig(dataDir)
	if err != nil {
		return nil, err
	}
	auth, err := NewAuth(dataDir)
	if err != nil {
		return nil, err
	}
	c := cfg.Get()
	a := &App{DataDir: dataDir, Cfg: cfg, Auth: auth, Docker: NewDocker(c.DockerSocket), Hist: NewMetricsHistory(dataDir),
		Net: NewScanner(filepath.Join(dataDir, "network.json"), dataDir), kick: make(chan string, 8)}
	a.histFile.path = filepath.Join(dataDir, "history.json")
	a.updFile.path = filepath.Join(dataDir, "updates.json")
	_ = a.histFile.Load(&a.history)
	_ = a.updFile.Load(&a.updates)
	a.updates.Checking = false
	if a.updates.Items == nil {
		a.updates.Items = []Update{}
	}
	a.Engine = newEngine(a, dataDir)
	a.Engine.migrateOld(dataDir)
	a.server = ReadServerInfo()
	a.started = time.Now()
	a.docker = DockerState{Containers: []Container{}, Images: []Image{}}
	a.tailscale = TailscaleState{Peers: []TailscalePeer{}}
	return a, nil
}

// Viewed se llama en cada petición autenticada de estado.
func (a *App) Viewed() {
	a.mu.Lock()
	a.lastViewer = time.Now()
	a.mu.Unlock()
}

func (a *App) watching() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return time.Since(a.lastViewer) < 2*time.Minute
}

// Refresh pide que una fuente se actualice ya (por ejemplo, tras una acción).
func (a *App) Refresh(what string) {
	select {
	case a.kick <- what:
	default:
	}
}

func (a *App) Run(ctx context.Context) {
	tick := func(d time.Duration) *time.Ticker { return time.NewTicker(d) }
	tMetrics, tDocker, tSlow, tAuto, tSave := tick(15*time.Second), tick(15*time.Second), tick(60*time.Second), tick(30*time.Second), tick(10*time.Minute)
	defer func() { tMetrics.Stop(); tDocker.Stop(); tSlow.Stop(); tAuto.Stop(); tSave.Stop() }()

	scanEvery := func() time.Duration { return time.Duration(a.Cfg.Get().ScanMinutes) * time.Minute }
	tScan := time.NewTimer(5 * time.Second)
	tUpd := time.NewTimer(2 * time.Minute)

	a.sampleNow()
	a.refreshDocker(ctx, true)
	go a.refreshSlow()

	for {
		select {
		case <-ctx.Done():
			a.Hist.Save()
			return
		case <-tMetrics.C:
			a.sampleNow()
			a.Engine.Evaluate()
		case <-tDocker.C:
			a.refreshDocker(ctx, a.watching())
		case <-tSlow.C:
			go a.refreshSlow()
		case <-tAuto.C:
			a.refreshPC()
		case <-tSave.C:
			a.Hist.Save()
		case <-tScan.C:
			go func() {
				if err := a.Net.Scan(a.Cfg.Get()); err != nil {
					log.Println("escaneo de red:", err)
				}
				a.refreshPC()
			}()
			tScan.Reset(scanEvery())
		case <-tUpd.C:
			go a.CheckUpdates()
			tUpd.Reset(24 * time.Hour)
		case k := <-a.kick:
			switch k {
			case "docker":
				a.refreshDocker(ctx, true)
			case "pc":
				a.refreshPC()
			case "scan":
				go func() { _ = a.Net.Scan(a.Cfg.Get()); a.refreshPC() }()
			case "slow":
				go a.refreshSlow()
			}
		}
	}
}

// sampleNow toma una lectura de métricas. La usan el temporizador y el botón
// Actualizar del panel.
func (a *App) sampleNow() {
	a.sampMu.Lock()
	defer a.sampMu.Unlock()
	a.sampleMetrics(&a.sampler)
}

/* ---------- Métricas en vivo ---------- */

// LivePoint es una lectura instantánea para la vista «En vivo» del panel.
type LivePoint struct {
	At   time.Time `json:"at"`
	CPU  float64   `json:"cpu"`
	Rx   float64   `json:"rx"`
	Tx   float64   `json:"tx"`
	Mem  float64   `json:"mem"`
	Temp *float64  `json:"temp"`
}

type liveState struct {
	mu   sync.Mutex
	s    Sampler
	last LivePoint
}

// Live solo lee mientras alguien mira la vista en vivo. Usa su propio
// muestreador ligero (sin discos) y no toca el historial. Si varias pestañas
// piden a la vez, comparten la misma lectura durante un segundo.
func (a *App) Live() (LivePoint, error) {
	l := &a.live
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.last.At) < time.Second {
		return l.last, nil
	}
	l.s.light = true
	// Una lectura antigua daría la media de todo el tiempo sin mirar.
	if !l.s.hasPrev || time.Since(l.s.prevAt) > 10*time.Second {
		if _, err := l.s.Sample(); err != nil {
			return LivePoint{}, err
		}
		time.Sleep(300 * time.Millisecond)
	}
	m, err := l.s.Sample()
	if err != nil {
		return LivePoint{}, err
	}
	p := LivePoint{At: time.Now(), CPU: m.CPUPct, Rx: m.Net.RxMbps, Tx: m.Net.TxMbps, Temp: m.TempC}
	if m.Mem.TotalMB > 0 {
		p.Mem = round1(float64(m.Mem.UsedMB) * 100 / float64(m.Mem.TotalMB))
	}
	l.last = p
	return p, nil
}

func (a *App) sampleMetrics(s *Sampler) {
	m, err := s.Sample()
	si := ReadServerInfo()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.server = si
	if err != nil {
		e := err.Error()
		a.metrics, a.metricErr = nil, &e
		return
	}
	a.metricErr = nil
	if s.hasPrev {
		a.metrics = &m
		a.Hist.Add(time.Now(), m)
	}
}

func (a *App) refreshDocker(ctx context.Context, withStats bool) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	ver, err := a.Docker.Version(ctx)
	if err != nil {
		e := err.Error()
		a.mu.Lock()
		a.docker = DockerState{Error: &e, Containers: []Container{}, Images: []Image{}}
		a.mu.Unlock()
		return
	}
	cs, err := a.Docker.Containers(ctx)
	if err != nil {
		e := err.Error()
		a.mu.Lock()
		a.docker.Error = &e
		a.mu.Unlock()
		return
	}
	imgs, _ := a.Docker.Images(ctx, cs)
	a.mu.RLock()
	prev := map[string]Container{}
	for _, c := range a.docker.Containers {
		prev[c.ID] = c
	}
	a.mu.RUnlock()
	var stats map[string][2]float64
	if withStats {
		stats = a.Docker.Stats(ctx, cs)
	}
	cfg := a.Cfg.Get()
	ip := a.serverIP()
	for i := range cs {
		c := &cs[i]
		if s, ok := stats[c.ID]; ok {
			cpu, mem := s[0], int(s[1]+0.5)
			c.CPUPct, c.MemMB = &cpu, &mem
		} else if p, ok := prev[c.ID]; ok && c.State == "running" {
			c.CPUPct, c.MemMB = p.CPUPct, p.MemMB
		}
		c.URL = containerURL(*c, cfg, ip)
	}
	if imgs == nil {
		imgs = []Image{}
	}
	a.mu.Lock()
	a.docker = DockerState{Available: true, Version: ver, Containers: cs, Images: imgs}
	a.mu.Unlock()
	a.clearSolvedUpdateErrors(cs)
}

// containerURL: una etiqueta umbral.url manda; si no, el primer puerto
// publicado por TCP en la IP del servidor.
func containerURL(c Container, cfg Config, ip string) *string {
	if u := c.labels["umbral.url"]; u != "" {
		return &u
	}
	if cfg.Nextcloud.URL != "" && c.Name == nextcloudContainer(cfg) {
		u := cfg.Nextcloud.URL
		return &u
	}
	for _, p := range c.Ports {
		host, _, ok := strings.Cut(p, "→")
		if ok && strings.HasSuffix(p, "/tcp") && ip != "" {
			u := "http://" + ip + ":" + host
			return &u
		}
	}
	return nil
}

func nextcloudContainer(cfg Config) string {
	if cfg.Nextcloud.Container != "" {
		return cfg.Nextcloud.Container
	}
	return "nextcloud"
}

func (a *App) serverIP() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.server.IP
}

func (a *App) refreshSlow() {
	ts := ReadTailscale()
	inet := internetUp()
	cfg := a.Cfg.Get()
	ncURL, ncName := cfg.Nextcloud.URL, nextcloudContainer(cfg)
	// Si no se ha indicado la dirección, se deduce del contenedor de Nextcloud.
	if ncURL == "" {
		a.mu.RLock()
		for _, c := range a.docker.Containers {
			if strings.HasPrefix(c.Image, "nextcloud") && c.URL != nil && (cfg.Nextcloud.Container == "" || c.Name == cfg.Nextcloud.Container) {
				ncURL, ncName = *c.URL, c.Name
				break
			}
		}
		a.mu.RUnlock()
	}
	nc := ReadNextcloud(ncURL, ncName)
	a.mu.Lock()
	a.tailscale, a.nextcloud, a.internet = ts, nc, &inet
	a.mu.Unlock()
}

func (a *App) refreshPC() {
	cfg := a.Cfg.Get().PC
	st := PCState{Configured: cfg.MAC != "" && cfg.IP != "", Name: cfg.Name, IP: cfg.IP, MAC: cfg.MAC, State: "unknown"}
	if st.Configured {
		up := Reachable(cfg.IP, cfg.CheckPort, 1200*time.Millisecond)
		seen, at := a.Net.Seen(cfg.MAC)
		a.mu.RLock()
		wakeAt, prev := a.wakeAt, a.pc
		a.mu.RUnlock()
		switch {
		case up:
			st.State = "on"
			now := time.Now()
			st.LastSeen = &now
		case !wakeAt.IsZero() && time.Since(wakeAt) < 3*time.Minute:
			st.State = "waking"
		default:
			st.State = "off"
		}
		if !up && seen {
			st.State = "on"
		}
		if st.LastSeen == nil {
			if !at.IsZero() {
				st.LastSeen = &at
			} else {
				st.LastSeen = prev.LastSeen
			}
		}
		if !wakeAt.IsZero() {
			w := wakeAt
			st.LastWake = &w
		}
	}
	a.mu.Lock()
	a.pc = st
	a.mu.Unlock()
}

func (a *App) Wake(user, origin string) error {
	cfg := a.Cfg.Get().PC
	if cfg.MAC == "" {
		return fmt.Errorf("Falta la dirección MAC del PC. Añádela en Ajustes › Dispositivos registrados.")
	}
	err := SendWOL(cfg.MAC, cfg.Broadcast)
	a.Log(user, cfg.Name, "Wake-on-LAN", err, "Paquete enviado a "+cfg.MAC+origin)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.wakeAt = time.Now()
	a.pc.State = "waking"
	a.mu.Unlock()
	return nil
}

/* ---------- Historial de acciones ---------- */

func (a *App) Log(user, target, action string, err error, detail string) {
	it := HistoryItem{At: time.Now(), Target: target, Action: action, Result: "ok", Detail: detail, User: user}
	if err != nil {
		it.Result, it.Detail = "error", err.Error()
	}
	a.mu.Lock()
	a.history = append([]HistoryItem{it}, a.history...)
	if len(a.history) > 300 {
		a.history = a.history[:300]
	}
	h := append([]HistoryItem(nil), a.history...)
	a.mu.Unlock()
	_ = a.histFile.Save(h)
	log.Printf("acción: %s %s %s (%s) %s", user, action, target, it.Result, it.Detail)
}

/* ---------- Instantánea para el panel ---------- */

type Snapshot struct {
	At          time.Time      `json:"at"`
	Server      ServerInfo     `json:"server"`
	Metrics     *Metrics       `json:"metrics"`
	MetricsErr  *string        `json:"metricsError"`
	Docker      DockerState    `json:"docker"`
	Nextcloud   NextcloudState `json:"nextcloud"`
	PC          PCState        `json:"pc"`
	Tailscale   TailscaleState `json:"tailscale"`
	Network     NetworkState   `json:"network"`
	Updates     UpdatesState   `json:"updates"`
	History     []HistoryItem  `json:"history"`
	Automations []RuleView     `json:"automations"`
	Runs        []Run          `json:"runs"`
	Config      PublicConfig   `json:"config"`
	Jobs        []JobView      `json:"jobs"`
	Internet    *bool          `json:"internet"`
	Panel       PanelInfo      `json:"panel"`
}

func (a *App) Snapshot() Snapshot {
	netst := a.Net.State()
	if netst.Devices == nil {
		netst.Devices = []NetDevice{}
	}
	rules, runs := a.Engine.Views()
	a.mu.RLock()
	defer a.mu.RUnlock()
	s := Snapshot{At: time.Now(), Server: a.server, Metrics: a.metrics, MetricsErr: a.metricErr, Docker: a.docker, Nextcloud: a.nextcloud,
		PC: a.pc, Tailscale: a.tailscale, Network: netst, Updates: a.updates, Config: a.Cfg.Public(), Jobs: []JobView{}, Automations: rules, Runs: runs, Internet: a.internet, Panel: panelInfo(a.started, a.DataDir)}
	s.History = append([]HistoryItem{}, a.history...)
	for _, j := range a.jobs {
		s.Jobs = append(s.Jobs, j.copy())
	}
	return s
}

// usedPorts devuelve los puertos del servidor que ya están ocupados.
func (a *App) usedPorts() map[int]string {
	u := map[int]string{22: "el acceso SSH del servidor", 8420: "este panel"}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, c := range a.docker.Containers {
		for _, p := range c.Ports {
			var n int
			if _, err := fmt.Sscanf(p, "%d→", &n); err == nil {
				u[n] = c.Name
			}
		}
	}
	if _, port, err := net.SplitHostPort(a.Cfg.Get().Listen); err == nil {
		var n int
		fmt.Sscanf(port, "%d", &n)
		u[n] = "este panel"
	}
	return u
}

// internetUp comprueba la salida a Internet abriendo una conexión TCP con
// dos servidores DNS públicos. No envía datos.
func internetUp() bool {
	for _, h := range []string{"1.1.1.1:443", "8.8.8.8:443"} {
		if c, err := net.DialTimeout("tcp", h, 3*time.Second); err == nil {
			c.Close()
			return true
		}
	}
	return false
}

type PanelInfo struct {
	Version   string `json:"version"`
	MemMB     int    `json:"memMB"`
	UptimeSec int64  `json:"uptimeSec"`
	DataDir   string `json:"dataDir"`
}

var Version = "0.1.0"

func panelInfo(started time.Time, dataDir string) PanelInfo {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return PanelInfo{Version: Version, MemMB: int(m.Sys >> 20), UptimeSec: int64(time.Since(started).Seconds()), DataDir: dataDir}
}
