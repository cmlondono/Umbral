package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

/* Publicar servicios con HTTPS dentro de la red Tailscale («tailscale
 * serve»): Tailscale pone un certificado válido y solo llegan los equipos de
 * tu tailnet. Nunca se usa Funnel, que lo abriría a Internet. Umbral necesita
 * ser operador de Tailscale (instalar.sh lo configura). */

type ServeEntry struct {
	Port   int    `json:"port"`   // puerto HTTPS en el nombre .ts.net
	Target string `json:"target"` // http://127.0.0.1:8080
	URL    string `json:"url"`    // https://servidor.tailnet.ts.net:8080
	Local  int    `json:"local"`  // puerto local al que apunta (0 si no es local)
}

// parseServeStatus interpreta «tailscale serve status --json».
func parseServeStatus(b []byte) []ServeEntry {
	var raw struct {
		Web map[string]struct {
			Handlers map[string]struct{ Proxy string }
		}
	}
	out := []ServeEntry{}
	if json.Unmarshal(b, &raw) != nil {
		return out
	}
	for hostport, w := range raw.Web {
		host, p, err := net.SplitHostPort(hostport)
		if err != nil {
			continue
		}
		port, _ := strconv.Atoi(p)
		h, ok := w.Handlers["/"]
		if !ok || h.Proxy == "" {
			continue
		}
		e := ServeEntry{Port: port, Target: h.Proxy, URL: "https://" + host}
		if port != 443 {
			e.URL += ":" + p
		}
		if u, err := url.Parse(h.Proxy); err == nil && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") {
			e.Local, _ = strconv.Atoi(u.Port())
		} else if !strings.Contains(h.Proxy, "://") {
			// «tailscale serve 8080» guarda solo el puerto o host:puerto.
			if _, lp, err := net.SplitHostPort(h.Proxy); err == nil {
				e.Local, _ = strconv.Atoi(lp)
			} else {
				e.Local, _ = strconv.Atoi(h.Proxy)
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

func readServe(path string) ([]ServeEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "serve", "status", "--json").Output()
	if err != nil {
		return []ServeEntry{}, err
	}
	return parseServeStatus(out), nil
}

var reTSLink = regexp.MustCompile(`https://login\.tailscale\.com/\S+`)

// tsServe ejecuta «tailscale serve» y traduce los errores habituales.
func tsServe(args ...string) error {
	path, err := exec.LookPath("tailscale")
	if err != nil {
		return errors.New("Tailscale no está instalado en el servidor.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, append([]string{"serve"}, args...)...).CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if link := reTSLink.FindString(msg); link != "" && (err != nil || ctx.Err() != nil || strings.Contains(strings.ToLower(msg), "enable")) {
		return errors.New("Tu red Tailscale aún no permite HTTPS. Actívalo una vez abriendo " + link + " (o en la consola de Tailscale › DNS: MagicDNS y HTTPS Certificates) y vuelve a intentarlo.")
	}
	if ctx.Err() != nil {
		return errors.New("Tailscale no respondió a tiempo.")
	}
	if err != nil {
		low := strings.ToLower(msg)
		switch {
		case strings.Contains(low, "access denied"), strings.Contains(low, "permission"):
			return errors.New("Umbral no tiene permiso para configurar Tailscale. En el servidor ejecuta: sudo tailscale set --operator=umbral")
		case strings.Contains(low, "https") && strings.Contains(low, "not enabled"), strings.Contains(low, "certificates"):
			return errors.New("Tu red Tailscale aún no permite HTTPS. En la consola de Tailscale › DNS, activa MagicDNS y HTTPS Certificates.")
		}
		return errors.New("Tailscale respondió: " + lastLines(msg, 2))
	}
	return nil
}

// Publish publica un puerto local con HTTPS en la red Tailscale.
func (a *App) Publish(user string, httpsPort, local int, label string) error {
	if httpsPort < 1 || httpsPort > 65535 || local < 1 || local > 65535 {
		return errors.New("Puerto no válido.")
	}
	a.mu.RLock()
	ts := a.tailscale
	a.mu.RUnlock()
	if !ts.Running {
		return errors.New("Tailscale no está conectado en el servidor.")
	}
	for _, e := range ts.Serve {
		if e.Port == httpsPort && e.Local != local {
			return fmt.Errorf("El puerto HTTPS %d ya publica otra cosa (%s). Deja de publicarla antes.", httpsPort, e.Target)
		}
	}
	err := tsServe("--bg", fmt.Sprintf("--https=%d", httpsPort), fmt.Sprintf("http://127.0.0.1:%d", local))
	a.Log(user, label, "Publicado en Tailscale con HTTPS", err, fmt.Sprintf("puerto %d", httpsPort))
	a.Refresh("slow")
	return err
}

func (a *App) Unpublish(user string, httpsPort int, label string) error {
	err := tsServe(fmt.Sprintf("--https=%d", httpsPort), "off")
	a.Log(user, label, "Dejó de publicarse en Tailscale", err, fmt.Sprintf("puerto %d", httpsPort))
	a.Refresh("slow")
	return err
}
