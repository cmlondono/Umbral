package core

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

/* Sistema operativo: actualizaciones de Debian y salud de los discos.
 *
 * Umbral no es root. Lo que necesita root (leer SMART y refrescar la lista de
 * paquetes con apt-get update) lo hace «umbral sistema», lanzado por systemd
 * como root cada 6 h o cuando el panel deja una petición. No recibe datos del
 * panel: siempre hace lo mismo y escribe sus resultados en HelperDir, una
 * carpeta de root que Umbral solo lee. La simulación de actualizaciones
 * (apt-get -s) no necesita root y la hace el propio Umbral. */

const HelperDir = "/var/lib/umbral-helper"

/* ---------- Parte de root: umbral sistema ---------- */

type SmartDisk struct {
	Name          string `json:"name"`
	Model         string `json:"model"`
	Serial        string `json:"serial"`
	SizeGB        int    `json:"sizeGB"`
	Kind          string `json:"kind"` // nvme | ssd | hdd
	Supported     bool   `json:"supported"`
	Passed        *bool  `json:"passed"`
	TempC         *int   `json:"tempC"`
	Hours         *int   `json:"hours"`
	Reallocated   *int   `json:"reallocated"`
	Pending       *int   `json:"pending"`
	Uncorrectable *int   `json:"uncorrectable"`
	PercentUsed   *int   `json:"percentUsed"` // NVMe: desgaste
	MediaErrors   *int   `json:"mediaErrors"`
	Health        string `json:"health"` // ok | warn | fail | unknown
	Note          string `json:"note"`
}

type SmartReport struct {
	At        time.Time   `json:"at"`
	Available bool        `json:"available"` // smartctl instalado
	Disks     []SmartDisk `json:"disks"`
}

type AptUpdateReport struct {
	At    time.Time `json:"at"`
	OK    bool      `json:"ok"`
	Error string    `json:"error"`
}

// RunSystemHelper es lo que ejecuta systemd como root.
func RunSystemHelper(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o755)
	rep := SmartReport{At: time.Now(), Disks: []SmartDisk{}}
	if _, err := exec.LookPath("smartctl"); err == nil {
		rep.Available = true
		for _, d := range listDisks() {
			rep.Disks = append(rep.Disks, readSmart(d))
		}
	}
	err1 := writeRootJSON(dir, "smart.json", rep)
	var err2 error
	if _, err := exec.LookPath("apt-get"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		out, err := exec.CommandContext(ctx, "apt-get", "update", "-qq").CombinedOutput()
		cancel()
		r := AptUpdateReport{At: time.Now(), OK: err == nil}
		if err != nil {
			r.Error = lastLines(strings.TrimSpace(string(out)), 2)
			if r.Error == "" {
				r.Error = err.Error()
			}
		}
		err2 = writeRootJSON(dir, "apt-update.json", r)
	}
	return errors.Join(err1, err2)
}

// writeRootJSON escribe con un nombre temporal aleatorio y lo renombra, para
// que root nunca siga un enlace simbólico ajeno.
func writeRootJSON(dir, name string, v any) error {
	b, _ := json.MarshalIndent(v, "", " ")
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if err := errors.Join(werr, cerr, os.Chmod(f.Name(), 0o644)); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, name))
}

func listDisks() []string {
	out, err := exec.Command("lsblk", "-dn", "-o", "NAME,TYPE").Output()
	if err != nil {
		return nil
	}
	var ds []string
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		if len(f) == 2 && f[1] == "disk" && !strings.HasPrefix(f[0], "zram") && !strings.HasPrefix(f[0], "loop") {
			ds = append(ds, f[0])
		}
	}
	return ds
}

func readSmart(name string) SmartDisk {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	// smartctl devuelve un código distinto de 0 también cuando solo avisa: se
	// interpreta su JSON en cualquier caso.
	out, _ := exec.CommandContext(ctx, "smartctl", "--json=c", "-a", "/dev/"+name).Output()
	return parseSmart(name, out)
}

