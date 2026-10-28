package core

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

/* Registro de accesos al panel: entradas, intentos fallidos, bloqueos y
 * cambios de seguridad. Se guardan los 300 más recientes en access.json. */

type AccessEvent struct {
	At    time.Time `json:"at"`
	User  string    `json:"user"`
	IP    string    `json:"ip"`
	Kind  string    `json:"kind"` // login | fail | locked | logout | password | totp-on | totp-off
	Agent string    `json:"agent"`
	New   bool      `json:"new"` // primera entrada correcta desde esta IP
}

type AccessLog struct {
	mu sync.Mutex
	ev []AccessEvent
	f  jsonFile[[]AccessEvent]
}

func newAccessLog(dataDir string) *AccessLog {
	l := &AccessLog{}
	l.f.path = filepath.Join(dataDir, "access.json")
	_ = l.f.Load(&l.ev)
	return l
}

func (l *AccessLog) List() []AccessEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]AccessEvent{}, l.ev...)
}

func (l *AccessLog) add(e AccessEvent) AccessEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.Kind == "login" {
		e.New = true
		for _, o := range l.ev {
			if o.Kind == "login" && o.IP == e.IP {
				e.New = false
				break
			}
		}
		// Sin entradas previas (instalación nueva) nada es «nuevo».
		if len(l.ev) == 0 {
			e.New = false
		}
	}
	l.ev = append([]AccessEvent{e}, l.ev...)
	if len(l.ev) > 300 {
		l.ev = l.ev[:300]
	}
	_ = l.f.Save(l.ev)
	return e
}

// Access anota un acceso y avisa a las automatizaciones que lo esperan.
func (a *App) Access(r *http.Request, user, kind string) {
	e := a.AccessLog.add(AccessEvent{At: time.Now(), User: user, IP: clientIP(r).String(), Kind: kind, Agent: shortAgent(r.UserAgent())})
	who := user
	if who == "" {
		who = "usuario desconocido"
	}
	switch {
	case kind == "locked":
		a.Engine.Event("access", "locked", "Bloqueo por intentos fallidos ("+who+" desde "+e.IP+")")
	case kind == "login" && e.New:
		a.Engine.Event("access", "newip", "Entrada de "+who+" desde un equipo nuevo ("+e.IP+", "+e.Agent+")")
		a.Engine.Event("access", "login", "Entrada de "+who+" desde "+e.IP)
	case kind == "login":
		a.Engine.Event("access", "login", "Entrada de "+who+" desde "+e.IP)
	}
}

// shortAgent resume el navegador y el sistema: «Safari en iPhone».
func shortAgent(ua string) string {
	os := ""
	switch {
	case strings.Contains(ua, "iPhone"):
		os = "iPhone"
	case strings.Contains(ua, "iPad"):
		os = "iPad"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Mac OS X"):
		os = "Mac"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}
	br := ""
	switch {
	case strings.Contains(ua, "Edg/"):
		br = "Edge"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS"):
		br = "Firefox"
	case strings.Contains(ua, "Chrome/"), strings.Contains(ua, "CriOS"):
		br = "Chrome"
	case strings.Contains(ua, "Safari/"):
		br = "Safari"
	case strings.HasPrefix(ua, "curl/"):
		return "curl"
	}
	switch {
	case br != "" && os != "":
		return br + " en " + os
	case br != "" || os != "":
		return br + os
	}
	return "Desconocido"
}
