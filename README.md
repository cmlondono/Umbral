# Umbral

Panel web para administrar tu servidor Linux doméstico y tu red desde el navegador, en el ordenador o en el móvil. Está en español y tiene un diseño inspirado en Apple.

![Inicio de Umbral](docs/capturas/inicio.jpg)

Es un único programa de unos 9 MB que gasta alrededor de 15 MB de RAM. No usa base de datos ni servicios extra: guarda su estado en archivos JSON en `/var/lib/umbral`. Lo que no exista en tu servidor (Nextcloud, Tailscale, sensor de temperatura…) simplemente no aparece.

## Qué hace

- **Docker**:
  - contenedores e imágenes;
  - un asistente de despliegue desde el catálogo, una imagen o Docker Compose;
  - actualizaciones con vuelta atrás. Las de Nextcloud son seguras: modo de mantenimiento y copia previa de la base de datos.
- **Servidor**: CPU, memoria, discos, red y temperatura. Se ven en vivo (cada 2 s) o con historial de 1 hora, 24 horas y 7 días.
- **Red**: los dispositivos conectados, con su nombre, fabricante y estado. Puedes ponerles nombre y seguir los que te interesen.
- **PC**: encenderlo con Wake-on-LAN y abrir el escritorio remoto (RDP, RustDesk o Moonlight).
- **Automatizaciones**: «cuando pase esto, si se cumple esto, haz esto». Se lanzan desde:
  - un horario;
  - un servicio que se cae;
  - un umbral de recursos;
  - la llegada o salida de un dispositivo;
  - un atajo del iPhone o una etiqueta NFC.
- **Seguridad**: solo accesible desde tu red local y Tailscale. Contraseña y verificación en dos pasos (TOTP) opcional.

## Capturas

| | |
|---|---|
| ![Servidor](docs/capturas/servidor.jpg) | ![Servicios](docs/capturas/servicios.jpg) |
| **Servidor**: métricas con lectura de cada punto | **Servicios**: contenedores, imágenes y actualizaciones |
| ![Asistente de despliegue](docs/capturas/despliegue.jpg) | ![Red](docs/capturas/red.jpg) |
| **Asistente de despliegue** | **Red**: dispositivos detectados |
| ![Automatizaciones](docs/capturas/automatizaciones.jpg) | ![Editor de automatizaciones](docs/capturas/editor-automatizacion.jpg) |
| **Automatizaciones** | **Editor de reglas** |
| ![Modo oscuro](docs/capturas/inicio-oscuro.jpg) | ![Inicio de sesión](docs/capturas/login.jpg) |
| **Modo oscuro** | **Inicio de sesión** |

<p>
  <img src="docs/capturas/movil-inicio.jpg" alt="Inicio en el móvil" width="260">
  <img src="docs/capturas/movil-servidor.jpg" alt="Servidor en el móvil, modo oscuro" width="260">
</p>

Las capturas usan el servidor simulado de desarrollo, con datos de ejemplo.

## Requisitos

