package core

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Config es la configuración del panel. Vive en <dataDir>/config.json y se
// edita desde Ajustes. Nunca contiene secretos: las contraseñas están en
// users.json (con hash) y los secretos de los despliegues en secrets/.
type Config struct {
	Listen       string   `json:"listen"`
	DockerSocket string   `json:"dockerSocket"`
	AllowNets    []string `json:"allowNets"`

	ServerName  string       `json:"serverName"`
	PC          PCConfig     `json:"pc"`
	Nextcloud   NCConfig     `json:"nextcloud"`
	RemoteTool  *string      `json:"remoteTool"`
	SSHUser     string       `json:"sshUser"`
	DataRoot    string       `json:"dataRoot"`
	ScanMinutes int          `json:"scanMinutes"`
	Router      RouterConfig `json:"router"`
}

type PCConfig struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Broadcast string `json:"broadcast"`
	CheckPort int    `json:"checkPort"`
}

type NCConfig struct {
	URL       string `json:"url"`
	Container string `json:"container"`
}

type RouterConfig struct {
	IP    string `json:"ip"`
	Model string `json:"model"`
}

// PublicConfig es la parte que ve el navegador.
type PublicConfig struct {
	ServerName  string       `json:"serverName"`
	PC          PCConfig     `json:"pc"`
	Nextcloud   NCConfig     `json:"nextcloud"`
	RemoteTool  *string      `json:"remoteTool"`
	SSHUser     string       `json:"sshUser"`
	DataRoot    string       `json:"dataRoot"`
	ScanMinutes int          `json:"scanMinutes"`
	Router      RouterConfig `json:"router"`
}

func defaultConfig() Config {
	host, _ := os.Hostname()
	return Config{
		Listen:       ":8420",
		DockerSocket: "/var/run/docker.sock",
		// Red local, Tailscale y el propio equipo. Nada más puede abrir el panel.
		AllowNets:   []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "fd7a:115c:a1e0::/48"},
		ServerName:  host,
		PC:          PCConfig{CheckPort: 3389},
		DataRoot:    "/srv/umbral",
		ScanMinutes: 5,
	}
}

type ConfigStore struct {
	mu   sync.RWMutex
	path string
	cfg  Config
	nets []*net.IPNet
}

func LoadConfig(dataDir string) (*ConfigStore, error) {
	cs := &ConfigStore{path: filepath.Join(dataDir, "config.json"), cfg: defaultConfig()}
	b, err := os.ReadFile(cs.path)
	if errors.Is(err, os.ErrNotExist) {
		if err := cs.save(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if err := json.Unmarshal(b, &cs.cfg); err != nil {
		return nil, errors.New("config.json no es válido: " + err.Error())
	}
	if cs.cfg.PC.CheckPort == 0 {
		cs.cfg.PC.CheckPort = 3389
	}
	if cs.cfg.ScanMinutes < 1 {
		cs.cfg.ScanMinutes = 5
	}
	return cs, cs.parseNets()
}

func (cs *ConfigStore) parseNets() error {
	cs.nets = nil
	for _, s := range cs.cfg.AllowNets {
		_, n, err := net.ParseCIDR(strings.TrimSpace(s))
		if err != nil {
			return errors.New("allowNets contiene una red no válida: " + s)
		}
		cs.nets = append(cs.nets, n)
	}
	return nil
}

func (cs *ConfigStore) save() error {
	b, _ := json.MarshalIndent(cs.cfg, "", "  ")
	return writeFileAtomic(cs.path, b, 0o600)
}

func (cs *ConfigStore) Get() Config {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	c := cs.cfg
	c.AllowNets = append([]string(nil), cs.cfg.AllowNets...)
	return c
}

func (cs *ConfigStore) Public() PublicConfig {
	c := cs.Get()
	return PublicConfig{c.ServerName, c.PC, c.Nextcloud, c.RemoteTool, c.SSHUser, c.DataRoot, c.ScanMinutes, c.Router}
}

func (cs *ConfigStore) Allowed(ip net.IP) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	for _, n := range cs.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Update aplica los cambios editables desde Ajustes, tras validarlos.
func (cs *ConfigStore) Update(p PublicConfig) error {
	if p.PC.MAC != "" {
		if _, err := net.ParseMAC(p.PC.MAC); err != nil {
			return errors.New("La dirección MAC del PC no es válida. Usa el formato 3C:7C:3F:A1:52:0E.")
		}
	}
	for _, ip := range []string{p.PC.IP, p.PC.Broadcast, p.Router.IP} {
		if ip != "" && net.ParseIP(ip) == nil {
			return errors.New("La dirección IP " + ip + " no es válida.")
		}
	}
	if p.PC.CheckPort < 1 || p.PC.CheckPort > 65535 {
		return errors.New("El puerto de comprobación del PC debe estar entre 1 y 65535.")
	}
	if p.Nextcloud.URL != "" && !strings.HasPrefix(p.Nextcloud.URL, "http://") && !strings.HasPrefix(p.Nextcloud.URL, "https://") {
		return errors.New("La dirección de Nextcloud debe empezar por http:// o https://.")
	}
	if p.RemoteTool != nil {
		switch *p.RemoteTool {
		case "rdp", "rustdesk", "moonlight":
		default:
			return errors.New("Herramienta de escritorio remoto desconocida.")
		}
	}
	if p.ScanMinutes < 1 || p.ScanMinutes > 60 {
		return errors.New("El escaneo de red debe hacerse cada 1 a 60 minutos.")
	}
	if !filepath.IsAbs(p.DataRoot) {
		return errors.New("La carpeta de datos debe ser una ruta absoluta, como /srv/umbral.")
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	c := &cs.cfg
	c.ServerName, c.PC, c.Nextcloud, c.RemoteTool = strings.TrimSpace(p.ServerName), p.PC, p.Nextcloud, p.RemoteTool
	c.PC.MAC = strings.ToUpper(c.PC.MAC)
	c.SSHUser, c.DataRoot, c.ScanMinutes, c.Router = p.SSHUser, filepath.Clean(p.DataRoot), p.ScanMinutes, p.Router
	return cs.save()
}

// writeFileAtomic escribe en un archivo temporal y lo renombra, para que un
// corte de luz no deje un JSON a medias.
func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// jsonFile guarda un valor en un archivo JSON del directorio de datos.
type jsonFile[T any] struct {
	mu   sync.Mutex
	path string
}

func (f *jsonFile[T]) Load(v *T) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (f *jsonFile[T]) Save(v T) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFileAtomic(f.path, b, 0o600)
}
