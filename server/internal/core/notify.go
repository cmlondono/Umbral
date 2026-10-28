package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

/* Avisos al móvil con ntfy (https://ntfy.sh): sirve el servidor público o
 * uno propio. La configuración vive en secrets/notify.json porque el token
 * es un secreto: el panel solo sabe si existe. Otros canales (Telegram…)
 * serían otro Kind con su propio Send. */

type NotifyConfig struct {
	Kind   string `json:"kind"` // ntfy
	Server string `json:"server"`
	Topic  string `json:"topic"`
	Token  string `json:"token,omitempty"`
}

type NotifyView struct {
	Configured bool   `json:"configured"`
	Server     string `json:"server"`
	Topic      string `json:"topic"`
	HasToken   bool   `json:"hasToken"`
}

type Notifier struct {
	mu sync.Mutex
	c  NotifyConfig
	f  jsonFile[NotifyConfig]
	hc *http.Client
}

var reTopic = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

func newNotifier(dataDir string) *Notifier {
	n := &Notifier{hc: &http.Client{Timeout: 15 * time.Second}}
	n.f.path = filepath.Join(dataDir, "secrets", "notify.json")
	_ = n.f.Load(&n.c)
	return n
}

func (n *Notifier) View() NotifyView {
	n.mu.Lock()
	defer n.mu.Unlock()
	return NotifyView{Configured: n.c.Topic != "", Server: n.c.Server, Topic: n.c.Topic, HasToken: n.c.Token != ""}
}

func (n *Notifier) Configured() bool { return n.View().Configured }

// Update guarda la configuración. token nil conserva el actual; "" lo borra.
func (n *Notifier) Update(server, topic string, token *string) error {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	topic = strings.TrimSpace(topic)
	if topic == "" {
		n.mu.Lock()
		defer n.mu.Unlock()
		n.c = NotifyConfig{}
		return n.f.Save(n.c)
	}
	u, err := url.Parse(server)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" {
		return errors.New("El servidor debe ser una dirección como https://ntfy.sh.")
	}
	if !reTopic.MatchString(topic) {
		return errors.New("El tema debe tener de 6 a 64 letras, números, guiones o guiones bajos.")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	c := NotifyConfig{Kind: "ntfy", Server: server, Topic: topic, Token: n.c.Token}
	if token != nil {
		c.Token = strings.TrimSpace(*token)
	}
	n.c = c
	return n.f.Save(c)
}

// NewTopic propone un tema difícil de adivinar: en un servidor público,
// quien conoce el tema puede leer los avisos.
func NewTopic() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "umbral-" + hex.EncodeToString(b)
}

// Send publica un aviso. priority va de 1 (mínima) a 5 (urgente).
func (n *Notifier) Send(ctx context.Context, title, msg string, priority int, tags ...string) error {
	n.mu.Lock()
	c := n.c
	n.mu.Unlock()
	if c.Topic == "" {
		return errors.New("los avisos no están configurados (Ajustes › Avisos)")
	}
	if priority < 1 || priority > 5 {
		priority = 3
	}
	body, _ := json.Marshal(map[string]any{"topic": c.Topic, "title": title, "message": msg, "priority": priority, "tags": tags})
	req, _ := http.NewRequestWithContext(ctx, "POST", c.Server+"/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := n.hc.Do(req)
	if err != nil {
		return fmt.Errorf("no se pudo contactar con %s: %v", c.Server, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		var e struct{ Error string }
		_ = json.Unmarshal(b, &e)
		switch res.StatusCode {
		case 401, 403:
			return errors.New("el servidor de avisos rechazó el acceso: revisa el token")
		case 429:
			return errors.New("el servidor de avisos pide esperar: demasiados avisos seguidos")
		}
		if e.Error != "" {
			return errors.New("el servidor de avisos respondió: " + e.Error)
		}
		return errors.New("el servidor de avisos respondió " + res.Status)
	}
	return nil
}
