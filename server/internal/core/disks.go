package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

/* Disco externo para las copias de seguridad.
 *
 * Umbral detecta los discos USB sin ser root: lee /sys, la base de datos de
 * udev (/run/udev/data) y los puntos de montaje. Prepararlo (formatearlo en
 * ext4, montarlo en cada arranque y crear la carpeta de Umbral) sí necesita
 * root: lo hace «umbral disco», que systemd lanza (umbral-disco.path) cuando
 * el panel deja una petición. El ayudante no se fía de ella: vuelve a detectar
 * los discos y repite todas las comprobaciones antes de tocar nada. Solo
 * acepta discos USB que no estén en uso por el sistema. */

const (
	diskReqName    = "disk-request"
	diskResultName = "disco.json"
	diskUnitPath   = "/etc/systemd/system/umbral-disco.path"
	diskFSLabel    = "umbral-copias"
)

type DiskPart struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	FSType    string `json:"fsType"`
	Label     string `json:"label"`
	UUID      string `json:"uuid"`
	Mount     string `json:"mount"`
	holders   bool   // lo usa LVM, RAID o un volumen cifrado
}

type ExtDisk struct {
	Name      string     `json:"name"`
	Model     string     `json:"model"`
	Serial    string     `json:"serial"`
	SizeBytes int64      `json:"sizeBytes"`
	Parts     []DiskPart `json:"parts"`
	// ready: es el de las copias · mounted: ext4 montado sin usar ·
	// ext4: ext4 sin montar · other: otro formato o vacío · busy: no se toca.
	State     string `json:"state"`
	Mount     string `json:"mount"`
	Known     bool   `json:"known"` // su partición ext4 ya está en /etc/fstab
	CanFormat bool   `json:"canFormat"`
	Problem   string `json:"problem"`
}

type diskEnv struct{ sys, udev, mountinfo, fstab string }

var hostDisks = diskEnv{"/sys/block", "/run/udev/data", "/proc/self/mountinfo", "/etc/fstab"}

type mountEnt struct{ dev, point, fstype string }

func readMounts(p string) []mountEnt {
	b, _ := os.ReadFile(p)
	var out []mountEnt
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		sep := -1
		for i, x := range f {
			if x == "-" {
				sep = i
				break
			}
		}
		if len(f) < 5 || sep < 0 || sep+1 >= len(f) {
			continue
		}
		out = append(out, mountEnt{f[2], unescapeMount(f[4]), f[sep+1]})
	}
	return out
}

// unescapeMount deshace los \040 (espacio) y similares de mountinfo y fstab.
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

type fstabEnt struct{ spec, point string }

func readFstab(p string) []fstabEnt {
	b, _ := os.ReadFile(p)
	var out []fstabEnt
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 || strings.HasPrefix(f[0], "#") {
			continue
		}
		out = append(out, fstabEnt{f[0], unescapeMount(f[1])})
	}
	return out
}

// fstabPoint devuelve dónde monta /etc/fstab el sistema de archivos uuid.
func fstabPoint(fst []fstabEnt, uuid string) string {
	if uuid == "" {
		return ""
	}
	for _, f := range fst {
		if f.spec == "UUID="+uuid || f.spec == "/dev/disk/by-uuid/"+uuid {
			return f.point
		}
	}
	return ""
}

func udevProps(dir, devnum string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(filepath.Join(dir, "b"+devnum))
	if err != nil {
		return m
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(strings.TrimPrefix(ln, "E:"), "="); ok && strings.HasPrefix(ln, "E:") {
			m[k] = v
		}
	}
	return m
}

