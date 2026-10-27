package core

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

func ifaceIPv4(name string) (*net.IPNet, error) {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return nil, err
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return n, nil
		}
	}
	return nil, errors.New("la interfaz " + name + " no tiene IPv4")
}

func broadcastOf(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	b := make(net.IP, 4)
	for i := range b {
		b[i] = ip[i] | ^n.Mask[i]
	}
	return b
}

/* ---------- Wake-on-LAN ---------- */

func magicPacket(mac string) ([]byte, error) {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return nil, errors.New("La dirección MAC del PC no es válida.")
	}
	p := make([]byte, 0, 102)
	for i := 0; i < 6; i++ {
		p = append(p, 0xff)
	}
	for i := 0; i < 16; i++ {
		p = append(p, hw...)
	}
	return p, nil
}

// SendWOL envía el paquete mágico a la difusión de la red local (y a la
// configurada, si hay una).
func SendWOL(mac, broadcast string) error {
	p, err := magicPacket(mac)
	if err != nil {
		return err
	}
	targets := map[string]bool{"255.255.255.255": true}
	if broadcast != "" {
		targets[broadcast] = true
	}
	if iface, _ := defaultRoute(); iface != "" {
		if n, err := ifaceIPv4(iface); err == nil {
			targets[broadcastOf(n).String()] = true
		}
	}
	var lastErr error
	sent := 0
	for t := range targets {
		c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(t), Port: 9})
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := c.Write(p); err != nil {
			lastErr = err
		} else {
			sent++
		}
		c.Close()
	}
	if sent == 0 {
		return fmt.Errorf("no se pudo enviar el paquete de encendido: %v", lastErr)
	}
	return nil
}

// Reachable comprueba si un equipo acepta conexiones TCP en un puerto.
// Una conexión rechazada también demuestra que el equipo está encendido.
func Reachable(ip string, port int, timeout time.Duration) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, fmt.Sprint(port)), timeout)
	if err == nil {
		c.Close()
		return true
	}
	return strings.Contains(err.Error(), "refused")
}

/* ---------- Escaneo de la red local ---------- */

type NetDevice struct {
	IP         string    `json:"ip"`
	MAC        string    `json:"mac"`
	Name       string    `json:"name"`   // el que se muestra
	Custom     string    `json:"custom"` // el que pone el usuario en el panel
	Pinned     bool      `json:"pinned"` // aparece en Dispositivos
	Vendor     string    `json:"vendor"`
	Online     bool      `json:"online"`
	FirstSeen  time.Time `json:"firstSeen"`
	LastSeen   time.Time `json:"lastSeen"`
	Registered *string   `json:"registered"`
}

type NetworkState struct {
	Subnet   string      `json:"subnet"`
	Gateway  string      `json:"gateway"`
	Iface    string      `json:"iface"`
	LastScan *time.Time  `json:"lastScan"`
	Devices  []NetDevice `json:"devices"`
}

type Scanner struct {
	mu    sync.Mutex
	known map[string]*NetDevice // por MAC
	state NetworkState
	file  jsonFile[map[string]*NetDevice]
	oui   *ouiDB
}

func NewScanner(path, dataDir string) *Scanner {
	s := &Scanner{known: map[string]*NetDevice{}, oui: newOUI(dataDir)}
	s.file.path = path
	_ = s.file.Load(&s.known)
	if s.known == nil {
		s.known = map[string]*NetDevice{}
	}
	return s
}

