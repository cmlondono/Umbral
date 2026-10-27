package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Pasos especiales para actualizar Nextcloud sin poner en riesgo los datos:
// modo de mantenimiento, copia de la base de datos antes de tocar nada y
// espera a que Nextcloud termine de actualizar su base de datos.

func isNextcloudImage(ref string) bool {
	repo, _ := splitRef(ref)
	return repo == "nextcloud" || strings.HasSuffix(repo, "/nextcloud")
}

func (a *App) occ(ctx context.Context, ctr string, args ...string) (string, error) {
	var out bytes.Buffer
	stderr, code, err := a.Docker.Exec(ctx, ctr, "www-data", append([]string{"php", "/var/www/html/occ"}, args...), &out)
	if err != nil {
		return "", err
	}
	if code != 0 {
		msg := strings.TrimSpace(stderr + " " + out.String())
		return out.String(), fmt.Errorf("occ %s terminó con código %d: %s", strings.Join(args, " "), code, lastLines(msg, 2))
	}
	return out.String(), nil
}

type occStatus struct {
	Installed      bool   `json:"installed"`
	VersionString  string `json:"versionstring"`
	Maintenance    bool   `json:"maintenance"`
	NeedsDBUpgrade bool   `json:"needsDbUpgrade"`
}

func (a *App) ncStatus(ctx context.Context, ctr string) (occStatus, error) {
	var st occStatus
	out, err := a.occ(ctx, ctr, "status", "--output=json")
	if err != nil {
		return st, err
	}
	// occ puede escribir avisos antes del JSON.
	if i := strings.Index(out, "{"); i >= 0 {
		out = out[i:]
	}
	err = json.Unmarshal([]byte(strings.TrimSpace(out)), &st)
	return st, err
}

// findDatabase busca el contenedor de base de datos de la misma pila de
// Docker Compose que Nextcloud.
func (a *App) findDatabase(raw map[string]json.RawMessage) (name, kind string) {
	var cfg struct{ Labels map[string]string }
	_ = json.Unmarshal(raw["Config"], &cfg)
	project := cfg.Labels["com.docker.compose.project"]
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, c := range a.docker.Containers {
		if c.State != "running" || (project != "" && c.labels["com.docker.compose.project"] != project) {
			continue
		}
		repo, _ := splitRef(c.Image)
		base := repo[strings.LastIndex(repo, "/")+1:]
		switch {
		case strings.HasPrefix(base, "mariadb"), strings.HasPrefix(base, "mysql"):
			return c.Name, "mysql"
		case strings.HasPrefix(base, "postgres"):
			return c.Name, "postgres"
		}
	}
	return "", ""
}

const mysqlDump = `if command -v mariadb-dump >/dev/null 2>&1; then D=mariadb-dump; else D=mysqldump; fi
P="${MARIADB_ROOT_PASSWORD:-$MYSQL_ROOT_PASSWORD}"
if [ -n "$P" ]; then exec $D --single-transaction --quick --all-databases -uroot -p"$P"; fi
U="${MARIADB_USER:-$MYSQL_USER}"; PW="${MARIADB_PASSWORD:-$MYSQL_PASSWORD}"; DB="${MARIADB_DATABASE:-$MYSQL_DATABASE}"
exec $D --single-transaction --quick -u"$U" -p"$PW" "$DB"`

// backupDatabase vuelca la base de datos a un archivo comprimido en
// <dataRoot>/copias y conserva las 5 copias más recientes de cada contenedor.
func (a *App) backupDatabase(ctx context.Context, db, kind string) (string, error) {
	dir := filepath.Join(a.Cfg.Get().DataRoot, "copias")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("no se pudo crear %s: %v", dir, err)
	}
	path := filepath.Join(dir, db+"-"+time.Now().Format("20060102-150405")+".sql.gz")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	gz := gzip.NewWriter(f)
	counter := &countWriter{w: gz}
	cmd := []string{"sh", "-c", mysqlDump}
	if kind == "postgres" {
		cmd = []string{"sh", "-c", `exec pg_dumpall -U "${POSTGRES_USER:-postgres}"`}
	}
	stderr, code, err := a.Docker.Exec(ctx, db, "", cmd, counter)
	cerr := gz.Close()
	ferr := f.Close()
	if err == nil && code != 0 {
		err = fmt.Errorf("la copia terminó con código %d: %s", code, lastLines(strings.TrimSpace(stderr), 2))
	}
	if err == nil && counter.n < 1000 {
		err = errors.New("la copia salió vacía")
	}
	if err == nil {
		err = errors.Join(cerr, ferr)
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	// Conserva las 5 copias más recientes de esta base de datos.
	old, _ := filepath.Glob(filepath.Join(dir, db+"-*.sql.gz"))
	sort.Strings(old)
	for len(old) > 5 {
		_ = os.Remove(old[0])
		old = old[1:]
	}
	return path, nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// waitNextcloudUpgrade espera a que el contenedor nuevo termine de actualizar
// Nextcloud (lo hace solo al arrancar) y comprueba que ya usa la versión nueva.
func (a *App) waitNextcloudUpgrade(ctx context.Context, ctr, want string, progress func(string)) error {
	deadline := time.Now().Add(20 * time.Minute)
	ok := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
		// Un reinicio suelto es normal: el arranque de Nextcloud puede fallar en
		// un paso final (por ejemplo, si MariaDB corta una conexión) y en el
		// segundo arranque ya no hay nada que actualizar. Solo se da por fallido
		// si el contenedor no se estabiliza.
		if raw, err := a.Docker.Inspect(ctx, ctr); err == nil {
			var restarts int
			_ = json.Unmarshal(raw["RestartCount"], &restarts)
			if restarts >= 5 {
				return fmt.Errorf("el contenedor nuevo se ha reiniciado %d veces y no se estabiliza", restarts)
			}
		}
		s, err := a.ncStatus(ctx, ctr)
		if err != nil || !s.Installed {
			progress("Nextcloud está arrancando y actualizándose…")
			ok = 0
			continue
		}
		if s.NeedsDBUpgrade || (want != "" && !strings.HasPrefix(want, s.VersionString) && !strings.HasPrefix(s.VersionString, want)) {
			progress("Actualizando la base de datos (versión " + s.VersionString + ")…")
			ok = 0
			continue
		}
		// Dos lecturas seguidas correctas, para no cortar el proceso a medias.
		if ok++; ok >= 2 {
			return nil
		}
	}
	return errors.New("Nextcloud no terminó de actualizarse en 20 minutos")
}
