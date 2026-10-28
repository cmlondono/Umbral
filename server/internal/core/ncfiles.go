package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

/* Copia de los archivos de Nextcloud al disco externo.
 *
 * Los archivos son de www-data y Umbral no puede (ni debe) leerlos. La copia
 * la hace un contenedor temporal con la propia imagen de Nextcloud, que ya
 * trae rsync, con los volúmenes de Nextcloud en solo lectura y la carpeta
 * externa montada. Sin red y se borra al terminar.
 *
 * Cada copia es una instantánea completa en <externa>/nextcloud-archivos/
 * <fecha>, pero los archivos que no han cambiado son enlaces duros a la
 * anterior (rsync --link-dest): solo ocupa espacio lo nuevo. Por eso el disco
 * externo necesita un sistema de archivos de Linux (ext4), no exFAT ni NTFS.
 * Incluye config, custom_apps, themes y la carpeta de datos; las miniaturas
 * no, porque Nextcloud las regenera. Antes se copia la base de datos. */

type FileSnapshot struct {
	Name          string    `json:"name"`
	At            time.Time `json:"at"`
	OK            bool      `json:"ok"`
	Error         string    `json:"error"`
	Files         int64     `json:"files"`
	TransferredMB int64     `json:"transferredMB"`
	TotalMB       int64     `json:"totalMB"`
	Seconds       int       `json:"seconds"`
	Maintenance   bool      `json:"maintenance"`
	Exists        bool      `json:"exists"` // sigue en el disco externo
}

const ncFilesDir = "nextcloud-archivos"

var reSnap = regexp.MustCompile(`^\d{8}-\d{6}$`)

func (a *App) ncFilesLog() []FileSnapshot {
	var l []FileSnapshot
	_ = a.ncFiles.Load(&l)
	if l == nil {
		l = []FileSnapshot{}
	}
	dst := a.Cfg.Get().BackupCopyDir
	have := map[string]bool{}
	if dst != "" {
		ents, _ := os.ReadDir(filepath.Join(dst, ncFilesDir))
		for _, e := range ents {
			if e.IsDir() && reSnap.MatchString(e.Name()) {
				have[e.Name()] = true
			}
		}
	}
	for i := range l {
		l[i].Exists = have[l[i].Name]
	}
	return l
}

// checkHardlinks comprueba que el disco externo admite enlaces duros.
func checkHardlinks(dir string) error {
	a, b := filepath.Join(dir, ".umbral-enlace-a"), filepath.Join(dir, ".umbral-enlace-b")
	defer os.Remove(a)
	defer os.Remove(b)
	if err := os.WriteFile(a, []byte("ok"), 0o600); err != nil {
		return errors.New("Umbral no puede escribir en la carpeta externa.")
	}
	if err := os.Link(a, b); err != nil {
		return errors.New("El disco externo no admite enlaces duros, así que cada copia ocuparía todo de nuevo. Suele pasar con exFAT o NTFS: formatéalo en ext4.")
	}
	return nil
}

const ncCopyScript = `set -e
D=/destino/nextcloud-archivos
mkdir -p "$D"
rm -rf "$D"/*.parcial
PREV=$(ls -1 "$D" | grep -E '^[0-9]{8}-[0-9]{6}$' | sort | tail -n 1 || true)
mkdir -p "$D/$TS.parcial"
copia() {
  L=""
  if [ -n "$PREV" ] && [ -d "$D/$PREV/$2" ]; then L="--link-dest=$D/$PREV/$2"; fi
  echo "== $2"
  rsync -aH --numeric-ids --stats --exclude='/appdata_*/preview/' $L "$1/" "$D/$TS.parcial/$2/"
}
copia /var/www/html/config config
[ -d /var/www/html/custom_apps ] && copia /var/www/html/custom_apps custom_apps
[ -d /var/www/html/themes ] && copia /var/www/html/themes themes
copia "$DATADIR" data
mv "$D/$TS.parcial" "$D/$TS"
ls -1 "$D" | grep -E '^[0-9]{8}-[0-9]{6}$' | sort | awk -v k="$KEEP" '{a[NR]=$0} END{for(i=1;i<=NR-k;i++) print a[i]}' | while read -r old; do rm -rf "$D/$old"; done
echo "== hecho"
`

