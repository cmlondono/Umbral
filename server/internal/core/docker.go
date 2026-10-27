package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Docker habla con la API del motor por el socket Unix, sin SDK.
type Docker struct {
	hc     *http.Client
	stream *http.Client
}

func NewDocker(socket string) *Docker {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}
	return &Docker{hc: &http.Client{Transport: tr, Timeout: 20 * time.Second}, stream: &http.Client{Transport: tr}}
}

type dockerErr struct {
	Message string `json:"message"`
}

func (d *Docker) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, "http://docker"+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := d.hc.Do(req)
	if err != nil {
		return errors.New("Docker no responde: " + friendlyNetErr(err))
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e dockerErr
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Message == "" {
			e.Message = res.Status
		}
		return &apiErr{code: res.StatusCode, msg: e.Message}
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

type apiErr struct {
	code int
	msg  string
}

func (e *apiErr) Error() string { return e.msg }

func friendlyNetErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "permission denied"):
		return "sin permiso para usar el socket de Docker. Añade el usuario umbral al grupo docker."
	case strings.Contains(s, "no such file"), strings.Contains(s, "connect: "):
		return "el socket de Docker no existe o el servicio está detenido."
	}
	return s
}

/* ---------- Contenedores ---------- */

type Container struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Image     string   `json:"image"`
	ImageID   string   `json:"-"`
	State     string   `json:"state"`
	Status    string   `json:"status"`
	Health    string   `json:"health"`
	Ports     []string `json:"ports"`
	CPUPct    *float64 `json:"cpuPct"`
	MemMB     *int     `json:"memMB"`
	StartedAt *string  `json:"startedAt"`
	URL       *string  `json:"url"`
	Compose   *string  `json:"compose"`
	labels    map[string]string
}

type DockerState struct {
	Available  bool        `json:"available"`
	Error      *string     `json:"error"`
	Version    string      `json:"version"`
	Containers []Container `json:"containers"`
	Images     []Image     `json:"images"`
}

func (d *Docker) Version(ctx context.Context) (string, error) {
	var v struct{ Version string }
	err := d.do(ctx, "GET", "/version", nil, &v)
	return v.Version, err
}

