#!/bin/sh
# Instala o actualiza Umbral en Debian. Ejecútalo con sudo desde esta carpeta:
#   sudo sh instalar.sh
# No borra nada: tus datos en /var/lib/umbral se conservan al actualizar.
set -eu

case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Arquitectura no compatible: $(uname -m). Umbral funciona en amd64 y arm64."; exit 1 ;;
esac
BIN=./umbral-linux-$ARCH
[ -f "$BIN" ] || BIN=../dist/umbral-linux-$ARCH
[ -f "$BIN" ] || { echo "No encuentro umbral-linux-$ARCH junto a este script."; exit 1; }
[ "$(id -u)" = 0 ] || { echo "Ejecútalo con sudo."; exit 1; }

echo "1/6  Usuario del servicio"
if ! id umbral >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin umbral
fi
if getent group docker >/dev/null; then
  usermod -aG docker umbral
else
  echo "     Aviso: no existe el grupo docker. ¿Está instalado Docker?"
fi

echo "2/6  Binario en /usr/local/bin/umbral"
install -m 0755 "$BIN" /usr/local/bin/umbral

echo "3/6  Carpetas"
install -d -o umbral -g umbral -m 0700 /var/lib/umbral
install -d -o umbral -g umbral -m 0750 /srv/umbral

echo "4/6  Servicio systemd"
install -m 0644 ./umbral.service /etc/systemd/system/umbral.service
systemctl daemon-reload
systemctl enable umbral >/dev/null
systemctl restart umbral

echo "5/6  Ayudante de sistema (salud de los discos y actualizaciones de Debian)"
for u in umbral-sistema.service umbral-sistema.timer umbral-sistema.path; do
  install -m 0644 ./$u /etc/systemd/system/$u
done
systemctl daemon-reload
systemctl enable --now umbral-sistema.timer umbral-sistema.path >/dev/null
systemctl start --no-block umbral-sistema.service
command -v smartctl >/dev/null 2>&1 || echo "     Para ver la salud de los discos instala smartmontools: sudo apt install smartmontools"

echo "6/6  Tailscale (opcional, para publicar servicios con HTTPS)"
if command -v tailscale >/dev/null 2>&1; then
  tailscale set --operator=umbral 2>/dev/null && echo "     umbral puede usar tailscale serve." || echo "     No se pudo dar permiso a umbral en Tailscale; hazlo luego con: sudo tailscale set --operator=umbral"
else
  echo "     Tailscale no está instalado; se puede añadir después."
fi

if [ ! -s /var/lib/umbral/users.json ]; then
  echo
  echo "Falta crear tu usuario del panel:"
  echo "  sudo -u umbral umbral usuario crear TU-NOMBRE -datos /var/lib/umbral"
fi
IP=$(hostname -I | awk '{print $1}')
echo
echo "Listo. Abre http://$IP:8420 desde tu red local."
command -v tailscale >/dev/null 2>&1 && echo "Desde fuera de casa (con Tailscale): http://$(tailscale status --json 2>/dev/null | sed -n 's/.*"DNSName": *"\([^"]*\)\.".*/\1/p' | head -1):8420"
echo "Registros del servicio: journalctl -u umbral -f"
