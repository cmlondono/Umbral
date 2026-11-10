package core

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

/* Estado del PC.
 *
 * El PC se sigue por su MAC, no por su IP: muchos equipos de casa reciben la
 * IP por DHCP y cambia. La IP actual sale del último escaneo de la red (la
 * configurada en Ajustes solo se usa si el escaneo aún no lo ha visto).
 *
 * «Encendido» exige que el PC conteste por TCP y que la respuesta venga de su
 * MAC. Aparecer en la tabla ARP no basta: muchas tarjetas con Wake-on-LAN
 * siguen respondiendo a ARP con el PC apagado, y Linux recuerda las entradas
 * unos minutos. Tampoco basta con que «algo» conteste en esa IP: si el PC
 * cambió de IP, la antigua puede ser ahora de otro equipo. */

// Puertos que se prueban, además del configurado: SMB y RPC en Windows, SSH en Linux y Mac.
var pcExtraPorts = []int{445, 135, 22}

// IPOf devuelve la última IP en la que el escaneo vio una MAC.
func (s *Scanner) IPOf(mac string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.known[strings.ToUpper(mac)]; d != nil {
		return d.IP
	}
	return ""
}

// pcIP: la IP actual del PC según la red, o la configurada si aún no se ha visto.
func (a *App) pcIP(cfg PCConfig) string {
	if ip := a.Net.IPOf(cfg.MAC); ip != "" {
		return ip
	}
	return cfg.IP
}

// arpMAC busca en la tabla ARP del servidor qué MAC tiene ahora una IP.
func arpMAC(ip string) (string, bool) {
	entries := readARP()
	if entries == nil {
		return "", false // fuera de Linux no hay tabla que consultar
	}
	for _, e := range entries {
		if e.IP == ip {
			return e.MAC, true
		}
	}
	return "", true
}

// probePC prueba los puertos del PC en paralelo. Una conexión aceptada o
// rechazada demuestra que el sistema está en marcha; después se comprueba que
// quien contestó es el PC y no otro equipo que heredó su IP.
func probePC(ip, mac string, port int, timeout time.Duration) bool {
	if ip == "" || mac == "" {
		return false
	}
	ports := []int{port}
	for _, p := range pcExtraPorts {
		if p != port {
			ports = append(ports, p)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	answered := false
	for _, p := range ports {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, fmt.Sprint(p)), timeout)
			if err == nil {
				c.Close()
			}
			if err == nil || strings.Contains(err.Error(), "refused") {
				mu.Lock()
				answered = true
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	if !answered {
		return false
	}
	got, ok := arpMAC(ip)
	if !ok {
		return true
	}
	return strings.EqualFold(got, mac)
}

// followWake vigila un encendido: escanea la red cada 20 s durante 3 min para
// descubrir pronto la IP que el PC reciba y marcarlo como encendido.
func (a *App) followWake() {
	a.mu.Lock()
	if a.wakeFollow {
		a.mu.Unlock()
		return
	}
	a.wakeFollow = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.wakeFollow = false; a.mu.Unlock() }()
	for i := 0; i < 9; i++ {
		time.Sleep(20 * time.Second)
		_ = a.Net.Scan(a.Cfg.Get())
		a.refreshPC()
		a.mu.RLock()
		on := a.pc.State == "on"
		a.mu.RUnlock()
		if on {
			return
		}
	}
}

func (a *App) refreshPC() {
	cfg := a.Cfg.Get().PC
	st := PCState{Configured: cfg.MAC != "", Name: cfg.Name, MAC: cfg.MAC, State: "unknown"}
	if st.Configured {
		st.IP = a.pcIP(cfg)
		up := probePC(st.IP, cfg.MAC, cfg.CheckPort, 1200*time.Millisecond)
		a.mu.RLock()
		wakeAt, prev := a.wakeAt, a.pc
		a.mu.RUnlock()
		switch {
		case up:
			st.State = "on"
			now := time.Now()
			st.LastSeen = &now
		case !wakeAt.IsZero() && time.Since(wakeAt) < 3*time.Minute:
			st.State = "waking"
		default:
			st.State = "off"
		}
		if st.LastSeen == nil {
			st.LastSeen = prev.LastSeen
		}
		if !wakeAt.IsZero() {
			w := wakeAt
			st.LastWake = &w
		}
	}
	a.mu.Lock()
	a.pc = st
	a.mu.Unlock()
}

// pcOn vuelve a comprobar el PC y dice si está encendido.
func (a *App) pcOn() bool {
	a.refreshPC()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.pc.State == "on"
}