- Linux con systemd (probado en Debian 12 y 13) y procesador amd64.
- Docker, si quieres gestionar contenedores.
- Opcional: [Tailscale](https://tailscale.com), para entrar desde fuera de casa.
- Para compilar: Node.js 20 o superior y Go 1.27 o superior. Puedes compilar en tu ordenador y copiar el binario al servidor.

## 1. Compilar

```sh
git clone <este-repositorio> umbral && cd umbral
npm install
npm run build                       # compila el panel en server/web/dist
cd server
go test ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ../dist/umbral-linux-amd64 .
```

El resultado es `dist/umbral-linux-amd64`, con el panel incluido.

## 2. Instalar en el servidor

Copia al servidor, a una misma carpeta, estos tres archivos:
- `dist/umbral-linux-amd64`;
- `deploy/instalar.sh`;
- `deploy/umbral.service`.

Por ejemplo, desde la carpeta del proyecto:

```sh
ssh USUARIO@SERVIDOR mkdir -p umbral
scp dist/umbral-linux-amd64 deploy/instalar.sh deploy/umbral.service USUARIO@SERVIDOR:umbral/
```

Después, en el servidor:

```sh
cd ~/umbral && sudo sh instalar.sh
```

El instalador:
- crea el usuario de sistema `umbral` y lo añade al grupo `docker`;
- copia el binario a `/usr/local/bin/umbral`;
- crea `/var/lib/umbral` (configuración y estado) y `/srv/umbral` (datos de lo que despliegues);
- instala y arranca el servicio `umbral`;
- si hay Tailscale, da permiso al usuario `umbral` para publicar servicios con HTTPS.

Crea tu usuario del panel. La contraseña debe tener al menos 10 caracteres:

```sh
sudo -u umbral umbral usuario crear TU-NOMBRE -datos /var/lib/umbral
```

Abre `http://IP-DEL-SERVIDOR:8420` desde tu red local.

## 3. Primeros pasos en el panel

1. **Ajustes › Seguridad y acceso**: activa la verificación en dos pasos.
2. **Ajustes › Dispositivos registrados**: elige tu PC entre los equipos detectados en la red para poder encenderlo.
3. **Acceso remoto**: elige tu herramienta de escritorio remoto.
4. **Red**: pon nombre a los dispositivos que no lo anuncian y añade a **Dispositivos** los que quieras seguir.
5. **Automatizaciones**: empieza con una plantilla.

## Recomendado: HTTPS con Tailscale

En la red local, el panel funciona por HTTP sin cifrar. Si usas Tailscale, publícalo con HTTPS y un certificado válido, sin consumo extra. Antes, activa **MagicDNS** y **HTTPS Certificates** en la consola de administración de Tailscale. Después:

```sh
sudo tailscale serve --bg --https=443 http://127.0.0.1:8420
```

Entra siempre por `https://<nombre-del-servidor>.<tu-red>.ts.net`, en casa y fuera.

## Dónde guarda los datos

| Ruta | Contenido |
|---|---|
| `/var/lib/umbral/` | Configuración, usuarios (contraseñas con hash), sesiones, historiales, métricas, dispositivos y automatizaciones, en archivos JSON con permisos `0600`. |
| `/var/lib/umbral/secrets/` | Secretos generados al desplegar servicios. |
| `/srv/umbral/` | Datos de los servicios desplegados desde el panel. |
| `/srv/umbral/copias/` | Copias de la base de datos de Nextcloud previas a cada actualización (se guardan las 5 últimas). |

Para hacer una copia de seguridad de Umbral basta con copiar `/var/lib/umbral`.

## Mantenimiento

| Para… | Orden |
|---|---|
| Ver los registros | `journalctl -u umbral -f` |
| Cambiar una contraseña | Desde **Ajustes**, o `sudo -u umbral umbral usuario crear NOMBRE -datos /var/lib/umbral` |
| Ver los usuarios | `sudo -u umbral umbral usuario lista -datos /var/lib/umbral` |
| Quitar el doble factor (móvil perdido) | `sudo -u umbral umbral usuario quitar-2fa NOMBRE -datos /var/lib/umbral` |
| Actualizar Umbral | Copia el binario nuevo junto a `instalar.sh` y vuelve a ejecutar `sudo sh instalar.sh`. Los datos se conservan. |
| Desinstalar | `sudo systemctl disable --now umbral && sudo rm /etc/systemd/system/umbral.service /usr/local/bin/umbral` (y, si quieres, `/var/lib/umbral`) |

La configuración avanzada está en `/var/lib/umbral/config.json`:
- `listen`: puerto, por defecto `:8420`.
- `allowNets`: redes que pueden abrir el panel.
- `dockerSocket`: ruta del socket de Docker.

Lo demás se configura desde el panel.

## Desarrollo

```sh
npm install
echo VITE_UMBRAL_PRUEBAS=1 > .env.development.local
npm run dev                         # http://localhost:5173, con un servidor simulado
```

- El servidor simulado acepta cualquier usuario con una contraseña de al menos 4 caracteres.
- Añade `?mantenimiento` o `?sinservidor` a la URL para probar esos estados.
- Solo existe en desarrollo: no entra en la compilación.

**Documentación:**
- `docs/arquitectura.md`: cómo funciona el servicio, qué lee y cada cuánto, y su modelo de seguridad.
- `docs/sistema-de-diseno.md`: el diseño.