// Scan provoca que el núcleo resuelva la MAC de cada dirección de la subred
// (un datagrama UDP vacío por dirección) y después lee la tabla ARP. No
// necesita permisos de administrador ni instalar nada.
func (s *Scanner) Scan(cfg Config) error {
	iface, gw := defaultRoute()
	if iface == "" {
		return errors.New("No se encontró la ruta por defecto: el escaneo solo funciona en el servidor Linux.")
	}
	n, err := ifaceIPv4(iface)
	if err != nil {
		return err
	}
	ones, bits := n.Mask.Size()
	if bits-ones > 10 {
		return fmt.Errorf("la red %s es demasiado grande para escanearla", n)
	}
	base := binary.BigEndian.Uint32(n.IP.To4().Mask(n.Mask))
	count := uint32(1) << (bits - ones)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 64)
	for i := uint32(1); i < count-1; i++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, base+i)
		if ip.Equal(n.IP) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ip net.IP) {
			defer wg.Done()
			defer func() { <-sem }()
			if c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip, Port: 9}); err == nil {
				_, _ = c.Write([]byte{0})
				c.Close()
			}
		}(ip)
	}
	wg.Wait()
	time.Sleep(3 * time.Second)

	now := time.Now()
	present := map[string]arpEntry{}
	for _, e := range readARP() {
		if e.Iface == iface {
			present[e.MAC] = e
		}
	}
	// El propio servidor no aparece en su tabla ARP.
	if ifc, err := net.InterfaceByName(iface); err == nil {
		present[strings.ToUpper(ifc.HardwareAddr.String())] = arpEntry{IP: n.IP.String(), MAC: strings.ToUpper(ifc.HardwareAddr.String()), Iface: iface}
	}

	names := lookupNames(present)
	// Los que el DNS del router no conoce: se pregunta a cada equipo.
	var ask []string
	var macs []string
	for mac, e := range present {
		if names[e.IP] == "" {
			ask = append(ask, e.IP)
		}
		if !isRandomMAC(mac) {
			macs = append(macs, mac)
		}
	}
	for ip, nm := range discoverNames(ask) {
		names[ip] = nm
	}
	s.oui.ensure()
	vendors := s.oui.lookup(macs)
	s.mu.Lock()
	defer s.mu.Unlock()
	for mac, e := range present {
		d := s.known[mac]
		if d == nil {
			d = &NetDevice{MAC: mac, FirstSeen: now}
			s.known[mac] = d
		}
		d.IP, d.LastSeen = e.IP, now
		if nm := names[e.IP]; nm != "" {
			d.Name = nm
		}
		if v := vendors[mac]; v != "" {
			d.Vendor = v
		}
	}
	// Olvida los equipos que no se ven desde hace 30 días.
	for mac, d := range s.known {
		if now.Sub(d.LastSeen) > 30*24*time.Hour {
			delete(s.known, mac)
		}
	}
	_ = s.file.Save(s.known)
	s.state = NetworkState{Subnet: n.IP.Mask(n.Mask).String() + fmt.Sprintf("/%d", ones), Iface: iface, LastScan: &now}
	if gw != nil {
		s.state.Gateway = gw.String()
	}
	s.rebuildLocked(cfg, present)
	return nil
}

func (s *Scanner) rebuildLocked(cfg Config, present map[string]arpEntry) {
	devs := make([]NetDevice, 0, len(s.known))
	srvMAC := ""
	if ifc, err := net.InterfaceByName(s.state.Iface); err == nil {
		srvMAC = strings.ToUpper(ifc.HardwareAddr.String())
	}
	for mac, d := range s.known {
		c := *d
		if d.Custom != "" {
			c.Name = d.Custom
		}
		_, c.Online = present[mac]
		if present == nil {
			c.Online = false
		}
		c.Registered = nil
		if mac == srvMAC {
			r := "server"
			c.Registered = &r
			if c.Name == "" {
				c.Name = cfg.ServerName
			}
		} else if cfg.PC.MAC != "" && strings.EqualFold(mac, cfg.PC.MAC) {
			r := "pc"
			c.Registered = &r
			if cfg.PC.Name != "" {
				c.Name = cfg.PC.Name
			}
		}
		if isRandomMAC(mac) && c.Vendor == "" {
			c.Vendor = "MAC privada (aleatoria)"
		}
		devs = append(devs, c)
	}
	sort.Slice(devs, func(i, j int) bool { return ipLess(devs[i].IP, devs[j].IP) })
	s.state.Devices = devs
}

// UpdateDevice guarda el nombre que el usuario pone a un dispositivo (vacío
// vuelve al detectado) y si debe aparecer en Dispositivos.
func (s *Scanner) UpdateDevice(cfg Config, mac, name string, pinned bool) error {
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 40 {
		return errors.New("El nombre puede tener como mucho 40 caracteres.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.known[strings.ToUpper(mac)]
	if d == nil {
		return errors.New("Ese dispositivo ya no está en la lista.")
	}
	d.Custom, d.Pinned = name, pinned
	_ = s.file.Save(s.known)
	present := map[string]arpEntry{}
	for _, x := range s.state.Devices {
		if x.Online {
			present[x.MAC] = arpEntry{}
		}
	}
	s.rebuildLocked(cfg, present)
	return nil
}

func (s *Scanner) State() NetworkState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	st.Devices = append([]NetDevice(nil), s.state.Devices...)
	return st
}

// Seen indica si una MAC respondió en el último escaneo.
func (s *Scanner) Seen(mac string) (bool, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.state.Devices {
		if strings.EqualFold(d.MAC, mac) {
			return d.Online, d.LastSeen
		}
	}
	return false, time.Time{}
}

func ipLess(a, b string) bool {
	x, y := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if x == nil || y == nil {
		return a < b
	}
	return binary.BigEndian.Uint32(x) < binary.BigEndian.Uint32(y)
}

func lookupNames(present map[string]arpEntry) map[string]string {
	out := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, e := range present {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			defer cancel()
			names, err := net.DefaultResolver.LookupAddr(ctx, ip)
			if err == nil && len(names) > 0 {
				n := strings.TrimSuffix(names[0], ".")
				n = strings.TrimSuffix(n, ".lan")
				n = strings.TrimSuffix(n, ".home")
				n = strings.TrimSuffix(n, ".local")
				mu.Lock()
				out[ip] = n
				mu.Unlock()
			}
		}(e.IP)
	}
	wg.Wait()
	return out
}

