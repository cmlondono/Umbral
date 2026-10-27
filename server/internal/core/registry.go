package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// RemoteDigest pregunta al registro qué digest tiene ahora una etiqueta, con
// una petición HEAD: no descarga la imagen.
func RemoteDigest(ctx context.Context, ref string) (string, error) {
	host, path, tag := registryParts(ref)
	u := "https://" + host + "/v2/" + path + "/manifests/" + tag
	cl := &http.Client{Timeout: 10 * time.Second}
	head := func(token string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(ctx, "HEAD", u, nil)
		req.Header.Set("Accept", strings.Join([]string{
			"application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json",
			"application/vnd.docker.distribution.manifest.v2+json", "application/vnd.oci.image.manifest.v1+json"}, ", "))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return cl.Do(req)
	}
	res, err := head("")
	if err != nil {
		return "", err
	}
	res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		tok, err := registryToken(ctx, cl, res.Header.Get("WWW-Authenticate"))
		if err != nil {
			return "", err
		}
		if res, err = head(tok); err != nil {
			return "", err
		}
		res.Body.Close()
	}
	if res.StatusCode != http.StatusOK {
		return "", errors.New("el registro respondió " + res.Status)
	}
	dg := res.Header.Get("Docker-Content-Digest")
	if dg == "" {
		return "", errors.New("el registro no indicó el digest")
	}
	return dg, nil
}

var authParam = regexp.MustCompile(`(\w+)="([^"]*)"`)

func registryToken(ctx context.Context, cl *http.Client, hdr string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(hdr), "bearer ") {
		return "", errors.New("el registro pide un tipo de autenticación no compatible")
	}
	p := map[string]string{}
	for _, m := range authParam.FindAllStringSubmatch(hdr, -1) {
		p[m[1]] = m[2]
	}
	if p["realm"] == "" {
		return "", errors.New("el registro no indicó dónde pedir el token")
	}
	q := url.Values{}
	if p["service"] != "" {
		q.Set("service", p["service"])
	}
	if p["scope"] != "" {
		q.Set("scope", p["scope"])
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", p["realm"]+"?"+q.Encode(), nil)
	res, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var t struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&t); err != nil {
		return "", err
	}
	if t.Token == "" {
		t.Token = t.AccessToken
	}
	return t.Token, nil
}

// registryParts: nginx:1.27 → registry-1.docker.io, library/nginx, 1.27.
func registryParts(ref string) (host, path, tag string) {
	repo, tag := splitRef(ref)
	host = "registry-1.docker.io"
	if i := strings.Index(repo, "/"); i > 0 && (strings.ContainsAny(repo[:i], ".:") || repo[:i] == "localhost") {
		host, repo = repo[:i], repo[i+1:]
	}
	if host == "docker.io" {
		host = "registry-1.docker.io"
	}
	if host == "registry-1.docker.io" && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	return host, repo, tag
}

// digestMatches indica si alguno de los digest locales coincide con el remoto.
func digestMatches(local []string, remote string) bool {
	for _, d := range local {
		if strings.HasSuffix(d, "@"+remote) {
			return true
		}
	}
	return false
}
