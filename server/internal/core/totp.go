package core

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

/* Doble factor con códigos de 6 dígitos (TOTP, RFC 6238): el estándar que
 * usan la app Contraseñas del iPhone, Google Authenticator, 1Password, etc.
 * El secreto se guarda en users.json (0600) junto al hash de la contraseña;
 * los códigos de recuperación, solo como hash. */

var ErrNeedCode = errors.New("Escribe el código de tu app de autenticación.")

const totpStep = 30

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func totpCode(secret []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	h := m.Sum(nil)
	off := h[len(h)-1] & 0x0f
	v := binary.BigEndian.Uint32(h[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// totpCheck acepta el código del intervalo actual y de los vecinos (±30 s,
// por si el reloj del móvil va un poco desfasado). Devuelve el contador usado
// para impedir que el mismo código sirva dos veces.
func totpCheck(secretB32, code string, last int64, now time.Time) (int64, bool) {
	secret, err := b32.DecodeString(strings.ToUpper(secretB32))
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if err != nil || len(code) != 6 {
		return 0, false
	}
	c := now.Unix() / totpStep
	for _, d := range []int64{0, -1, 1} {
		if n := c + d; n > last && subtle.ConstantTimeCompare([]byte(totpCode(secret, n)), []byte(code)) == 1 {
			return n, true
		}
	}
	return 0, false
}

func newRecoveryCodes() (plain, hashed []string) {
	const alpha = "abcdefghjkmnpqrstuvwxyz23456789" // sin 0/o ni 1/l, para no confundirlos
	for i := 0; i < 8; i++ {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		var sb strings.Builder
		for j, x := range b {
			if j == 4 {
				sb.WriteByte('-')
			}
			sb.WriteByte(alpha[int(x)%len(alpha)])
		}
		plain = append(plain, sb.String())
		hashed = append(hashed, hashToken(sb.String()))
	}
	return
}

type totpPending struct {
	secret  string
	expires time.Time
}

// TOTPEnabled indica si el usuario tiene activado el doble factor.
func (a *Auth) TOTPEnabled(user string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncLocked()
	return a.users[user].TOTP != ""
}

func (a *Auth) checkPassword(user, password string) bool {
	a.mu.Lock()
	a.syncLocked()
	rec, ok := a.users[user]
	a.mu.Unlock()
	if !ok {
		return false
	}
	salt, _ := hex.DecodeString(rec.Salt)
	h, _ := pbkdf2.Key(sha256.New, password, salt, rec.Iter, 32)
	want, _ := hex.DecodeString(rec.Hash)
	return subtle.ConstantTimeCompare(h, want) == 1
}

// TOTPBegin genera un secreto nuevo (pendiente hasta confirmarlo con un
// código) y la dirección otpauth:// para el QR.
func (a *Auth) TOTPBegin(user, password, issuer string) (secret, uri string, err error) {
	if !a.checkPassword(user, password) {
		return "", "", errors.New("La contraseña no es correcta.")
	}
	raw := make([]byte, 20)
	_, _ = rand.Read(raw)
	secret = b32.EncodeToString(raw)
	a.mu.Lock()
	a.pending[user] = totpPending{secret: secret, expires: time.Now().Add(10 * time.Minute)}
	a.mu.Unlock()
	label := url.PathEscape(issuer + ":" + user)
	q := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return secret, "otpauth://totp/" + label + "?" + q.Encode(), nil
}

// TOTPConfirm activa el doble factor si el código coincide con el secreto
// pendiente. Devuelve los códigos de recuperación, que solo se muestran ahora.
func (a *Auth) TOTPConfirm(user, code string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[user]
	if !ok || time.Now().After(p.expires) {
		return nil, errors.New("El código QR ha caducado. Empieza otra vez.")
	}
	n, ok := totpCheck(p.secret, code, 0, time.Now())
	if !ok {
		return nil, errors.New("El código no coincide. Comprueba que la hora del móvil sea automática y prueba con el siguiente.")
	}
	a.syncLocked()
	rec := a.users[user]
	plain, hashed := newRecoveryCodes()
	rec.TOTP, rec.TOTPLast, rec.Recovery = p.secret, n, hashed
	a.users[user] = rec
	delete(a.pending, user)
	a.killSessionsLocked(user, "")
	return plain, a.uf.Save(a.users)
}

// TOTPDisable quita el doble factor; pide la contraseña y un código válido.
func (a *Auth) TOTPDisable(user, password, code string) error {
	if !a.checkPassword(user, password) {
		return errors.New("La contraseña no es correcta.")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rec := a.users[user]
	if rec.TOTP == "" {
		return nil
	}
	if !a.useSecondFactorLocked(user, &rec, code) {
		return errors.New("El código no es correcto.")
	}
	rec.TOTP, rec.TOTPLast, rec.Recovery = "", 0, nil
	a.users[user] = rec
	return a.uf.Save(a.users)
}

// ResetTOTP lo quita sin código: solo desde el terminal del servidor, para
// quien haya perdido el móvil y los códigos de recuperación.
func (a *Auth) ResetTOTP(user string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.users[user]
	if !ok {
		return errors.New("no existe el usuario " + user)
	}
	rec.TOTP, rec.TOTPLast, rec.Recovery = "", 0, nil
	a.users[user] = rec
	return a.uf.Save(a.users)
}

// useSecondFactorLocked acepta un código de la app o uno de recuperación
// (que se gasta). Guarda el cambio si lo hay.
func (a *Auth) useSecondFactorLocked(user string, rec *userRecord, code string) bool {
	if n, ok := totpCheck(rec.TOTP, code, rec.TOTPLast, time.Now()); ok {
		rec.TOTPLast = n
		a.users[user] = *rec
		_ = a.uf.Save(a.users)
		return true
	}
	h := hashToken(strings.ToLower(strings.TrimSpace(code)))
	for i, r := range rec.Recovery {
		if subtle.ConstantTimeCompare([]byte(r), []byte(h)) == 1 {
			rec.Recovery = append(rec.Recovery[:i:i], rec.Recovery[i+1:]...)
			a.users[user] = *rec
			_ = a.uf.Save(a.users)
			return true
		}
	}
	return false
}

// RecoveryLeft dice cuántos códigos de recuperación quedan.
func (a *Auth) RecoveryLeft(user string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.users[user].Recovery)
}
