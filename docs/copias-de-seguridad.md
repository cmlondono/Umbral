# Copias de seguridad de Nextcloud

Guía para dejar Nextcloud protegido con copias en un disco externo y para restaurarlo si algún día hace falta. Puedes hacerla cuando quieras: hasta que elijas una carpeta externa, Umbral solo guarda copias de la base de datos en el propio servidor.

## Qué se copia y cómo

| Qué | Dónde | Cuántas |
|---|---|---|
| Base de datos de Nextcloud | `<datos>/copias` en el servidor (por defecto `/srv/umbral/copias`) | Las 5 últimas |
| Las mismas copias de la base de datos | `<carpeta externa>` | Las 10 últimas |
| Archivos de los usuarios, `config`, `custom_apps` y `themes` | `<carpeta externa>/nextcloud-archivos/<fecha>` | Las 7 últimas (configurable: 3, 7, 14 o 30) |

**Base de datos:** se copia sola antes de cada actualización de Nextcloud. Además, se puede copiar desde la ficha del contenedor, con una automatización o como parte de la copia completa.

**Copia completa** (base de datos y archivos):
- Cada copia es una carpeta completa que se puede restaurar por sí sola.
- Los archivos que no cambiaron desde la copia anterior no se duplican: son enlaces duros a la anterior. Por eso la primera copia tarda (lo copia todo) y las siguientes tardan minutos y ocupan solo lo nuevo.
- No se copian las miniaturas de las fotos, porque Nextcloud las vuelve a generar.

**Cómo funciona por dentro:**
- La copia la hace un contenedor temporal con la propia imagen de Nextcloud. Monta los volúmenes de Nextcloud en solo lectura y la carpeta externa, no tiene red y se borra al terminar.
- Nextcloud sigue funcionando mientras copia, salvo que actives «Mantenimiento durante la copia».

## Requisitos del disco externo

- Formato **ext4**. Con exFAT o NTFS, que es como suelen venir de fábrica, cada copia ocuparía todo de nuevo. Umbral lo comprueba antes de copiar y te avisa.
- Montado en `/mnt` o `/media`, porque el servicio solo puede escribir ahí.
- Un disco **distinto** del que tiene los datos de Nextcloud: si es el mismo, no protege de un fallo del disco. Umbral se niega a copiar en ese caso.
- Espacio: algo más de lo que ocupen los datos de Nextcloud. Las copias siguientes solo suman lo que cambie. Umbral muestra el espacio libre.

## 1. Preparar el disco (una sola vez)

> ⚠️ Formatear borra todo lo que tenga el disco. Si tiene algo que quieras conservar, cópialo antes.

1. Conecta el disco al servidor e identifícalo por su tamaño y modelo:

   ```sh
   lsblk -o NAME,SIZE,MODEL,FSTYPE,MOUNTPOINT
   ```

   Anota su nombre, por ejemplo `sdb`. **Asegúrate de que no es el disco del sistema** (el que tiene montado `/`) ni el de los datos.

2. Créale una partición y dale formato ext4. Cambia `sdX` por el nombre que anotaste:

   ```sh
   sudo apt install parted
   sudo wipefs -a /dev/sdX
   sudo parted -s /dev/sdX mklabel gpt mkpart copias ext4 0% 100%
   sudo mkfs.ext4 -L copias /dev/sdX1
   ```

3. Haz que se monte solo en cada arranque. Con `nofail`, el servidor arranca igual aunque el disco esté desconectado:

   ```sh
   sudo mkdir -p /mnt/copias
   echo 'LABEL=copias /mnt/copias ext4 defaults,nofail,x-systemd.device-timeout=10s 0 2' | sudo tee -a /etc/fstab
   sudo systemctl daemon-reload
   sudo mount /mnt/copias
   ```

4. Crea la carpeta de Umbral y dale permiso:

   ```sh
   sudo mkdir -p /mnt/copias/umbral
   sudo chown umbral: /mnt/copias/umbral
   ```

5. Comprueba que está montado: `df -h /mnt/copias` debe mostrar el tamaño del disco externo.

## 2. Configurarlo en Umbral

1. Ve a **Servidor › Copias de seguridad › Disco externo**.
2. Escribe `/mnt/copias/umbral` y pulsa **Guardar carpeta**.
3. Umbral comprueba que existe, que puede escribir, que está en otro disco y cuánto espacio libre hay. Si algo falla, te dice qué orden ejecutar.
4. Elige cuántas copias guardar (por defecto 7) y si quieres el modo de mantenimiento durante la copia:
   - **Sin mantenimiento**, la opción por defecto: Nextcloud sigue disponible. Un archivo que alguien suba justo en ese momento puede quedar para la copia siguiente.
   - **Con mantenimiento**: la copia es exacta, pero nadie puede usar Nextcloud mientras dura.

## 3. Primera copia