var (
	reRsFiles = regexp.MustCompile(`Number of regular files transferred: ([\d,.]+)`)
	reRsXfer  = regexp.MustCompile(`Total transferred file size: ([\d,.]+) bytes`)
	reRsTotal = regexp.MustCompile(`Total file size: ([\d,.]+) bytes`)
)

func rsNum(s string) int64 {
	n, _ := strconv.ParseInt(strings.NewReplacer(",", "", ".", "").Replace(s), 10, 64)
	return n
}

// parseRsyncStats suma las estadísticas de las varias pasadas de rsync.
func parseRsyncStats(out string) (files, xferMB, totalMB int64) {
	var x, t int64
	for _, m := range reRsFiles.FindAllStringSubmatch(out, -1) {
		files += rsNum(m[1])
	}
	for _, m := range reRsXfer.FindAllStringSubmatch(out, -1) {
		x += rsNum(m[1])
	}
	for _, m := range reRsTotal.FindAllStringSubmatch(out, -1) {
		t += rsNum(m[1])
	}
	return files, x >> 20, t >> 20
}

// runTemp crea, arranca y espera un contenedor temporal; devuelve su salida.
func (d *Docker) runTemp(ctx context.Context, spec map[string]any) (int, string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	if err := d.do(ctx, "POST", "/containers/create", spec, &out); err != nil {
		return -1, "", err
	}
	defer func() { _ = d.do(context.Background(), "DELETE", "/containers/"+out.ID+"?force=1", nil, nil) }()
	if err := d.do(ctx, "POST", "/containers/"+out.ID+"/start", nil, nil); err != nil {
		return -1, "", err
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://docker/containers/"+out.ID+"/wait", nil)
	res, err := d.stream.Do(req)
	if err != nil {
		return -1, "", errors.New("se perdió la conexión con Docker mientras copiaba")
	}
	var w struct{ StatusCode int }
	_ = json.NewDecoder(res.Body).Decode(&w)
	res.Body.Close()
	lreq, _ := http.NewRequestWithContext(context.Background(), "GET", "http://docker/containers/"+out.ID+"/logs?stdout=1&stderr=1&tail=400", nil)
	var buf, ebuf bytes.Buffer
	if lres, err := d.hc.Do(lreq); err == nil {
		_ = demux(lres.Body, &buf, &ebuf)
		lres.Body.Close()
	}
	return w.StatusCode, buf.String() + "\n" + ebuf.String(), nil
}

// BackupNextcloudFiles lanza la copia completa (base de datos y archivos).
func (a *App) BackupNextcloudFiles(user string) (*Job, error) {
	cfg := a.Cfg.Get()
	if cfg.BackupCopyDir == "" {
		return nil, errors.New("Elige antes la carpeta externa en Servidor › Copias de seguridad.")
	}
	name := a.nextcloudName()
	if a.busy("", name) || a.busy("ncfiles", "") {
		return nil, errors.New("Ya hay una acción en curso sobre Nextcloud.")
	}
	j := a.newJob("ncfiles", name, "Comprobando el disco externo", "Copia de la base de datos", "Copiando la base de datos al disco externo", "Copiando los archivos de Nextcloud", "Guardando la instantánea")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
		defer cancel()
		if err := a.runNCFiles(ctx, j, user); err != nil {
			j.fail(err.Error())
		}
	}()
	return j, nil
}