/* ---------- Tailscale ---------- */

type TailscalePeer struct {
	Name     string     `json:"name"`
	IP       string     `json:"ip"`
	OS       string     `json:"os"`
	Online   bool       `json:"online"`
	LastSeen *time.Time `json:"lastSeen"`
}

type TailscaleSelf struct {
	Name    string `json:"name"`
	IP      string `json:"ip"`
	DNSName string `json:"dnsName"`
}

type TailscaleState struct {
	Installed bool            `json:"installed"`
	Running   bool            `json:"running"`
	Error     *string         `json:"error"`
	Self      *TailscaleSelf  `json:"self"`
	Peers     []TailscalePeer `json:"peers"`
}

func ReadTailscale() TailscaleState {
	st := TailscaleState{Peers: []TailscalePeer{}}
	path, err := exec.LookPath("tailscale")
	if err != nil {
		return st
	}
	st.Installed = true
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "status", "--json").Output()
	if err != nil {
		e := "No se pudo leer el estado de Tailscale: " + err.Error()
		st.Error = &e
		return st
	}
	return parseTailscale(out, st)
}

func parseTailscale(out []byte, st TailscaleState) TailscaleState {
	var raw struct {
		BackendState string
		Self         *struct {
			HostName     string
			DNSName      string
			TailscaleIPs []string
		}
		Peer map[string]struct {
			HostName     string
			OS           string
			TailscaleIPs []string
			Online       bool
			LastSeen     time.Time
		}
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		e := "La respuesta de Tailscale no es válida."
		st.Error = &e
		return st
	}
	st.Running = raw.BackendState == "Running"
	if raw.Self != nil {
		ip := ""
		if len(raw.Self.TailscaleIPs) > 0 {
			ip = raw.Self.TailscaleIPs[0]
		}
		st.Self = &TailscaleSelf{Name: raw.Self.HostName, IP: ip, DNSName: strings.TrimSuffix(raw.Self.DNSName, ".")}
	}
	for _, p := range raw.Peer {
		tp := TailscalePeer{Name: p.HostName, OS: p.OS, Online: p.Online}
		if len(p.TailscaleIPs) > 0 {
			tp.IP = p.TailscaleIPs[0]
		}
		if !p.LastSeen.IsZero() && p.LastSeen.Year() > 2000 {
			t := p.LastSeen
			tp.LastSeen = &t
		}
		st.Peers = append(st.Peers, tp)
	}
	sort.Slice(st.Peers, func(i, j int) bool { return st.Peers[i].Name < st.Peers[j].Name })
	return st
}

/* ---------- Nextcloud ---------- */

type NextcloudState struct {
	Configured  bool    `json:"configured"`
	URL         string  `json:"url"`
	Container   string  `json:"container"`
	Reachable   bool    `json:"reachable"`
	HTTPStatus  *int    `json:"httpStatus"`
	Version     *string `json:"version"`
	Maintenance *bool   `json:"maintenance"`
	Error       *string `json:"error"`
}

func ReadNextcloud(url, container string) NextcloudState {
	st := NextcloudState{URL: url, Container: container, Configured: url != ""}
	if url == "" {
		return st
	}
	cl := &http.Client{Timeout: 5 * time.Second}
	res, err := cl.Get(strings.TrimRight(url, "/") + "/status.php")
	if err != nil {
		e := "No responde: " + err.Error()
		st.Error = &e
		return st
	}
	defer res.Body.Close()
	st.Reachable = true
	code := res.StatusCode
	st.HTTPStatus = &code
	var body struct {
		Maintenance   bool   `json:"maintenance"`
		VersionString string `json:"versionstring"`
	}
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if json.Unmarshal(b, &body) == nil && body.VersionString != "" {
		st.Version = &body.VersionString
		st.Maintenance = &body.Maintenance
	} else if code == 200 {
		e := "La respuesta de /status.php no es de Nextcloud."
		st.Error = &e
	}
	return st
}
