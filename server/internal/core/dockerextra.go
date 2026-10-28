package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

/* ---------- Registros de un contenedor ---------- */

type LogLine struct {
	At   string `json:"at"` // RFC 3339, puede ir vacío
	Text string `json:"text"`
	Err  bool   `json:"err"` // salió por la salida de errores
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// Logs devuelve las últimas líneas del registro de un contenedor.
func (d *Docker) Logs(ctx context.Context, name string, tail int) ([]LogLine, error) {
	var info struct {
		Config struct{ Tty bool }
	}
	if err := d.do(ctx, "GET", "/containers/"+url.PathEscape(name)+"/json", nil, &info); err != nil {
		return nil, err
	}
	q := fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1&timestamps=1&tail=%d", url.PathEscape(name), tail)
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://docker"+q, nil)
	res, err := d.hc.Do(req)
	if err != nil {
		return nil, errors.New("Docker no responde: " + friendlyNetErr(err))
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e dockerErr
		_ = json.NewDecoder(res.Body).Decode(&e)
		return nil, errors.New("No se pudo leer el registro: " + e.Message)
	}
	body := io.LimitReader(res.Body, 8<<20)
	var out, errOut bytes.Buffer
	if info.Config.Tty {
		_, err = io.Copy(&out, body)
	} else {
		err = demux(body, &out, &errOut)
	}
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	lines := append(splitLogLines(out.String(), false), splitLogLines(errOut.String(), true)...)
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].At < lines[j].At })
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return lines, nil
}

func splitLogLines(s string, isErr bool) []LogLine {
	var out []LogLine
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		ln := strings.TrimRight(sc.Text(), "\r")
		l := LogLine{Err: isErr}
		if ts, rest, ok := strings.Cut(ln, " "); ok {
			if _, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				l.At, ln = ts, rest
			}
		}
		l.Text = ansiRe.ReplaceAllString(ln, "")
		if len(l.Text) > 2000 {
			l.Text = l.Text[:2000] + "…"
		}
		out = append(out, l)
	}
	return out
}

/* ---------- Límite de memoria ---------- */

type ContainerDetails struct {
	MemLimitMB int    `json:"memLimitMB"` // 0 = sin límite
	Restart    string `json:"restart"`
	CreatedAt  string `json:"createdAt"`
	Mounts     []struct {
		Type        string `json:"type"`
		Name        string `json:"name"`
		Source      string `json:"source"`
		Destination string `json:"destination"`
	} `json:"mounts"`
}

func (d *Docker) Details(ctx context.Context, name string) (ContainerDetails, error) {
	var raw struct {
		Created    string
		HostConfig struct {
			Memory        int64
			RestartPolicy struct{ Name string }
		}
		Mounts []struct{ Type, Name, Source, Destination string }
	}
	if err := d.do(ctx, "GET", "/containers/"+url.PathEscape(name)+"/json", nil, &raw); err != nil {
		return ContainerDetails{}, err
	}
	det := ContainerDetails{MemLimitMB: int(raw.HostConfig.Memory >> 20), Restart: raw.HostConfig.RestartPolicy.Name, CreatedAt: raw.Created}
	for _, m := range raw.Mounts {
		det.Mounts = append(det.Mounts, struct {
			Type        string `json:"type"`
			Name        string `json:"name"`
			Source      string `json:"source"`
			Destination string `json:"destination"`
		}{m.Type, m.Name, m.Source, m.Destination})
	}
	return det, nil
}

// SetMemoryLimit cambia el límite de memoria sin recrear el contenedor. La
// memoria de intercambio queda al doble, como hace Docker por defecto.
// Docker no permite quitar un límite así: eso lo hace App.SetMemory recreando.
func (d *Docker) SetMemoryLimit(ctx context.Context, name string, mb int) error {
	body := map[string]int64{"Memory": int64(mb) << 20, "MemorySwap": int64(mb) << 21}
	if err := d.do(ctx, "POST", "/containers/"+url.PathEscape(name)+"/update", body, nil); err != nil {
		return errors.New("Docker no aceptó el cambio: " + err.Error())
	}
	det, err := d.Details(ctx, name)
	if err != nil {
		return err
	}
	if det.MemLimitMB != mb {
		return fmt.Errorf("Docker no aplicó el límite: ahora tiene %d MB", det.MemLimitMB)
	}
	return nil
}

/* ---------- Espacio de Docker ---------- */

type DockerVolume struct {
	Name      string `json:"name"`
	SizeMB    int64  `json:"sizeMB"` // -1 si Docker no lo calcula
	CreatedAt string `json:"createdAt"`
	Stack     string `json:"stack"`
	Anonymous bool   `json:"anonymous"`
}

type DockerDisk struct {
	BuildCacheMB int64          `json:"buildCacheMB"`
	Unused       []DockerVolume `json:"unused"` // volúmenes que ningún contenedor usa, ni detenido
	VolumesMB    int64          `json:"volumesMB"`
	ImagesMB     int64          `json:"imagesMB"`
}

var anonVolRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (d *Docker) Disk(ctx context.Context) (DockerDisk, error) {
	var raw struct {
		LayersSize int64
		Volumes    []struct {
			Name      string
			CreatedAt string
			Labels    map[string]string
			UsageData *struct{ Size, RefCount int64 }
		}
		BuildCache []struct {
			Size   int64
			InUse  bool
			Shared bool
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://docker/system/df", nil)
	res, err := d.stream.Do(req)
	if err != nil {
		return DockerDisk{}, errors.New("Docker no responde: " + friendlyNetErr(err))
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return DockerDisk{}, errors.New("Respuesta de Docker no válida.")
	}
	out := DockerDisk{ImagesMB: raw.LayersSize >> 20, Unused: []DockerVolume{}}
	for _, b := range raw.BuildCache {
		if !b.InUse && !b.Shared {
			out.BuildCacheMB += b.Size
		}
	}
	out.BuildCacheMB >>= 20
	for _, v := range raw.Volumes {
		size := int64(-1)
		if v.UsageData != nil && v.UsageData.Size >= 0 {
			size = v.UsageData.Size >> 20
			out.VolumesMB += size
		}
		if v.UsageData == nil || v.UsageData.RefCount != 0 {
			continue
		}
		out.Unused = append(out.Unused, DockerVolume{Name: v.Name, SizeMB: size, CreatedAt: v.CreatedAt,
			Stack: v.Labels["com.docker.compose.project"], Anonymous: anonVolRe.MatchString(v.Name)})
	}
	sort.Slice(out.Unused, func(i, j int) bool { return out.Unused[i].SizeMB > out.Unused[j].SizeMB })
	return out, nil
}

func (d *Docker) PruneBuildCache(ctx context.Context) (int64, error) {
	var r struct{ SpaceReclaimed int64 }
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://docker/build/prune", nil)
	res, err := d.stream.Do(req)
	if err != nil {
		return 0, errors.New("Docker no responde: " + friendlyNetErr(err))
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return 0, errors.New("Docker no pudo liberar la caché: " + res.Status)
	}
	_ = json.NewDecoder(res.Body).Decode(&r)
	return r.SpaceReclaimed >> 20, nil
}

func (d *Docker) RemoveVolume(ctx context.Context, name string) error {
	err := d.do(ctx, "DELETE", "/volumes/"+url.PathEscape(name), nil, nil)
	var ae *apiErr
	if errors.As(err, &ae) && ae.code == 409 {
		return errors.New("lo está usando un contenedor")
	}
	return err
}

// SetMemory pone, cambia o quita el límite de memoria de un contenedor.
func (a *App) SetMemory(ctx context.Context, user, name string, mb int) error {
	if a.busy("", name) {
		return errors.New("Ya hay una acción en curso para " + name + ".")
	}
	if mb != 0 && (mb < 32 || mb > 1<<20) {
		return errors.New("El límite debe ser de al menos 32 MB.")
	}
	det, err := a.Docker.Details(ctx, name)
	if err != nil {
		return err
	}
	if mb != 0 {
		a.mu.RLock()
		var used int
		for _, c := range a.docker.Containers {
			if c.Name == name && c.MemMB != nil {
				used = *c.MemMB
			}
		}
		a.mu.RUnlock()
		if used > 0 && mb < used+used/5 {
			return fmt.Errorf("Ahora usa %d MB: un límite de %d MB lo dejaría sin memoria y Docker lo cerraría. Pon al menos %d MB.", used, mb, used+used/5)
		}
		err = a.Docker.SetMemoryLimit(ctx, name, mb)
		a.Log(user, name, "Límite de memoria", err, fmt.Sprintf("%d MB", mb))
		return err
	}
	if det.MemLimitMB == 0 {
		return nil
	}
	// Quitarlo exige recrear el contenedor con la misma imagen exacta.
	raw, err := a.Docker.Inspect(ctx, name)
	if err != nil {
		return err
	}
	var host map[string]any
	var cfg struct{ Image string }
	var imgID string
	_ = json.Unmarshal(raw["HostConfig"], &host)
	_ = json.Unmarshal(raw["Config"], &cfg)
	_ = json.Unmarshal(raw["Image"], &imgID)
	host["Memory"], host["MemorySwap"] = 0, 0
	raw["HostConfig"], _ = json.Marshal(host)
	image := cfg.Image
	if id, _, err := a.Docker.ImageID(ctx, image); err != nil || id != imgID {
		image = imgID // la etiqueta ya apunta a otra versión: se usa la imagen exacta
	}
	j := a.newJob("memory", name, "Recreando "+name+" sin límite de memoria")
	j.step(0)
	err = a.swapImage(ctx, name, raw, image)
	if err == nil {
		err = a.waitRunning(ctx, name)
	}
	a.Log(user, name, "Límite de memoria", err, "Sin límite")
	if err != nil {
		j.fail(err.Error())
		return err
	}
	j.finish("Sin límite de memoria.", nil)
	return nil
}