func (a *App) runNCFiles(ctx context.Context, j *Job, user string) (err error) {
	cfg := a.Cfg.Get()
	dst := cfg.BackupCopyDir
	name := a.nextcloudName()
	t0 := time.Now()
	snap := FileSnapshot{Name: t0.Format("20060102-150405"), At: t0, Maintenance: cfg.NCBackupMaint}
	defer func() {
		snap.Seconds = int(time.Since(t0).Seconds())
		snap.OK = err == nil
		if err != nil {
			snap.Error = err.Error()
		}
		var l []FileSnapshot
		_ = a.ncFiles.Load(&l)
		l = append([]FileSnapshot{snap}, l...)
		if len(l) > 60 {
			l = l[:60]
		}
		_ = a.ncFiles.Save(l)
		a.Log(user, name, "Copia de archivos de Nextcloud", err, dst)
		a.Refresh("docker")
	}()

	j.step(0)
	st := checkCopyDir(dst, a.backupDir())
	if !st.OK {
		return errors.New(st.Problem)
	}
	if st.SameDisk {
		return errors.New("La carpeta externa está en el mismo disco que Nextcloud: la copia no protegería de un fallo del disco.")
	}
	if err := checkHardlinks(dst); err != nil {
		return err
	}
	raw, err := a.Docker.Inspect(ctx, name)
	if err != nil {
		return errors.New("No encuentro el contenedor de Nextcloud (" + name + ").")
	}
	var image string
	_ = json.Unmarshal(raw["Image"], &image)
	datadir := "/var/www/html/data"
	if out, err := a.occ(ctx, name, "config:system:get", "datadirectory"); err == nil && strings.HasPrefix(strings.TrimSpace(out), "/") {
		datadir = strings.TrimSpace(out)
	}

	j.step(1)
	db, kind := a.findDatabase(raw)
	if db == "" {
		return errors.New("No encuentro la base de datos de Nextcloud en marcha.")
	}
	dump, err := a.backupDatabase(ctx, db, kind)
	if err != nil {
		return fmt.Errorf("la copia de la base de datos falló: %v", err)
	}
	a.Log(user, db, "Copia de seguridad", nil, dump)
	j.step(2)
	if _, err := a.CopyBackups(user); err != nil {
		return err
	}

	j.step(3)
	if cfg.NCBackupMaint {
		if _, err := a.occ(ctx, name, "maintenance:mode", "--on"); err != nil {
			return fmt.Errorf("no se pudo activar el modo de mantenimiento: %v", err)
		}
		defer func() { _, _ = a.occ(context.Background(), name, "maintenance:mode", "--off") }()
	}
	keep := cfg.NCBackupKeep
	if keep < 1 {
		keep = 7
	}
	spec := map[string]any{
		"Image":      image,
		"Entrypoint": []string{"sh", "-c", ncCopyScript},
		"User":       "0",
		"Env":        []string{"TS=" + snap.Name, "KEEP=" + strconv.Itoa(keep), "DATADIR=" + datadir},
		"Labels":     map[string]string{"umbral.temporal": "copia-nextcloud"},
		"HostConfig": map[string]any{"VolumesFrom": []string{name + ":ro"}, "Binds": []string{dst + ":/destino"}, "NetworkMode": "none"},
	}
	code, out, err := a.Docker.runTemp(ctx, spec)
	if err != nil {
		return err
	}
	snap.Files, snap.TransferredMB, snap.TotalMB = parseRsyncStats(out)
	if code != 0 {
		low := strings.ToLower(out)
		switch {
		case strings.Contains(low, "no space left"):
			return errors.New("El disco externo se ha llenado. Libera espacio o guarda menos copias.")
		case strings.Contains(low, "rsync: not found"), strings.Contains(low, "rsync: command not found"):
			return errors.New("La imagen de Nextcloud no trae rsync.")
		}
		return fmt.Errorf("la copia de archivos terminó con código %d: %s", code, lastLines(strings.TrimSpace(out), 2))
	}
	j.step(4)
	j.finish(fmt.Sprintf("Copia completa en %s/%s/%s. %d archivos nuevos o cambiados (%s); el resto se enlaza con la copia anterior.", dst, ncFilesDir, snap.Name, snap.Files, mbLabel(snap.TransferredMB<<20)), nil)
	return nil
}
