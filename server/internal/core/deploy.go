package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type EnvVar struct {
	K      string `json:"k"`
	V      string `json:"v,omitempty"`
	Secret bool   `json:"secret,omitempty"`
}

type DataDir struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type CatalogApp struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	Desc       string    `json:"desc"`
	Image      string    `json:"image"`
	Port       int       `json:"port"`
	CPort      int       `json:"cport"`
	ExtraPorts []string  `json:"extraPorts"`
	MemMB      int       `json:"memMB"`
	Icon       string    `json:"icon"`
	Env        []EnvVar  `json:"env"`
	Dirs       []DataDir `json:"dirs"`
	Note       string    `json:"note,omitempty"`
	Heavy      bool      `json:"heavy,omitempty"`
	Cmd        []string  `json:"-"`
}

// Catálogo: solo aplicaciones de un contenedor y ligeras para un servidor
// de 8 GB. Las que necesitan varios contenedores se despliegan con Compose.
func Catalog() []CatalogApp {
	tz := serverTZ()
	return []CatalogApp{
		{ID: "vaultwarden", Label: "Vaultwarden", Desc: "Gestor de contraseñas compatible con Bitwarden", Image: "vaultwarden/server:latest", Port: 8081, CPort: 80, MemMB: 40, Icon: "lock",
			Env: []EnvVar{{K: "ADMIN_TOKEN", Secret: true}, {K: "SIGNUPS_ALLOWED", V: "false"}}, Dirs: []DataDir{{"data", "/data"}},
			Note: "Las apps de Bitwarden exigen HTTPS: publícalo con Tailscale."},
		{ID: "adguard", Label: "AdGuard Home", Desc: "Bloquea anuncios y rastreadores en toda la red", Image: "adguard/adguardhome:latest", Port: 3000, CPort: 3000, ExtraPorts: []string{"53→53/udp", "53→53/tcp"}, MemMB: 60, Icon: "shield",
			Env: []EnvVar{}, Dirs: []DataDir{{"work", "/opt/adguardhome/work"}, {"conf", "/opt/adguardhome/conf"}},
			Note: "Para filtrar toda la red, el router tiene que usar este servidor como DNS. Ese cambio se hace en el router."},
		{ID: "ntfy", Label: "ntfy", Desc: "Notificaciones en el iPhone desde tu servidor", Image: "binwiederhier/ntfy:latest", Port: 8082, CPort: 80, MemMB: 20, Icon: "bell",
			Env: []EnvVar{{K: "TZ", V: tz}, {K: "NTFY_BASE_URL", V: "http://localhost"}}, Dirs: []DataDir{{"cache", "/var/cache/ntfy"}},
			Cmd: []string{"serve"}},
		{ID: "jellyfin", Label: "Jellyfin", Desc: "Películas, series y música en tus dispositivos", Image: "jellyfin/jellyfin:latest", Port: 8096, CPort: 8096, MemMB: 350, Icon: "tv",
			Env: []EnvVar{{K: "TZ", V: tz}}, Dirs: []DataDir{{"config", "/config"}, {"cache", "/cache"}}},
		{ID: "homeassistant", Label: "Home Assistant", Desc: "Domótica: controla y automatiza los dispositivos del hogar", Image: "ghcr.io/home-assistant/home-assistant:stable", Port: 8123, CPort: 8123, MemMB: 420, Icon: "home",
			Env: []EnvVar{{K: "TZ", V: tz}}, Dirs: []DataDir{{"config", "/config"}}, Heavy: true,
			Note: "Su interfaz web se abre en el puerto 8123 tras el primer arranque."},
	}
}

func serverTZ() string {
	if b, err := os.ReadFile("/etc/timezone"); err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b))
	}
	if l, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(l, "zoneinfo/"); i >= 0 {
			return l[i+9:]
		}
	}
	return "UTC"
}

type DeploySpec struct {
	Source     string    `json:"source"`
	App        string    `json:"app"`
	Image      string    `json:"image"`
	Compose    string    `json:"compose"`
	Name       string    `json:"name"`
	Port       int       `json:"port"`
	CPort      int       `json:"cport"`
	ExtraPorts []string  `json:"extraPorts"`
	Access     string    `json:"access"`
	Restart    string    `json:"restart"`
	MemLimitMB *int      `json:"memLimitMB"`
	Dirs       []DataDir `json:"dirs"`
}