1. Pulsa **Hacer copia completa ahora**. Verás los pasos:
   1. comprobar el disco;
   2. copia de la base de datos;
   3. llevarla al disco externo;
   4. copiar los archivos;
   5. guardar la instantánea.
2. La primera copia lo copia todo. Como referencia, con 250 GB en un disco duro USB 3 tarda entre una y dos horas; con USB 2, bastante más. Hazla cuando no haga falta reiniciar el servidor.
3. Al terminar aparece en la tabla con lo copiado y la duración. También queda en el historial de acciones.

## 4. Copia automática cada noche

1. Configura antes los avisos en **Ajustes › Avisos**, para enterarte si una copia falla.
2. En **Automatizaciones › Plantillas**, elige **«Copia nocturna de Nextcloud»**:
   - cada día a las 3:00 hace la copia completa (base de datos y archivos);
   - lleva marcada **«Avisarme al móvil si falla»**.
3. Ajusta la hora si quieres y guárdala.

## 5. Comprobar de vez en cuando

Una copia que nunca se ha comprobado no es una copia. Una vez al mes:

- Mira en **Servidor › Copias de seguridad** que la última copia sea de anoche y diga «Completa».
- Comprueba en el servidor que hay carpetas con fecha y que un archivo conocido está dentro:

  ```sh
  sudo ls /mnt/copias/umbral/nextcloud-archivos/
  sudo ls /mnt/copias/umbral/nextcloud-archivos/<fecha>/data/<usuario>/files/
  ```

## 6. Restaurar

Umbral no restaura solo, a propósito: un clic equivocado sobrescribiría los datos de todos los usuarios. Hazlo a mano, con calma, siguiendo estos pasos. En los ejemplos, `nextcloud` es el contenedor de la aplicación y `nextcloud-db` el de la base de datos; usa los nombres que veas en **Servicios**.

1. **Elige la copia.** Sus carpetas están en `/mnt/copias/umbral/nextcloud-archivos/` y se llaman por su fecha y hora (`AAAAMMDD-HHMMSS`). La base de datos de esa misma copia está en `/mnt/copias/umbral/` con un nombre como `nextcloud-db-AAAAMMDD-HHMMSS.sql.gz`, unos segundos antes que la carpeta.

2. **Pon Nextcloud en mantenimiento:**

   ```sh
   docker exec -u www-data nextcloud php occ maintenance:mode --on
   ```

3. **Localiza dónde guarda Nextcloud sus archivos en el servidor:**

   ```sh
   docker inspect nextcloud --format '{{range .Mounts}}{{.Destination}} -> {{.Source}}{{println}}{{end}}'
   ```

   Busca la línea de `/var/www/html`. La carpeta de datos suele ser `<esa ruta>/data`, salvo que la tengas en otro volumen.

4. **Devuelve los archivos y la configuración** desde la copia elegida. `--delete` deja la carpeta exactamente como estaba en la copia:

   ```sh
   C=/mnt/copias/umbral/nextcloud-archivos/AAAAMMDD-HHMMSS
   V=<ruta de /var/www/html del paso 3>
   sudo rsync -aH --numeric-ids --delete "$C/data/" "$V/data/"
   sudo rsync -aH --numeric-ids "$C/config/" "$V/config/"
   ```

5. **Restaura la base de datos** de esa misma copia:
   - MariaDB o MySQL:

     ```sh
     gunzip -c /mnt/copias/umbral/nextcloud-db-AAAAMMDD-HHMMSS.sql.gz | \
       docker exec -i nextcloud-db sh -c 'exec mariadb -u"${MARIADB_USER:-$MYSQL_USER}" -p"${MARIADB_PASSWORD:-$MYSQL_PASSWORD}" "${MARIADB_DATABASE:-$MYSQL_DATABASE}"'
     ```

   - PostgreSQL:

     ```sh
     gunzip -c /mnt/copias/umbral/nextcloud-db-AAAAMMDD-HHMMSS.sql.gz | docker exec -i nextcloud-db psql -U postgres
     ```

6. **Avisa a los clientes y vuelve a abrir Nextcloud:**

   ```sh
   docker exec -u www-data nextcloud php occ maintenance:data-fingerprint
   docker exec -u www-data nextcloud php occ files:scan --all
   docker exec -u www-data nextcloud php occ maintenance:mode --off
   ```

   `data-fingerprint` avisa a las apps del móvil y del ordenador de que los datos volvieron atrás, para que no borren ni dupliquen archivos al sincronizar.

7. **Comprueba** entrando en Nextcloud que los archivos y los usuarios están como en la fecha de la copia.

### Si el disco del servidor murió del todo

1. Instala Debian, Docker y tu `docker-compose.yml` de Nextcloud, con la **misma versión** de Nextcloud que tenías.
2. Arranca Nextcloud una vez para que cree sus volúmenes y detenlo.
3. Sigue los pasos 2 a 7 de arriba.