func readTrim(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

func sysBytes(p string) int64 {
	n, _ := strconv.ParseInt(readTrim(p), 10, 64)
	return n * 512
}

func underMntMedia(p string) bool {
	return strings.HasPrefix(p, "/mnt/") || strings.HasPrefix(p, "/media/")
}

var (
	reSkipDisk = regexp.MustCompile(`^(loop|ram|zram|sr|dm-|md|nbd|fd)`)
	notData    = map[string]string{"swap": "la memoria de intercambio (swap)", "LVM2_member": "un volumen LVM", "linux_raid_member": "un RAID", "crypto_LUKS": "un volumen cifrado", "zfs_member": "un grupo ZFS", "bcache": "bcache"}
)

// detect lista los discos USB conectados.
func (e diskEnv) detect() []ExtDisk {
	ents, _ := os.ReadDir(e.sys)
	mounts := readMounts(e.mountinfo)
	fst := readFstab(e.fstab)
	mountOf := func(devnum string) string {
		for _, m := range mounts {
			if m.dev == devnum {
				return m.point
			}
		}
		return ""
	}
	hasHolders := func(dir string) bool {
		h, _ := os.ReadDir(filepath.Join(dir, "holders"))
		return len(h) > 0
	}
	out := []ExtDisk{}
	for _, de := range ents {
		name := de.Name()
		base := filepath.Join(e.sys, name)
		devnum := readTrim(filepath.Join(base, "dev"))
		size := sysBytes(filepath.Join(base, "size"))
		if reSkipDisk.MatchString(name) || devnum == "" || size == 0 {
			continue
		}
		props := udevProps(e.udev, devnum)
		link, _ := os.Readlink(base)
		if props["ID_BUS"] != "usb" && !strings.Contains(link, "/usb") {
			continue
		}
		d := ExtDisk{Name: name, SizeBytes: size, Serial: props["ID_SERIAL_SHORT"], Parts: []DiskPart{}}
		if d.Serial == "" {
			d.Serial = props["ID_SERIAL"]
		}
		d.Model = strings.Join(strings.Fields(readTrim(filepath.Join(base, "device", "vendor"))+" "+readTrim(filepath.Join(base, "device", "model"))), " ")
		if d.Model == "" {
			d.Model = strings.ReplaceAll(props["ID_MODEL"], "_", " ")
		}
		if d.Model == "" {
			d.Model = "Disco USB"
		}
		sub, _ := os.ReadDir(base)
		for _, s := range sub {
			pb := filepath.Join(base, s.Name())
			if !strings.HasPrefix(s.Name(), name) {
				continue
			}
			if _, err := os.Stat(filepath.Join(pb, "partition")); err != nil {
				continue
			}
			pdev := readTrim(filepath.Join(pb, "dev"))
			pp := udevProps(e.udev, pdev)
			d.Parts = append(d.Parts, DiskPart{Name: s.Name(), SizeBytes: sysBytes(filepath.Join(pb, "size")), FSType: pp["ID_FS_TYPE"], Label: pp["ID_FS_LABEL"], UUID: pp["ID_FS_UUID"], Mount: mountOf(pdev), holders: hasHolders(pb)})
		}
		if len(d.Parts) == 0 && (props["ID_FS_TYPE"] != "" || mountOf(devnum) != "" || hasHolders(base)) {
			// Sistema de archivos sobre el disco entero, sin particiones.
			d.Parts = append(d.Parts, DiskPart{Name: name, SizeBytes: size, FSType: props["ID_FS_TYPE"], Label: props["ID_FS_LABEL"], UUID: props["ID_FS_UUID"], Mount: mountOf(devnum), holders: hasHolders(base)})
		}
		sort.Slice(d.Parts, func(i, j int) bool { return d.Parts[i].Name < d.Parts[j].Name })
		classifyDisk(&d, fst)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func classifyDisk(d *ExtDisk, fst []fstabEnt) {
	d.CanFormat = true
	var ext *DiskPart
	for i := range d.Parts {
		p := &d.Parts[i]
		if why, ok := notData[p.FSType]; ok || p.holders {
			if !ok {
				why = "un volumen LVM, RAID o cifrado"
			}
			d.State, d.CanFormat, d.Problem = "busy", false, "Forma parte de "+why+". Umbral no lo toca."
			return
		}
		if p.Mount != "" {
			d.CanFormat = false
			if !underMntMedia(p.Mount) {
				d.State, d.Problem = "busy", "El sistema lo está usando (montado en "+p.Mount+"). Umbral no lo toca."
				return
			}
		}
		if p.FSType == "ext4" && (ext == nil || p.SizeBytes > ext.SizeBytes) {
			ext = p
		}
	}
	if ext != nil {
		d.Known = fstabPoint(fst, ext.UUID) != ""
		d.State, d.Mount = "ext4", ext.Mount
		if ext.Mount != "" {
			d.State = "mounted"
		}
	} else {
		d.State = "other"
	}
	if !d.CanFormat {
		for _, p := range d.Parts {
			if p.Mount != "" {
				d.Problem = "Para formatearlo, desmóntalo antes con: sudo umount " + p.Mount
				if d.State == "other" {
					d.State = "busy"
				}
				break
			}
		}
	}
}

/* ---------- Parte de Umbral ---------- */

type diskRequest struct {
	ID     string    `json:"id"`
	Action string    `json:"action"` // usar | formatear
	Disk   string    `json:"disk"`
	Serial string    `json:"serial"`
	Size   int64     `json:"size"`
	At     time.Time `json:"at"`
}

type diskResult struct {
	ID    string    `json:"id"`
	At    time.Time `json:"at"`
	Step  string    `json:"step"`
	Done  bool      `json:"done"`
	Error string    `json:"error"`
	Dir   string    `json:"dir"`
}

func DiskHelperInstalled() bool {
	_, err := os.Stat(diskUnitPath)
	return err == nil
}

// ExternalDisks lista los discos USB y marca el que guarda las copias.
func (a *App) ExternalDisks() []ExtDisk {
	l := hostDisks.detect()
	dst := a.Cfg.Get().BackupCopyDir
	for i := range l {
		if m := l[i].Mount; m != "" && dst != "" && (dst == m || strings.HasPrefix(dst, m+"/")) {
			l[i].State = "ready"
		}
	}
	return l
}

var diskStepLabels = map[string]string{
	"check":  "Comprobando el disco",
	"format": "Borrando el disco y formateándolo en ext4",
	"mount":  "Montándolo y haciendo que se monte en cada arranque",
	"folder": "Creando la carpeta de Umbral",
	"save":   "Guardando la carpeta para las copias",
}

// PrepareDisk pide al ayudante de root que prepare un disco para las copias.
func (a *App) PrepareDisk(user, action, name, serial string, size int64) (*Job, error) {
	if !DiskHelperInstalled() {
		return nil, errors.New("Falta el ayudante de discos: vuelve a ejecutar instalar.sh con esta versión de Umbral.")
	}
	if a.busy("disk", "") || a.busy("ncfiles", "") {
		return nil, errors.New("Espera a que termine la tarea en curso con el disco externo.")
	}
	var d *ExtDisk
	for _, x := range a.ExternalDisks() {
		if x.Name == name && x.Serial == serial && x.SizeBytes == size {
			d = &x
			break
		}
	}
	if d == nil {
		return nil, errors.New("Ese disco ya no está conectado o ha cambiado. Vuelve a comprobarlo.")
	}
	keys := []string{"check", "mount", "folder", "save"}
	switch action {
	case "formatear":
		if !d.CanFormat {
			return nil, errors.New(d.Problem)
		}
		keys = []string{"check", "format", "mount", "folder", "save"}
	case "usar":
		if d.State != "ext4" && d.State != "mounted" && d.State != "ready" {
			return nil, errors.New("Ese disco no tiene una partición ext4: hay que prepararlo (borra lo que tenga).")
		}
	default:
		return nil, errors.New("Acción desconocida.")
	}
	labels := make([]string, len(keys))
	for i, k := range keys {
		labels[i] = diskStepLabels[k]
	}
	j := a.newJob("disk", d.Name, labels...)
	b, _ := json.Marshal(diskRequest{ID: j.ID, Action: action, Disk: d.Name, Serial: d.Serial, Size: d.SizeBytes, At: time.Now()})
	if err := writeFileAtomic(filepath.Join(a.DataDir, diskReqName), b, 0o600); err != nil {
		j.fail(err.Error())
		return nil, err
	}
	j.step(0)
	what := d.Model + " (" + d.Name + ")"
	go a.followDisk(j, keys, user, what, action)
	return j, nil
}

// followDisk sigue el resultado que va dejando el ayudante.
func (a *App) followDisk(j *Job, keys []string, user, what, action string) {
	idx := map[string]int{}
	for i, k := range keys {
		idx[k] = i
	}
	verb := map[string]string{"usar": "Usar disco para las copias", "formatear": "Formatear disco para las copias"}[action]
	start, seen := time.Now(), false
	for {
		time.Sleep(time.Second)
		var res diskResult
		if b, err := os.ReadFile(filepath.Join(HelperDir, diskResultName)); err == nil && json.Unmarshal(b, &res) == nil && res.ID == j.ID {
			seen = true
			if i, ok := idx[res.Step]; ok {
				j.step(i)
			}
			if res.Done {
				if res.Error != "" {
					j.fail(res.Error)
					a.Log(user, what, verb, errors.New(res.Error), "")
					return
				}
				j.step(len(keys) - 1)
				p := a.Cfg.Public()
				p.BackupCopyDir = res.Dir
				err := a.Cfg.Update(p)
				a.Log(user, what, verb, err, res.Dir)
				if err != nil {
					j.fail("El disco está listo, pero no se pudo guardar la carpeta: " + err.Error())
					return
				}
				a.Refresh("slow")
				j.finish("Listo: las copias se guardarán en "+res.Dir+".", map[string]string{"dir": res.Dir})
				return
			}
		}
		switch {
		case !seen && time.Since(start) > 90*time.Second:
			j.fail("El ayudante de discos no respondió. Comprueba en el servidor: sudo systemctl status umbral-disco.path")
			return
		case time.Since(start) > 30*time.Minute:
			j.fail("El ayudante de discos tardó demasiado. Revisa en el servidor: journalctl -u umbral-disco")
			return
		}
	}
}

// diskMounted dice si hay un disco montado en la ruta p o en una carpeta que
// la contiene, dentro de /mnt o /media.
func diskMounted(p string) bool {
	for _, m := range readMounts(hostDisks.mountinfo) {
		if underMntMedia(m.point) && (p == m.point || strings.HasPrefix(p, m.point+"/")) {
			return true
		}
	}
	return false
}

/* ---------- Parte de root: umbral disco ---------- */

var reJobID = regexp.MustCompile(`^[0-9a-f]{12}$`)

// RunDiskHelper es lo que ejecuta systemd como root cuando el panel pide
// preparar un disco. Lee la petición, la borra y deja el resultado en outDir.
func RunDiskHelper(dataDir, outDir string) error {
	reqPath := filepath.Join(dataDir, diskReqName)
	st, err := os.Lstat(reqPath)
	if err != nil {
		return nil // no hay petición
	}
	defer os.Remove(reqPath)
	if !st.Mode().IsRegular() || st.Size() > 4096 {
		return errors.New("la petición no es válida")
	}
	b, err := os.ReadFile(reqPath)
	if err != nil {
		return err
	}
	var req diskRequest
	if err := json.Unmarshal(b, &req); err != nil || !reJobID.MatchString(req.ID) {
		return errors.New("la petición no es válida")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	res := diskResult{ID: req.ID}
	report := func(step string) {
		res.Step, res.At = step, time.Now()
		_ = writeRootJSON(outDir, diskResultName, res)
	}
	err = prepareDisk(req, report, &res)
	res.Done, res.At = true, time.Now()
	if err != nil {
		res.Error = err.Error()
	}
	return errors.Join(err, writeRootJSON(outDir, diskResultName, res))
}

func prepareDisk(req diskRequest, report func(string), res *diskResult) error {
	report("check")
	if time.Since(req.At) > 5*time.Minute || time.Until(req.At) > time.Minute {
		return errors.New("La petición caducó. Vuelve a intentarlo desde el panel.")
	}
	var d *ExtDisk
	for _, x := range hostDisks.detect() {
		if x.Name == req.Disk && x.Serial == req.Serial && x.SizeBytes == req.Size {
			d = &x
			break
		}
	}
	if d == nil {
		return errors.New("El disco ya no está conectado o ha cambiado. Vuelve a comprobarlo en el panel.")
	}
	var part DiskPart
	switch req.Action {
	case "formatear":
		if !d.CanFormat {
			return errors.New(d.Problem)
		}
		report("format")
		p, err := formatDisk(d.Name)
		if err != nil {
			return err
		}
		part = p
	case "usar":
		for _, p := range d.Parts {
			if p.FSType == "ext4" && p.UUID != "" && (part.Name == "" || p.SizeBytes > part.SizeBytes) {
				part = p
			}
		}
		if part.Name == "" || d.State == "busy" {
			return errors.New("El disco no tiene una partición ext4 que se pueda usar.")
		}
	default:
		return errors.New("Acción desconocida.")
	}
	report("mount")
	mp, err := mountForCopies(part)
	if err != nil {
		return err
	}
	report("folder")
	dir := filepath.Join(mp, "umbral")
	if err := umbralDir(dir); err != nil {
		return err
	}
	res.Dir = dir
	return nil
}

func runCmd(timeout time.Duration, stdin, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	c.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	out, err := c.CombinedOutput()
	if err != nil {
		msg := lastLines(strings.TrimSpace(string(out)), 2)
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s falló: %s", name, msg)
	}
	return nil
}

func udevSettle() { _ = runCmd(30*time.Second, "", "udevadm", "settle", "--timeout=20") }

// partDev es el nombre de la primera partición: sdb1, pero nvme0n1p1.
func partDev(disk string) string {
	if c := disk[len(disk)-1]; c >= '0' && c <= '9' {
		return disk + "p1"
	}
	return disk + "1"
}

// formatDisk borra el disco y deja una sola partición ext4.
func formatDisk(name string) (DiskPart, error) {
	dev := "/dev/" + name
	if err := runCmd(time.Minute, "", "wipefs", "-a", dev); err != nil {
		return DiskPart{}, err
	}
	var err error
	if _, e := exec.LookPath("sfdisk"); e == nil {
		err = runCmd(time.Minute, "label: gpt\n,,L\n", "sfdisk", "--quiet", "--wipe", "always", dev)
	} else if _, e := exec.LookPath("parted"); e == nil {
		err = runCmd(time.Minute, "", "parted", "-s", dev, "mklabel", "gpt", "mkpart", "copias", "ext4", "0%", "100%")
	} else {
		return DiskPart{}, errors.New("Falta una herramienta para crear particiones. Instálala en el servidor con: sudo apt install fdisk")
	}
	if err != nil {
		return DiskPart{}, err
	}
	udevSettle()
	pname := partDev(name)
	pdev := "/dev/" + pname
	for i := 0; i < 30; i++ {
		if _, err := os.Stat(pdev); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	_ = runCmd(time.Minute, "", "wipefs", "-a", pdev) // restos del formato anterior
	if err := runCmd(15*time.Minute, "", "mkfs.ext4", "-F", "-q", "-m", "0", "-L", diskFSLabel, pdev); err != nil {
		return DiskPart{}, err
	}
	udevSettle()
	p := DiskPart{Name: pname, FSType: "ext4"}
	if out, err := exec.Command("blkid", "-s", "UUID", "-o", "value", pdev).Output(); err == nil {
		p.UUID = strings.TrimSpace(string(out))
	}
	if p.UUID == "" {
		for _, d := range hostDisks.detect() {
			for _, x := range d.Parts {
				if x.Name == pname {
					p.UUID = x.UUID
				}
			}
		}
	}
	if p.UUID == "" {
		return p, errors.New("El disco quedó formateado, pero no se pudo leer su identificador. Desconéctalo, vuelve a conectarlo y pulsa «Usar este disco».")
	}
	return p, nil
}

// mountForCopies monta la partición y la añade a /etc/fstab (con nofail, para
// que el servidor arranque igual si el disco no está). Devuelve dónde quedó.
func mountForCopies(p DiskPart) (string, error) {
	fst := readFstab(hostDisks.fstab)
	mp := fstabPoint(fst, p.UUID)
	if mp != "" && !underMntMedia(mp) {
		return "", errors.New("Este disco ya figura en /etc/fstab montado en " + mp + ", fuera de /mnt y /media. Umbral no cambia esa configuración.")
	}
	if mp == "" {
		mp = p.Mount
		if mp == "" {
			mp = freeMountPoint(fst, p.UUID)
		}
		if mp == "" {
			return "", errors.New("No hay una carpeta libre en /mnt para montar el disco (/mnt/copias ya está en uso).")
		}
		if err := addFstab(p.UUID, mp); err != nil {
			return "", fmt.Errorf("no se pudo añadir el disco a /etc/fstab: %v", err)
		}
		_ = runCmd(time.Minute, "", "systemctl", "daemon-reload")
	}
	if p.Mount != "" && p.Mount != mp {
		return "", errors.New("El disco está montado en " + p.Mount + ", pero /etc/fstab lo monta en " + mp + ". Desmóntalo (sudo umount " + p.Mount + ") y vuelve a intentarlo.")
	}
	if p.Mount == "" {
		if err := os.MkdirAll(mp, 0o755); err != nil {
			return "", err
		}
		if err := runCmd(2*time.Minute, "", "mount", mp); err != nil {
			return "", err
		}
	}
	for _, m := range readMounts(hostDisks.mountinfo) {
		if m.point == mp && m.fstype == "ext4" {
			return mp, nil
		}
	}
	return "", errors.New("El disco no quedó montado en " + mp + ".")
}

func freeMountPoint(fst []fstabEnt, uuid string) string {
	mounts := readMounts(hostDisks.mountinfo)
	cands := []string{"/mnt/copias"}
	if len(uuid) >= 8 {
		cands = append(cands, "/mnt/copias-"+strings.ToLower(uuid[:8]))
	}
next:
	for _, c := range cands {
		for _, f := range fst {
			if f.point == c {
				continue next
			}
		}
		for _, m := range mounts {
			if m.point == c {
				continue next
			}
		}
		st, err := os.Lstat(c)
		if errors.Is(err, os.ErrNotExist) {
			return c
		}
		if err != nil || !st.IsDir() {
			continue
		}
		if ents, _ := os.ReadDir(c); len(ents) == 0 {
			return c
		}
	}
	return ""
}

// addFstab añade la línea del disco y guarda antes una copia de /etc/fstab.
func addFstab(uuid, mp string) error {
	const path = "/etc/fstab"
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(old) > 0 {
		if err := os.WriteFile(path+".antes-de-umbral", old, 0o644); err != nil {
			return err
		}
	}
	b := append([]byte{}, old...)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	b = append(b, fmt.Sprintf("# Umbral: disco de copias de seguridad\nUUID=%s %s ext4 defaults,nofail,x-systemd.device-timeout=10s 0 2\n", uuid, mp)...)
	f, err := os.CreateTemp("/etc", ".fstab-umbral-*")
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr, os.Chmod(f.Name(), 0o644)); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), path)
}

// umbralDir crea la carpeta de las copias y se la da al usuario umbral.
func umbralDir(dir string) error {
	st, err := os.Lstat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(dir, 0o750); err != nil {
			return err
		}
	case err != nil:
		return err
	case !st.IsDir():
		return errors.New(dir + " ya existe y no es una carpeta.")
	}
	uid, gid, ok := passwdIDs("/etc/passwd", "umbral")
	if !ok {
		return errors.New("No existe el usuario umbral en el servidor.")
	}
	if err := os.Lchown(dir, uid, gid); err != nil {
		return err
	}
	return os.Chmod(dir, 0o750)
}

// passwdIDs busca el uid y el gid de un usuario en /etc/passwd.
func passwdIDs(path, name string) (uid, gid int, ok bool) {
	b, _ := os.ReadFile(path)
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Split(ln, ":")
		if len(f) < 4 || f[0] != name {
			continue
		}
		u, e1 := strconv.Atoi(f[2])
		g, e2 := strconv.Atoi(f[3])
		return u, g, e1 == nil && e2 == nil
	}
	return 0, 0, false
}