func parseSmart(name string, out []byte) SmartDisk {
	d := SmartDisk{Name: name, Health: "unknown"}
	var j struct {
		Device       struct{ Type, Protocol string }
		ModelName    string `json:"model_name"`
		SerialNumber string `json:"serial_number"`
		UserCapacity struct {
			Bytes int64 `json:"bytes"`
		} `json:"user_capacity"`
		RotationRate *int `json:"rotation_rate"`
		SmartSupport *struct {
			Available bool `json:"available"`
			Enabled   bool `json:"enabled"`
		} `json:"smart_support"`
		SmartStatus *struct {
			Passed bool `json:"passed"`
		} `json:"smart_status"`
		Temperature *struct {
			Current int `json:"current"`
		} `json:"temperature"`
		PowerOnTime *struct {
			Hours int `json:"hours"`
		} `json:"power_on_time"`
		ATA *struct {
			Table []struct {
				ID  int `json:"id"`
				Raw struct {
					Value int `json:"value"`
				} `json:"raw"`
			} `json:"table"`
		} `json:"ata_smart_attributes"`
		NVMe *struct {
			PercentageUsed  int `json:"percentage_used"`
			MediaErrors     int `json:"media_errors"`
			CriticalWarning int `json:"critical_warning"`
		} `json:"nvme_smart_health_information_log"`
		Smartctl struct {
			Messages []struct{ String, Severity string } `json:"messages"`
		} `json:"smartctl"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		d.Note = "smartctl no devolvió datos de este disco."
		return d
	}
	d.Model, d.Serial = j.ModelName, j.SerialNumber
	d.SizeGB = int(j.UserCapacity.Bytes / 1e9)
	switch {
	case strings.EqualFold(j.Device.Protocol, "NVMe") || j.Device.Type == "nvme":
		d.Kind = "nvme"
	case j.RotationRate != nil && *j.RotationRate == 0:
		d.Kind = "ssd"
	case j.RotationRate != nil:
		d.Kind = "hdd"
	}
	d.Supported = j.SmartStatus != nil || j.NVMe != nil
	if j.SmartSupport != nil && !j.SmartSupport.Available {
		d.Supported = false
	}
	if !d.Supported {
		d.Note = "El disco no ofrece datos de salud (es habitual en memorias y adaptadores USB)."
		for _, m := range j.Smartctl.Messages {
			if m.Severity == "error" {
				d.Note += " " + m.String
				break
			}
		}
		return d
	}
	ip := func(v int) *int { return &v }
	if j.SmartStatus != nil {
		p := j.SmartStatus.Passed
		d.Passed = &p
	}
	if j.Temperature != nil && j.Temperature.Current > 0 {
		d.TempC = ip(j.Temperature.Current)
	}
	if j.PowerOnTime != nil {
		d.Hours = ip(j.PowerOnTime.Hours)
	}
	if j.ATA != nil {
		for _, a := range j.ATA.Table {
			switch a.ID {
			case 5:
				d.Reallocated = ip(a.Raw.Value)
			case 197:
				d.Pending = ip(a.Raw.Value)
			case 198:
				d.Uncorrectable = ip(a.Raw.Value)
			}
		}
	}
	if j.NVMe != nil {
		d.PercentUsed, d.MediaErrors = ip(j.NVMe.PercentageUsed), ip(j.NVMe.MediaErrors)
		if j.NVMe.CriticalWarning != 0 {
			d.Note = "El disco informa de un aviso crítico."
		}
	}
	d.Health = smartHealth(d, j.NVMe != nil && j.NVMe.CriticalWarning != 0)
	return d
}

func smartHealth(d SmartDisk, critical bool) string {
	gt0 := func(p *int) bool { return p != nil && *p > 0 }
	switch {
	case d.Passed != nil && !*d.Passed, critical:
		return "fail"
	case gt0(d.Reallocated), gt0(d.Pending), gt0(d.Uncorrectable), gt0(d.MediaErrors), d.PercentUsed != nil && *d.PercentUsed >= 90:
		return "warn"
	case d.Passed != nil:
		return "ok"
	}
	return "unknown"
}

/* ---------- Parte de Umbral ---------- */

type AptPkg struct {
	Name     string `json:"name"`
	From     string `json:"from"`
	To       string `json:"to"`
	Security bool   `json:"security"`
}

type SystemState struct {
	HelperInstalled bool             `json:"helperInstalled"`
	Requested       *time.Time       `json:"requested"` // petición pendiente al ayudante
	Smart           *SmartReport     `json:"smart"`
	AptAvailable    bool             `json:"aptAvailable"`
	AptCheckedAt    *time.Time       `json:"aptCheckedAt"`
	AptError        string           `json:"aptError"`
	ListsAt         *time.Time       `json:"listsAt"` // última vez que se descargó la lista de paquetes
	ListsUpdate     *AptUpdateReport `json:"listsUpdate"`
	Packages        []AptPkg         `json:"packages"`
	Security        int              `json:"security"`
	Reboot          bool             `json:"reboot"`
	RebootPkgs      []string         `json:"rebootPkgs"`
	Unattended      bool             `json:"unattended"`
}

type sysMon struct {
	mu        sync.Mutex
	st        SystemState
	reqFile   string
	lastReq   time.Time
	helperRun time.Time // fecha de apt-update.json la última vez que se simuló
}

func newSysMon(dataDir string) *sysMon {
	return &sysMon{reqFile: filepath.Join(dataDir, "helper-request"), st: SystemState{Packages: []AptPkg{}}}
}

// Request pide al ayudante de root una lectura nueva (como mucho cada 5 min).
func (m *sysMon) Request() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.st.HelperInstalled {
		return errors.New("Falta el ayudante de sistema: vuelve a ejecutar instalar.sh con la versión nueva de Umbral.")
	}
	if time.Since(m.lastReq) < 5*time.Minute {
		return nil
	}
	if err := os.WriteFile(m.reqFile, []byte(time.Now().Format(time.RFC3339)), 0o600); err != nil {
		return err
	}
	now := time.Now()
	m.lastReq, m.st.Requested = now, &now
	return nil
}

func (m *sysMon) State() SystemState {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.st
	s.Packages = append([]AptPkg{}, m.st.Packages...)
	return s
}

// Refresh lee lo que dejó el ayudante y, si hace falta, vuelve a simular la
// actualización. force la simula siempre.
func (m *sysMon) Refresh(force bool) {
	st, err := os.Stat(HelperDir)
	installed := err == nil && st.IsDir()
	var smart *SmartReport
	if b, err := os.ReadFile(filepath.Join(HelperDir, "smart.json")); err == nil {
		var r SmartReport
		if json.Unmarshal(b, &r) == nil {
			smart = &r
		}
	}
	var upd *AptUpdateReport
	if b, err := os.ReadFile(filepath.Join(HelperDir, "apt-update.json")); err == nil {
		var r AptUpdateReport
		if json.Unmarshal(b, &r) == nil {
			upd = &r
		}
	}
	_, aptErr := exec.LookPath("apt-get")

	m.mu.Lock()
	m.st.HelperInstalled, m.st.Smart, m.st.ListsUpdate, m.st.AptAvailable = installed, smart, upd, aptErr == nil
	if m.st.Requested != nil && smart != nil && smart.At.After(*m.st.Requested) {
		m.st.Requested = nil
	}
	if m.st.Requested != nil && time.Since(*m.st.Requested) > 15*time.Minute {
		m.st.Requested = nil // el ayudante no respondió; se puede volver a pedir
	}
	due := force || m.st.AptCheckedAt == nil || time.Since(*m.st.AptCheckedAt) > 6*time.Hour || (upd != nil && upd.At.After(m.helperRun))
	if upd != nil {
		m.helperRun = upd.At
	}
	m.mu.Unlock()

	reboot, pkgs := rebootRequired()
	unattended := unattendedOn()
	listsAt := aptListsTime()
	m.mu.Lock()
	m.st.Reboot, m.st.RebootPkgs, m.st.Unattended, m.st.ListsAt = reboot, pkgs, unattended, listsAt
	m.mu.Unlock()
	if !due || aptErr != nil {
		return
	}
	list, err := simulateUpgrade()
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st.AptCheckedAt = &now
	if err != nil {
		m.st.AptError = err.Error()
		return
	}
	m.st.AptError, m.st.Packages, m.st.Security = "", list, 0
	for _, p := range list {
		if p.Security {
			m.st.Security++
		}
	}
}

func simulateUpgrade() ([]AptPkg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "-o", "Dir::Cache::pkgcache=", "-o", "Dir::Cache::srcpkgcache=", "dist-upgrade")
	c.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	out, err := c.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, errors.New("apt-get no pudo calcular las actualizaciones: " + lastLines(strings.TrimSpace(string(ee.Stderr)), 1))
		}
		return nil, err
	}
	return parseAptSim(string(out)), nil
}

var reAptInst = regexp.MustCompile(`^Inst (\S+) (?:\[([^\]]+)\] )?\((\S+) ([^\[]*)`)

// parseAptSim lee las líneas «Inst paquete [actual] (nueva origen [arq])».
func parseAptSim(out string) []AptPkg {
	list := []AptPkg{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		m := reAptInst.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		list = append(list, AptPkg{Name: m[1], From: m[2], To: m[3], Security: strings.Contains(strings.ToLower(m[4]), "security")})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Security && !list[j].Security })
	return list
}

func rebootRequired() (bool, []string) {
	if _, err := os.Stat("/run/reboot-required"); err != nil {
		return false, nil
	}
	b, _ := os.ReadFile("/run/reboot-required.pkgs")
	var pkgs []string
	for _, p := range strings.Fields(string(b)) {
		if !contains(pkgs, p) {
			pkgs = append(pkgs, p)
		}
	}
	return true, pkgs
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func unattendedOn() bool {
	files, _ := filepath.Glob("/etc/apt/apt.conf.d/*")
	on := false
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, ln := range strings.Split(string(b), "\n") {
			ln = strings.TrimSpace(ln)
			if strings.HasPrefix(ln, "APT::Periodic::Unattended-Upgrade") {
				on = strings.Contains(ln, `"1"`)
			}
		}
	}
	if _, err := os.Stat("/usr/bin/unattended-upgrade"); err != nil {
		return false
	}
	return on
}

func aptListsTime() *time.Time {
	files, _ := filepath.Glob("/var/lib/apt/lists/*Release")
	var newest time.Time
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && st.ModTime().After(newest) {
			newest = st.ModTime()
		}
	}
	if newest.IsZero() {
		return nil
	}
	return &newest
}
