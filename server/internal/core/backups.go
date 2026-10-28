package core

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

/* Copias de seguridad fuera del disco del servidor: se pueden descargar desde
 * el panel o copiar a otra carpeta (normalmente un disco USB montado en
 * /media o /mnt). systemd solo deja escribir a Umbral en esas dos rutas. */

type BackupFile struct {
	Name   string    `json:"name"`
	DB     string    `json:"db"`
	SizeKB int64     `json:"sizeKB"`
	At     time.Time `json:"at"`
	Copied bool      `json:"copied"` // ya está en la carpeta externa
}

type CopyDirStatus struct {
	Path     string `json:"path"`
	OK       bool   `json:"ok"`
	Problem  string `json:"problem"`
	FreeGB   int64  `json:"freeGB"`
	SameDisk bool   `json:"sameDisk"`
	Files    int    `json:"files"`
	LastCopy string `json:"lastCopy"`
}

var reBackup = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)-(\d{8}-\d{6})\.sql\.gz$`)

func (a *App) backupDir() string { return filepath.Join(a.Cfg.Get().DataRoot, "copias") }

func listBackups(dir string) []BackupFile {
	ents, _ := os.ReadDir(dir)
	out := []BackupFile{}
	for _, e := range ents {
		m := reBackup.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupFile{Name: e.Name(), DB: m[1], SizeKB: info.Size() / 1024, At: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// Backups lista las copias y, si hay carpeta externa, cuáles ya están allí.
func (a *App) Backups() ([]BackupFile, *CopyDirStatus) {
	list := listBackups(a.backupDir())
	dst := a.Cfg.Get().BackupCopyDir
	if dst == "" {
		return list, nil
	}
	st := checkCopyDir(dst, a.backupDir())
	there := map[string]bool{}
	for _, b := range listBackups(dst) {
		there[b.Name] = true
	}
	for i := range list {
		list[i].Copied = there[list[i].Name]
	}
	return list, &st
}

// ValidCopyDir: una ruta absoluta dentro de /media o /mnt.
func ValidCopyDir(p string) error {
	if p == "" {
		return nil
	}
	c := path.Clean(p)
	if !strings.HasPrefix(c, "/") || !(strings.HasPrefix(c, "/media/") || strings.HasPrefix(c, "/mnt/")) {
		return errors.New("La carpeta externa debe estar dentro de /media o /mnt, por ejemplo /mnt/usb/umbral.")
	}
	return nil
}

func checkCopyDir(dst, src string) CopyDirStatus {
	s := CopyDirStatus{Path: dst}
	st, err := os.Stat(dst)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.Problem = "La carpeta no existe. ¿Está conectado y montado el disco? Créala con: sudo mkdir -p " + dst + " && sudo chown umbral: " + dst
		return s
	case err != nil:
		s.Problem = "No se puede leer la carpeta: " + err.Error()
		return s
	case !st.IsDir():
		s.Problem = "La ruta no es una carpeta."
		return s
	}
	probe := filepath.Join(dst, ".umbral-prueba")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		s.Problem = "Umbral no puede escribir en ella. Dale permiso con: sudo chown umbral: " + dst
		return s
	}
	_ = os.Remove(probe)
	s.SameDisk = sameDevice(dst, src)
	s.FreeGB = freeGB(dst)
	files := listBackups(dst)
	s.Files = len(files)
	if len(files) > 0 {
		s.LastCopy = files[0].At.Format(time.RFC3339)
	}
	s.OK = true
	if s.SameDisk {
		s.Problem = "Está en el mismo disco que las copias originales: si el disco falla, se perderían las dos. Usa un disco distinto."
	}
	return s
}

// CopyBackups copia a la carpeta externa las copias que aún no están y deja
// allí las 10 más recientes de cada base de datos.
func (a *App) CopyBackups(user string) (int, error) {
	dst := a.Cfg.Get().BackupCopyDir
	if dst == "" {
		return 0, errors.New("Elige antes una carpeta externa en Servidor › Copias de seguridad.")
	}
	st := checkCopyDir(dst, a.backupDir())
	if !st.OK {
		return 0, errors.New(st.Problem)
	}
	n := 0
	var err error
	for _, b := range listBackups(a.backupDir()) {
		if _, e := os.Stat(filepath.Join(dst, b.Name)); e == nil {
			continue
		}
		if err = copyFile(filepath.Join(a.backupDir(), b.Name), filepath.Join(dst, b.Name)); err != nil {
			break
		}
		n++
	}
	// Rotación en la carpeta externa: 10 por base de datos.
	byDB := map[string][]BackupFile{}
	for _, b := range listBackups(dst) {
		byDB[b.DB] = append(byDB[b.DB], b)
	}
	for _, l := range byDB {
		for _, old := range l[min(len(l), 10):] {
			_ = os.Remove(filepath.Join(dst, old.Name))
		}
	}
	a.Log(user, "Copias de seguridad", "Copia a carpeta externa", err, fmt.Sprintf("%d archivos a %s", n, dst))
	return n, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".parcial"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("no se pudo copiar %s: %v", filepath.Base(src), err)
	}
	if st, e := in.Stat(); e == nil {
		_ = os.Chtimes(tmp, st.ModTime(), st.ModTime())
	}
	return os.Rename(tmp, dst)
}

// BackupPath devuelve la ruta de una copia para descargarla, o un error si
// el nombre no es el de una copia.
func (a *App) BackupPath(name string) (string, error) {
	if !reBackup.MatchString(name) {
		return "", errors.New("Esa copia no existe.")
	}
	p := filepath.Join(a.backupDir(), name)
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
		return "", errors.New("Esa copia no existe.")
	}
	return p, nil
}