var (
	nameRe  = regexp.MustCompile(`^[a-z][a-z0-9-]{1,29}$`)
	imageRe = regexp.MustCompile(`^[a-z0-9]+([._/:-][a-z0-9]+)*(:[A-Za-z0-9._-]+)?(@sha256:[a-f0-9]{64})?$`)
	dirRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,30}$`)
	extraRe = regexp.MustCompile(`^(\d+)→(\d+)/(tcp|udp)$`)
)

func ValidImage(s string) bool { return imageRe.MatchString(strings.TrimSpace(s)) }

// Deploy valida la petición y lanza el despliegue como un trabajo.
func (a *App) Deploy(user string, s DeploySpec) (*Job, error) {
	cfg := a.Cfg.Get()
	if !nameRe.MatchString(s.Name) {
		return nil, errors.New("Usa de 2 a 30 caracteres: minúsculas, números y guiones, empezando por una letra.")
	}
	a.mu.RLock()
	for _, c := range a.docker.Containers {
		if c.Name == s.Name {
			a.mu.RUnlock()
			return nil, errors.New("Ya hay un contenedor llamado " + s.Name + ".")
		}
	}
	dockerOK := a.docker.Available
	a.mu.RUnlock()
	if !dockerOK {
		return nil, errors.New("Docker no está disponible en el servidor.")
	}
	if s.Source == "compose" {
		return a.deployCompose(user, s, cfg)
	}
	used := a.usedPorts()
	if s.Port < 1024 || s.Port > 65535 {
		return nil, errors.New("Elige un puerto entre 1024 y 65535.")
	}
	if who, ok := used[s.Port]; ok {
		return nil, fmt.Errorf("El puerto %d ya lo usa %s.", s.Port, who)
	}
	var env []EnvVar
	var cmd []string
	dirs := s.Dirs
	extra := s.ExtraPorts
	switch s.Source {
	case "catalog":
		var app *CatalogApp
		for _, c := range Catalog() {
			if c.ID == s.App {
				c := c
				app = &c
			}
		}
		if app == nil {
			return nil, errors.New("Esa aplicación no está en el catálogo.")
		}
		s.Image, s.CPort, env, dirs, extra, cmd = app.Image, app.CPort, app.Env, app.Dirs, app.ExtraPorts, app.Cmd
	case "image":
		if !ValidImage(s.Image) {
			return nil, errors.New("Formato de imagen no válido. Usa nombre:etiqueta, por ejemplo, nginx:1.27.")
		}
	default:
		return nil, errors.New("Origen de despliegue desconocido.")
	}
	if s.CPort < 1 || s.CPort > 65535 {
		return nil, errors.New("El puerto del contenedor debe estar entre 1 y 65535.")
	}
	ports := map[string]string{fmt.Sprintf("%d/tcp", s.CPort): fmt.Sprint(s.Port)}
	for _, p := range extra {
		m := extraRe.FindStringSubmatch(p)
		if m == nil {
			return nil, errors.New("Puerto adicional no válido: " + p)
		}
		var hp int
		fmt.Sscan(m[1], &hp)
		if who, ok := used[hp]; ok {
			return nil, fmt.Errorf("El puerto %d ya lo usa %s.", hp, who)
		}
		ports[m[2]+"/"+m[3]] = m[1]
	}
	for _, d := range dirs {
		if !dirRe.MatchString(d.Name) || !strings.HasPrefix(d.Path, "/") || strings.Contains(d.Path, "..") {
			return nil, errors.New("Carpeta de datos no válida: " + d.Name)
		}
	}
	if s.Access == "tailscale" {
		a.mu.RLock()
		ts := a.tailscale
		a.mu.RUnlock()
		if !ts.Running || ts.Self == nil {
			return nil, errors.New("Tailscale no está en marcha en el servidor: usa el puerto directo.")
		}
	}

	ctx := context.Background()
	_, _, localErr := a.Docker.ImageID(ctx, s.Image)
	first := "Descargando " + s.Image
	if localErr == nil {
		first = "Usando la imagen local " + s.Image
	}
	steps := []string{first}
	base := filepath.Join(cfg.DataRoot, s.Name)
	if len(dirs) > 0 {
		steps = append(steps, "Creando "+base)
	}
	steps = append(steps, "Creando el contenedor "+s.Name)
	if s.Access == "tailscale" {
		steps = append(steps, "Publicando con Tailscale (HTTPS)")
	}
	steps = append(steps, "Comprobando que responde")
	j := a.newJob("deploy", s.Name, steps...)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		i := 0
		next := func() { i++; j.step(i) }
		j.step(0)
		if localErr != nil {
			if err := a.Docker.Pull(ctx, s.Image, func(p int, _ string) { j.set(func(j *Job) { j.Progress = p * 6 / 10 }) }); err != nil {
				a.deployFailed(user, j, s, err)
				return
			}
		}
		var binds []string
		if len(dirs) > 0 {
			next()
			for _, d := range dirs {
				p := filepath.Join(base, d.Name)
				if err := os.MkdirAll(p, 0o750); err != nil {
					a.deployFailed(user, j, s, fmt.Errorf("no se pudo crear %s: %v", p, err))
					return
				}
				binds = append(binds, p+":"+d.Path)
			}
		}
		next()
		envList, err := a.resolveEnv(s.Name, env)
		if err != nil {
			a.deployFailed(user, j, s, err)
			return
		}
		url := fmt.Sprintf("http://%s:%d", a.serverIP(), s.Port)
		if s.Access == "tailscale" {
			a.mu.RLock()
			url = fmt.Sprintf("https://%s:%d", a.tailscale.Self.DNSName, s.Port)
			a.mu.RUnlock()
		}
		restart := "unless-stopped"
		if s.Restart == "never" {
			restart = "no"
		}
		var mem int64
		if s.MemLimitMB != nil && *s.MemLimitMB > 0 {
			mem = int64(*s.MemLimitMB) << 20
		}
		id, err := a.Docker.Create(ctx, CreateSpec{Name: s.Name, Image: s.Image, Env: envList, Ports: ports, Binds: binds, Restart: restart, MemBytes: mem, Cmd: cmd,
			Labels: map[string]string{"umbral.managed": "1", "umbral.url": url}})
		if err == nil {
			err = a.Docker.ContainerAction(ctx, id, "start")
		}
		if err != nil {
			a.deployFailed(user, j, s, err)
			return
		}
		if s.Access == "tailscale" {
			next()
			if out, err := exec.CommandContext(ctx, "tailscale", "serve", "--bg", fmt.Sprintf("--https=%d", s.Port), fmt.Sprintf("http://127.0.0.1:%d", s.Port)).CombinedOutput(); err != nil {
				a.deployFailed(user, j, s, fmt.Errorf("el contenedor está en marcha, pero Tailscale no lo publicó: %s", strings.TrimSpace(string(out))))
				return
			}
		}
		next()
		ok := false
		for t := 0; t < 30 && !ok; t++ {
			time.Sleep(time.Second)
			ok = Reachable("127.0.0.1", s.Port, time.Second)
		}
		a.Refresh("docker")
		msg := s.Name + " está en marcha y responde en " + url + "."
		if !ok {
			msg = s.Name + " está en marcha, pero aún no responde en el puerto " + fmt.Sprint(s.Port) + ". Algunas aplicaciones tardan en arrancar la primera vez."
		}
		a.Log(user, s.Name, "Despliegue", nil, s.Image+" en "+url)
		j.finish(msg, map[string]string{"url": url, "name": s.Name})
	}()
	return j, nil
}

func (a *App) deployFailed(user string, j *Job, s DeploySpec, err error) {
	a.Log(user, s.Name, "Despliegue", err, "")
	j.fail(err.Error())
	a.Refresh("docker")
}

// resolveEnv genera los secretos y los guarda en el servidor (0600). El
// navegador nunca los recibe.
func (a *App) resolveEnv(name string, env []EnvVar) ([]string, error) {
	var out, secrets []string
	for _, e := range env {
		if e.Secret {
			v := RandomSecret()
			out = append(out, e.K+"="+v)
			secrets = append(secrets, e.K+"="+v)
		} else {
			out = append(out, e.K+"="+e.V)
		}
	}
	if len(secrets) > 0 {
		p := filepath.Join(a.DataDir, "secrets", name+".env")
		if err := writeFileAtomic(p, []byte(strings.Join(secrets, "\n")+"\n"), 0o600); err != nil {
			return nil, errors.New("no se pudo guardar el secreto en el servidor: " + err.Error())
		}
	}
	return out, nil
}

/* ---------- Docker Compose ---------- */

type ComposeInfo struct {
	Services []string    `json:"services"`
	Images   []string    `json:"images"`
	Ports    [][2]string `json:"ports"`
}

var (
	composeImage = regexp.MustCompile(`(?m)^\s*image:\s*["']?([^"'\s#]+)`)
	composePort  = regexp.MustCompile(`(?m)^\s*-\s*["']?(?:[\d.]+:)?(\d+):(\d+)(?:/\w+)?["']?\s*$`)
	composeSvc   = regexp.MustCompile(`^ {2}([A-Za-z0-9_.-]+):\s*$`)
	composeTop   = regexp.MustCompile(`(?m)^services:\s*$`)
)

