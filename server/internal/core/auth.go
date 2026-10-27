package core

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	pbkdfIter       = 600_000
	maxFails        = 5
	lockFor         = 15 * time.Minute
	sessionShort    = 12 * time.Hour
	sessionRemember = 30 * 24 * time.Hour
)

var userRe = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,31}$`)

type userRecord struct {
	Salt     string   `json:"salt"`
	Hash     string   `json:"hash"`
	Iter     int      `json:"iter"`
	TOTP     string   `json:"totp,omitempty"`     // secreto del doble factor (base32)
	TOTPLast int64    `json:"totpLast,omitempty"` // último código usado, para que no sirva dos veces
	Recovery []string `json:"recovery,omitempty"` // hashes de los códigos de recuperación
}

type sessionRecord struct {
	User    string    `json:"user"`
	Expires time.Time `json:"expires"`
}

type failRecord struct {
	n     int
	until time.Time
}

// Auth guarda usuarios con PBKDF2-SHA256 y sesiones por token aleatorio.
// En disco solo hay el hash del token, nunca el token.
type Auth struct {
	mu       sync.Mutex
	users    map[string]userRecord
	sessions map[string]sessionRecord
	fails    map[string]*failRecord
	uf       jsonFile[map[string]userRecord]
	sf       jsonFile[map[string]sessionRecord]
	umod     time.Time // fecha de users.json la última vez que se leyó
	pending  map[string]totpPending
}

func NewAuth(dataDir string) (*Auth, error) {
	a := &Auth{users: map[string]userRecord{}, sessions: map[string]sessionRecord{}, fails: map[string]*failRecord{}, pending: map[string]totpPending{}}
	a.uf.path = filepath.Join(dataDir, "users.json")
	a.sf.path = filepath.Join(dataDir, "sessions.json")
	if err := a.uf.Load(&a.users); err != nil {
		return nil, err
	}
	_ = a.sf.Load(&a.sessions)
	if a.users == nil {
		a.users = map[string]userRecord{}
	}
	if st, err := os.Stat(a.uf.path); err == nil {
		a.umod = st.ModTime()
	}
	if a.sessions == nil {
		a.sessions = map[string]sessionRecord{}
	}
	return a, nil
}

// syncLocked vuelve a leer users.json si ha cambiado en disco, para que los
// usuarios creados o borrados por terminal valgan sin reiniciar el servicio.
func (a *Auth) syncLocked() {
	st, err := os.Stat(a.uf.path)
	if err != nil || st.ModTime().Equal(a.umod) {
		return
	}
	u := map[string]userRecord{}
	if a.uf.Load(&u) == nil {
		a.users, a.umod = u, st.ModTime()
	}
}

func (a *Auth) Configured() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncLocked()
	return len(a.users) > 0
}

func (a *Auth) SetPassword(user, password string) error {
	if !userRe.MatchString(user) {
		return errors.New("el usuario debe tener de 2 a 32 caracteres: minúsculas, números, punto, guion o guion bajo, empezando por una letra")
	}
	if len(password) < 10 {
		return errors.New("la contraseña debe tener al menos 10 caracteres")
	}
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	h, err := pbkdf2.Key(sha256.New, password, salt, pbkdfIter, 32)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncLocked()
	rec := a.users[user] // conserva el doble factor si lo tenía
	rec.Salt, rec.Hash, rec.Iter = hex.EncodeToString(salt), hex.EncodeToString(h), pbkdfIter
	a.users[user] = rec
	// Cambiar la contraseña cierra las sesiones abiertas de ese usuario.
	a.killSessionsLocked(user, "")
	return a.uf.Save(a.users)
}

func (a *Auth) killSessionsLocked(user, except string) {
	for k, s := range a.sessions {
		if s.User == user && k != except {
			delete(a.sessions, k)
		}
	}
	_ = a.sf.Save(a.sessions)
}

func (a *Auth) DeleteUser(user string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.users[user]; !ok {
		return errors.New("no existe el usuario " + user)
	}
	delete(a.users, user)
	for k, s := range a.sessions {
		if s.User == user {
			delete(a.sessions, k)
		}
	}
	_ = a.sf.Save(a.sessions)
	return a.uf.Save(a.users)
}

func (a *Auth) Users() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.users))
	for u := range a.users {
		out = append(out, u)
	}
	return out
}

var ErrLocked = errors.New("locked")

// Login devuelve el token de sesión. key identifica al cliente (IP) para el
// bloqueo tras varios intentos fallidos.
func (a *Auth) Login(key, user, password, code string, remember bool) (token string, ttl time.Duration, wait time.Duration, err error) {
	a.mu.Lock()
	f := a.fails[key]
	if f != nil && time.Now().Before(f.until) {
		a.mu.Unlock()
		return "", 0, time.Until(f.until), ErrLocked
	}
	a.syncLocked()
	rec, ok := a.users[user]
	a.mu.Unlock()

	// Se calcula el hash aunque el usuario no exista, para no revelar qué usuarios hay.
	salt, _ := hex.DecodeString(rec.Salt)
	iter := rec.Iter
	if !ok {
		salt, iter = []byte("umbral-sin-usuario"), pbkdfIter
	}
	h, _ := pbkdf2.Key(sha256.New, password, salt, iter, 32)
	want, _ := hex.DecodeString(rec.Hash)
	passOK := ok && subtle.ConstantTimeCompare(h, want) == 1
	codeOK := true
	if passOK && rec.TOTP != "" {
		if strings.TrimSpace(code) == "" {
			// La contraseña es buena: se pide el código sin contarlo como fallo.
			return "", 0, 0, ErrNeedCode
		}
		a.mu.Lock()
		codeOK = a.useSecondFactorLocked(user, &rec, code)
		a.mu.Unlock()
	}
	if !passOK || !codeOK {
		a.mu.Lock()
		if f == nil || time.Now().After(f.until) && f.n >= maxFails {
			f = &failRecord{}
			a.fails[key] = f
		}
		f.n++
		if f.n >= maxFails {
			f.until = time.Now().Add(lockFor)
		}
		left := maxFails - f.n
		a.mu.Unlock()
		if left <= 0 {
			return "", 0, lockFor, ErrLocked
		}
		if passOK {
			return "", 0, 0, errors.New("El código no es correcto.")
		}
		return "", 0, 0, errors.New("Usuario o contraseña incorrectos.")
	}
	a.mu.Lock()
	delete(a.fails, key)
	a.mu.Unlock()
	token, ttl = a.NewSession(user, remember)
	return token, ttl, 0, nil
}

// NewSession abre una sesión para un usuario ya identificado.
func (a *Auth) NewSession(user string, remember bool) (token string, ttl time.Duration) {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	token = base64.RawURLEncoding.EncodeToString(raw)
	ttl = sessionShort
	if remember {
		ttl = sessionRemember
	}
	a.mu.Lock()
	a.sessions[hashToken(token)] = sessionRecord{User: user, Expires: time.Now().Add(ttl)}
	a.pruneLocked()
	_ = a.sf.Save(a.sessions)
	a.mu.Unlock()
	return token, ttl
}

func (a *Auth) Check(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncLocked()
	s, ok := a.sessions[hashToken(token)]
	if !ok || time.Now().After(s.Expires) {
		return "", false
	}
	if _, exists := a.users[s.User]; !exists {
		return "", false
	}
	return s.User, true
}

func (a *Auth) Logout(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, hashToken(token))
	_ = a.sf.Save(a.sessions)
}

func (a *Auth) pruneLocked() {
	now := time.Now()
	for k, s := range a.sessions {
		if now.After(s.Expires) {
			delete(a.sessions, k)
		}
	}
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// RandomSecret genera un secreto para un despliegue. Se guarda en el servidor
// y nunca se envía al navegador.
func RandomSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// ChangePassword comprueba la contraseña actual antes de guardar la nueva.
// Cierra todas las sesiones del usuario, también las de otros dispositivos.
func (a *Auth) ChangePassword(user, current, next string) error {
	a.mu.Lock()
	a.syncLocked()
	rec, ok := a.users[user]
	a.mu.Unlock()
	if !ok {
		return errors.New("El usuario ya no existe.")
	}
	salt, _ := hex.DecodeString(rec.Salt)
	h, _ := pbkdf2.Key(sha256.New, current, salt, rec.Iter, 32)
	want, _ := hex.DecodeString(rec.Hash)
	if subtle.ConstantTimeCompare(h, want) != 1 {
		return errors.New("La contraseña actual no es correcta.")
	}
	if current == next {
		return errors.New("La contraseña nueva debe ser distinta de la actual.")
	}
	if err := a.SetPassword(user, next); err != nil {
		return errors.New("La contraseña nueva " + strings.TrimPrefix(err.Error(), "la contraseña ") + ".")
	}
	return nil
}