func (d *Docker) Containers(ctx context.Context) ([]Container, error) {
	var raw []struct {
		ID      string `json:"Id"`
		Names   []string
		Image   string
		ImageID string
		State   string
		Status  string
		Ports   []struct {
			IP          string
			PrivatePort int
			PublicPort  int
			Type        string
		}
		Labels map[string]string
	}
	if err := d.do(ctx, "GET", "/containers/json?all=1", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		c := Container{ID: r.ID, Image: r.Image, ImageID: r.ImageID, Status: r.Status, Ports: []string{}, labels: r.Labels, Health: "none"}
		if len(r.Names) > 0 {
			c.Name = strings.TrimPrefix(r.Names[0], "/")
		}
		switch r.State {
		case "running", "restarting", "paused", "created", "dead":
			c.State = r.State
		default:
			c.State = "stopped"
		}
		switch {
		case strings.Contains(r.Status, "(healthy)"):
			c.Health = "healthy"
		case strings.Contains(r.Status, "(unhealthy)"):
			c.Health = "unhealthy"
		case strings.Contains(r.Status, "(health: starting)"):
			c.Health = "starting"
		}
		seen := map[string]bool{}
		for _, p := range r.Ports {
			s := fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
			if p.PublicPort != 0 {
				s = fmt.Sprintf("%d→%d/%s", p.PublicPort, p.PrivatePort, p.Type)
			}
			if !seen[s] { // IPv4 e IPv6 repiten el mismo puerto
				seen[s] = true
				c.Ports = append(c.Ports, s)
			}
		}
		if p := r.Labels["com.docker.compose.project"]; p != "" {
			c.Compose = &p
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Stats pide una lectura de CPU y memoria de cada contenedor en ejecución.
func (d *Docker) Stats(ctx context.Context, cs []Container) map[string][2]float64 {
	out := map[string][2]float64{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range cs {
		if c.State != "running" {
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			var s struct {
				CPU struct {
					Usage struct {
						TotalUsage uint64 `json:"total_usage"`
					} `json:"cpu_usage"`
					System uint64 `json:"system_cpu_usage"`
					Online int    `json:"online_cpus"`
				} `json:"cpu_stats"`
				Pre struct {
					Usage struct {
						TotalUsage uint64 `json:"total_usage"`
					} `json:"cpu_usage"`
					System uint64 `json:"system_cpu_usage"`
				} `json:"precpu_stats"`
				Mem struct {
					Usage uint64            `json:"usage"`
					Stats map[string]uint64 `json:"stats"`
				} `json:"memory_stats"`
			}
			if d.do(ctx, "GET", "/containers/"+id+"/stats?stream=false", nil, &s) != nil {
				return
			}
			cpu := 0.0
			dc := float64(s.CPU.Usage.TotalUsage) - float64(s.Pre.Usage.TotalUsage)
			ds := float64(s.CPU.System) - float64(s.Pre.System)
			if dc > 0 && ds > 0 {
				n := s.CPU.Online
				if n == 0 {
					n = 1
				}
				cpu = dc / ds * float64(n) * 100
			}
			mem := float64(s.Mem.Usage)
			if v, ok := s.Mem.Stats["inactive_file"]; ok && v < s.Mem.Usage {
				mem -= float64(v)
			} else if v, ok := s.Mem.Stats["cache"]; ok && v < s.Mem.Usage {
				mem -= float64(v)
			}
			mu.Lock()
			out[id] = [2]float64{round1(cpu), mem / 1048576}
			mu.Unlock()
		}(c.ID)
	}
	wg.Wait()
	return out
}

func (d *Docker) ContainerAction(ctx context.Context, id, action string) error {
	switch action {
	case "start", "stop", "restart":
		return d.do(ctx, "POST", "/containers/"+url.PathEscape(id)+"/"+action, nil, nil)
	case "remove":
		return d.do(ctx, "DELETE", "/containers/"+url.PathEscape(id), nil, nil)
	}
	return errors.New("acción desconocida")
}

/* ---------- Imágenes ---------- */

type Image struct {
	ID          string   `json:"id"`
	Repo        string   `json:"repo"`
	Tag         string   `json:"tag"`
	SizeMB      float64  `json:"sizeMB"`
	Created     string   `json:"created"`
	UsedBy      []string `json:"usedBy"`
	ExposedPort *int     `json:"exposedPort"`
	Volumes     []string `json:"volumes"`
	Arch        string   `json:"arch"`
	digests     []string
}

type imageMeta struct {
	port    *int
	volumes []string
	arch    string
	digests []string
}

type imageCache struct {
	mu sync.Mutex
	m  map[string]imageMeta
}

var imgMeta = imageCache{m: map[string]imageMeta{}}

func (d *Docker) Images(ctx context.Context, cs []Container) ([]Image, error) {
	var raw []struct {
		ID       string `json:"Id"`
		RepoTags []string
		Size     int64
		Created  int64
	}
	if err := d.do(ctx, "GET", "/images/json", nil, &raw); err != nil {
		return nil, err
	}
	var out []Image
	for _, r := range raw {
		tags := r.RepoTags
		if len(tags) == 0 || (len(tags) == 1 && tags[0] == "<none>:<none>") {
			tags = []string{"<none>:<none>"}
		}
		meta := d.imageMeta(ctx, r.ID)
		used := []string{}
		for _, c := range cs {
			if c.ImageID == r.ID {
				used = append(used, c.Name)
			}
		}
		for _, t := range tags {
			repo, tag := splitRef(t)
			if t == "<none>:<none>" {
				repo, tag = "<none>", "<none>"
			}
			out = append(out, Image{ID: strings.TrimPrefix(r.ID, "sha256:")[:12], Repo: repo, Tag: tag, SizeMB: round1(float64(r.Size) / 1048576),
				Created: time.Unix(r.Created, 0).UTC().Format(time.RFC3339), UsedBy: used, ExposedPort: meta.port, Volumes: meta.volumes, Arch: meta.arch, digests: meta.digests})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Repo+out[i].Tag < out[j].Repo+out[j].Tag })
	return out, nil
}

func (d *Docker) imageMeta(ctx context.Context, id string) imageMeta {
	imgMeta.mu.Lock()
	m, ok := imgMeta.m[id]
	imgMeta.mu.Unlock()
	if ok {
		return m
	}
	var raw struct {
		Architecture string
		RepoDigests  []string
		Config       struct {
			ExposedPorts map[string]struct{}
			Volumes      map[string]struct{}
		}
	}
	m = imageMeta{volumes: []string{}}
	if d.do(ctx, "GET", "/images/"+id+"/json", nil, &raw) == nil {
		m.arch, m.digests = raw.Architecture, raw.RepoDigests
		ports := []int{}
		for p := range raw.Config.ExposedPorts {
			var n int
			if _, err := fmt.Sscanf(p, "%d", &n); err == nil {
				ports = append(ports, n)
			}
		}
		sort.Ints(ports)
		if len(ports) > 0 {
			m.port = &ports[0]
		}
		for v := range raw.Config.Volumes {
			m.volumes = append(m.volumes, v)
		}
		sort.Strings(m.volumes)
		imgMeta.mu.Lock()
		imgMeta.m[id] = m
		imgMeta.mu.Unlock()
	}
	return m
}

func (d *Docker) RemoveImage(ctx context.Context, id string) error {
	return d.do(ctx, "DELETE", "/images/"+url.PathEscape(id), nil, nil)
}

// Pull descarga una imagen e informa del progreso (0–100) por capas.
func (d *Docker) Pull(ctx context.Context, ref string, progress func(pct int, msg string)) error {
	repo, tag := splitRef(ref)
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://docker/images/create?fromImage="+url.QueryEscape(repo)+"&tag="+url.QueryEscape(tag), nil)
	res, err := d.stream.Do(req)
	if err != nil {
		return errors.New("Docker no responde: " + friendlyNetErr(err))
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e dockerErr
		_ = json.NewDecoder(res.Body).Decode(&e)
		return errors.New(translatePullErr(e.Message))
	}
	type layer struct{ cur, total int64 }
	layers := map[string]*layer{}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var ev struct {
			Status   string
			ID       string `json:"id"`
			Error    string
			Progress struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Error != "" {
			return errors.New(translatePullErr(ev.Error))
		}
		if ev.ID == "" || ev.ID == tag {
			continue
		}
		l := layers[ev.ID]
		if l == nil {
			l = &layer{}
			layers[ev.ID] = l
		}
		switch ev.Status {
		case "Downloading":
			l.cur, l.total = ev.Progress.Current, ev.Progress.Total
		case "Download complete", "Pull complete", "Already exists":
			if l.total == 0 {
				l.total = 1
			}
			l.cur = l.total
		}
		var cur, total int64
		done := 0
		for _, x := range layers {
			cur, total = cur+x.cur, total+x.total
			if x.total > 0 && x.cur >= x.total {
				done++
			}
		}
		pct := 0
		if total > 0 {
			pct = int(cur * 100 / total)
		}
		progress(pct, fmt.Sprintf("Capa %d de %d: %s de %s", min(done+1, len(layers)), len(layers), mbLabel(cur), mbLabel(total)))
	}
	return sc.Err()
}

func translatePullErr(m string) string {
	switch {
	case strings.Contains(m, "not found"), strings.Contains(m, "manifest unknown"):
		return "La imagen o la etiqueta no existen en el registro."
	case strings.Contains(m, "unauthorized"), strings.Contains(m, "denied"):
		return "El registro no permite descargar esta imagen sin iniciar sesión."
	case strings.Contains(m, "no matching manifest"):
		return "La imagen no está publicada para la arquitectura de tu servidor."
	case strings.Contains(m, "dial tcp"), strings.Contains(m, "timeout"):
		return "No se pudo conectar con el registro. Revisa la conexión a Internet del servidor."
	}
	return m
}

func mbLabel(b int64) string {
	mb := float64(b) / 1048576
	if mb >= 1000 {
		return strings.Replace(fmt.Sprintf("%.1f GB", mb/1024), ".", ",", 1)
	}
	return fmt.Sprintf("%.0f MB", mb)
}

func splitRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	if i := strings.Index(ref, "@"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	c, sl := strings.LastIndex(ref, ":"), strings.LastIndex(ref, "/")
	if c > sl {
		return ref[:c], ref[c+1:]
	}
	return ref, "latest"
}

/* ---------- Crear contenedores ---------- */

type CreateSpec struct {
	Name     string
	Image    string
	Env      []string
	Ports    map[string]string // "80/tcp" → "8080"
	Binds    []string
	Restart  string
	MemBytes int64
	Cmd      []string
	Labels   map[string]string
}

func (d *Docker) Create(ctx context.Context, s CreateSpec) (string, error) {
	exposed := map[string]struct{}{}
	bindings := map[string][]map[string]string{}
	for cp, hp := range s.Ports {
		exposed[cp] = struct{}{}
		bindings[cp] = []map[string]string{{"HostPort": hp}}
	}
	body := map[string]any{
		"Image": s.Image, "Cmd": s.Cmd, "Env": s.Env, "ExposedPorts": exposed, "Labels": s.Labels,
		"HostConfig": map[string]any{"PortBindings": bindings, "Binds": s.Binds, "RestartPolicy": map[string]string{"Name": s.Restart}, "Memory": s.MemBytes},
	}
	var out struct {
		ID string `json:"Id"`
	}
	err := d.do(ctx, "POST", "/containers/create?name="+url.QueryEscape(s.Name), body, &out)
	return out.ID, err
}

// Inspect devuelve la configuración cruda de un contenedor, para recrearlo
// al actualizar.
func (d *Docker) Inspect(ctx context.Context, id string) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	err := d.do(ctx, "GET", "/containers/"+url.PathEscape(id)+"/json", nil, &raw)
	return raw, err
}

func (d *Docker) Rename(ctx context.Context, id, name string) error {
	return d.do(ctx, "POST", "/containers/"+url.PathEscape(id)+"/rename?name="+url.QueryEscape(name), nil, nil)
}

func (d *Docker) ImageID(ctx context.Context, ref string) (string, []string, error) {
	var raw struct {
		ID          string `json:"Id"`
		RepoDigests []string
	}
	err := d.do(ctx, "GET", "/images/"+url.PathEscape(ref)+"/json", nil, &raw)
	return raw.ID, raw.RepoDigests, err
}

func (d *Docker) Tag(ctx context.Context, id, ref string) error {
	repo, tag := splitRef(ref)
	return d.do(ctx, "POST", "/images/"+url.PathEscape(id)+"/tag?repo="+url.QueryEscape(repo)+"&tag="+url.QueryEscape(tag), nil, nil)
}

// Recreate crea un contenedor nuevo con la misma configuración que otro,
// pero con la imagen indicada.
func (d *Docker) Recreate(ctx context.Context, raw map[string]json.RawMessage, name, image string) (string, error) {
	var cfg map[string]any
	var host map[string]any
	var netw struct{ Networks map[string]map[string]any }
	_ = json.Unmarshal(raw["Config"], &cfg)
	_ = json.Unmarshal(raw["HostConfig"], &host)
	_ = json.Unmarshal(raw["NetworkSettings"], &netw)
	if cfg == nil {
		return "", errors.New("no se pudo leer la configuración del contenedor")
	}
	cfg["Image"] = image
	delete(cfg, "Hostname")
	keepAnonymousVolumes(raw, host)
	cfg["HostConfig"] = host
	eps := map[string]any{}
	for n, ep := range netw.Networks {
		eps[n] = map[string]any{"Aliases": ep["Aliases"]}
	}
	cfg["NetworkingConfig"] = map[string]any{"EndpointsConfig": eps}
	var out struct {
		ID string `json:"Id"`
	}
	err := d.do(ctx, "POST", "/containers/create?name="+url.QueryEscape(name), cfg, &out)
	return out.ID, err
}

// keepAnonymousVolumes añade a la configuración nueva los volúmenes que el
// contenedor tenía sin estar declarados en HostConfig (los «anónimos» que
// crea una imagen con VOLUME). Sin esto, el contenedor nuevo empezaría con
// esas carpetas vacías.
func keepAnonymousVolumes(raw map[string]json.RawMessage, host map[string]any) {
	var mounts []struct {
		Type, Name, Destination string
	}
	_ = json.Unmarshal(raw["Mounts"], &mounts)
	covered := map[string]bool{}
	var binds []any
	if b, ok := host["Binds"].([]any); ok {
		binds = b
		for _, x := range b {
			if s, ok := x.(string); ok {
				if parts := strings.Split(s, ":"); len(parts) >= 2 {
					covered[parts[1]] = true
				}
			}
		}
	}
	if ms, ok := host["Mounts"].([]any); ok {
		for _, x := range ms {
			if m, ok := x.(map[string]any); ok {
				if t, ok := m["Target"].(string); ok {
					covered[t] = true
				}
			}
		}
	}
	for _, m := range mounts {
		if m.Type == "volume" && m.Name != "" && !covered[m.Destination] {
			binds = append(binds, m.Name+":"+m.Destination)
		}
	}
	if len(binds) > 0 {
		host["Binds"] = binds
	}
}

// ImageEnv devuelve el valor de una variable de entorno declarada en una imagen.
func (d *Docker) ImageEnv(ctx context.Context, ref, key string) string {
	var raw struct{ Config struct{ Env []string } }
	if d.do(ctx, "GET", "/images/"+url.PathEscape(ref)+"/json", nil, &raw) != nil {
		return ""
	}
	for _, e := range raw.Config.Env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v
		}
	}
	return ""
}

// Exec ejecuta una orden dentro de un contenedor y devuelve su salida
// estándar (en w), su salida de errores y su código de salida.
func (d *Docker) Exec(ctx context.Context, ctr, user string, cmd []string, w io.Writer) (stderr string, code int, err error) {
	var created struct {
		ID string `json:"Id"`
	}
	body := map[string]any{"Cmd": cmd, "AttachStdout": true, "AttachStderr": true, "User": user}
	if err := d.do(ctx, "POST", "/containers/"+url.PathEscape(ctr)+"/exec", body, &created); err != nil {
		return "", -1, err
	}
	b, _ := json.Marshal(map[string]bool{"Detach": false, "Tty": false})
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://docker/exec/"+created.ID+"/start", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	res, err := d.stream.Do(req)
	if err != nil {
		return "", -1, errors.New("Docker no responde: " + friendlyNetErr(err))
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", -1, errors.New("no se pudo ejecutar la orden en " + ctr + ": " + res.Status)
	}
	var errBuf bytes.Buffer
	if err := demux(res.Body, w, &errBuf); err != nil {
		return errBuf.String(), -1, err
	}
	var st struct{ ExitCode int }
	if err := d.do(ctx, "GET", "/exec/"+created.ID+"/json", nil, &st); err != nil {
		return errBuf.String(), -1, err
	}
	return errBuf.String(), st.ExitCode, nil
}

// demux separa la salida multiplexada de Docker (cabecera de 8 bytes por
// bloque: tipo de flujo y tamaño) en salida estándar y salida de errores.
func demux(r io.Reader, out, errOut io.Writer) error {
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		n := int64(hdr[4])<<24 | int64(hdr[5])<<16 | int64(hdr[6])<<8 | int64(hdr[7])
		dst := out
		if hdr[0] == 2 {
			dst = errOut
		}
		if _, err := io.CopyN(dst, r, n); err != nil {
			return err
		}
	}
}