// ParseCompose hace la misma lectura ligera que el asistente del panel.
func ParseCompose(t string) (ComposeInfo, error) {
	var info ComposeInfo
	if !composeTop.MatchString(t) {
		return info, errors.New("Falta la sección services:. Un archivo docker-compose empieza normalmente por esa línea.")
	}
	for _, m := range composeImage.FindAllStringSubmatch(t, -1) {
		info.Images = append(info.Images, m[1])
	}
	if len(info.Images) == 0 {
		return info, errors.New("No se encontró ninguna imagen. Cada servicio necesita una línea image: (este asistente no construye imágenes con build:).")
	}
	for _, m := range composePort.FindAllStringSubmatch(t, -1) {
		info.Ports = append(info.Ports, [2]string{m[1], m[2]})
	}
	in := false
	for _, l := range strings.Split(t, "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "services:" && !strings.HasPrefix(l, " ") {
			in = true
			continue
		}
		if in && l != "" && l[0] != ' ' && l[0] != '#' {
			in = false
		}
		if m := composeSvc.FindStringSubmatch(l); in && m != nil {
			info.Services = append(info.Services, m[1])
		}
	}
	if len(info.Services) == 0 {
		info.Services = []string{"servicio"}
	}
	return info, nil
}

func (a *App) deployCompose(user string, s DeploySpec, cfg Config) (*Job, error) {
	info, err := ParseCompose(s.Compose)
	if err != nil {
		return nil, err
	}
	used := a.usedPorts()
	for _, p := range info.Ports {
		var n int
		fmt.Sscan(p[0], &n)
		if who, ok := used[n]; ok {
			return nil, fmt.Errorf("El puerto %d ya lo usa %s. Cámbialo en el archivo.", n, who)
		}
	}
	if exec.Command("docker", "compose", "version").Run() != nil {
		return nil, errors.New("Docker Compose no está instalado en el servidor (paquete docker-compose-plugin).")
	}
	dir := filepath.Join(cfg.DataRoot, s.Name)
	steps := []string{"Guardando el archivo en " + dir, "Descargando las imágenes y creando " + fmt.Sprint(len(info.Services)) + " contenedores", "Comprobando que responde"}
	j := a.newJob("deploy", s.Name, steps...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		j.step(0)
		file := filepath.Join(dir, "docker-compose.yml")
		if err := os.MkdirAll(dir, 0o750); err == nil {
			err = os.WriteFile(file, []byte(s.Compose), 0o640)
		}
		if err != nil {
			a.deployFailed(user, j, s, err)
			return
		}
		j.step(1)
		out, err := exec.CommandContext(ctx, "docker", "compose", "-p", s.Name, "-f", file, "up", "-d").CombinedOutput()
		if err != nil {
			a.deployFailed(user, j, s, fmt.Errorf("docker compose falló: %s", lastLines(string(out), 4)))
			return
		}
		j.step(2)
		url := ""
		if len(info.Ports) > 0 {
			var n int
			fmt.Sscan(info.Ports[0][0], &n)
			url = fmt.Sprintf("http://%s:%d", a.serverIP(), n)
			for t := 0; t < 30 && !Reachable("127.0.0.1", n, time.Second); t++ {
				time.Sleep(time.Second)
			}
		}
		a.Refresh("docker")
		a.Log(user, s.Name, "Despliegue (Compose)", nil, strings.Join(info.Images, ", "))
		j.finish(s.Name+" está en marcha.", map[string]string{"url": url, "name": s.Name})
	}()
	return j, nil
}

func lastLines(s string, n int) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, " ")
}
