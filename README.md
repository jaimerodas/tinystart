# TinyStart!

Ahora publicado en [start.pati.to](https://start.pati.to)!

Es la página de inicio de mi navegador: una barra de comandos y una reja de
tiles escogidos a mano, organizados en grupos repartidos en columnas. Antes
vivía dentro de [TinyLinks](https://links.pati.to), pero las dos cosas se
fueron separando en la práctica —en TinyLinks guardo un archivo, aquí guardo el
puñado de destinos que uso a diario— así que la saqué a su propia app, con su
propia base de datos y sus propios usuarios.

Es una aplicación en Go (1.26), casi pura biblioteca estándar: `net/http`,
`html/template`, `database/sql` y tres dependencias — el driver de SQLite en Go
puro, bcrypt y un parser de YAML. El frontend es Hotwire (Turbo + Stimulus)
tal cual, sin bundler. De DB usa SQLite, así que no necesita más que un server;
usa Kamal 2 para el deploy a DigitalOcean, en el mismo droplet que TinyLinks
pero como app aparte. En producción ocupa unos 15 MB de memoria.

Nació como app de Rails y se reescribió en Go en agosto de 2026, sobre la misma
base de datos y con la misma interfaz. Cómo y por qué está en
[`docs/go-rewrite-plan.md`](docs/go-rewrite-plan.md).

## Qué hace
- Muestra tus tiles en `/`, agrupados y repartidos en las columnas que quieras.
- Los editas en `/start/edit`: agregar, cambiar, borrar y reordenar grupos y
  links, arrastrando con el mouse o puro teclado.
- La reja es un solo stop de `Tab` con un highlight que se mueve: `↑↓←→` para
  navegar, `Space` para levantar un renglón y volverlo a soltar, `Enter` para
  editar, `Esc` para cancelar el movimiento.
- `⌥E` te lleva de la start page al editor y `⌥S` de regreso. `?` abre la lista
  completa de atajos.
- La barra de comandos filtra tus tiles al instante y, si tienes una conexión
  configurada, le pregunta también a la otra app y te muestra esos resultados en
  una segunda sección.
- Tema (claro / oscuro / el del sistema) y color de acento, en Settings.
- Exporta e importa la página como un YAML chiquito que se puede editar a mano
  (`docs/start-page-format.md`), en Settings → Import & Export.

El primer usuario que se registra queda como admin y aprobado automáticamente;
los demás necesitan que un admin los apruebe desde Settings.

## Instalación
Con Go 1.26 instalado (`go.mod` fija la versión exacta del toolchain y `go` la
baja solo si hace falta):
```sh
TINYSTART_SECRET_KEY=$(openssl rand -hex 32) go run ./cmd/tinystart
```
y ya está en http://localhost:3000. La base de datos es
`storage/development.sqlite3` por default: si no existe la crea con el esquema
completo, y el primer registro queda como admin.

Todo se configura por variables de entorno:

| Variable | Qué es | Default |
|---|---|---|
| `TINYSTART_SECRET_KEY` | firma las cookies y los links de recuperar contraseña; obligatoria, ≥ 32 bytes | — |
| `TINYSTART_DB` | ruta del archivo SQLite | `storage/development.sqlite3` |
| `TINYSTART_ADDR` | dónde escucha | `:3000` (la imagen usa `:80`) |
| `TINYSTART_HOST` | URL base para los links en correos, p. ej. `https://start.pati.to` | la del request |
| `TINYSTART_ENV` | `production` prende cookies `Secure` y HSTS | — |
| `POSTMARK_API_TOKEN` | para mandar correo; sin él, el correo se escribe en el log | — |
| `POSTMARK_API_URL` | otro endpoint de Postmark, para pruebas | la API real |

Sin `POSTMARK_API_TOKEN` los correos no se mandan: se escriben en el log del
server, link de recuperar contraseña incluido. Es la manera de verlo sin
configurar nada; en producción sí se mandan, por Postmark.

Si te quedas fuera de tu cuenta, `tinystart set-password <correo>` lee una
contraseña nueva de stdin y la fija:
```sh
go run ./cmd/tinystart set-password jaime@example.com
```

## Conexiones
La búsqueda federada es el único punto de contacto con otra app. La conexión se
hace desde **Settings → Conexiones**, que arranca el device flow de la otra app:
le das Connect, apruebas en la otra pestaña, y la página se recarga sola cuando
el token llega.

Dos cosas que importan:

- **El token vive en la base de datos de TinyStart**, no en la configuración.
  Por eso rotarlo nunca necesita un deploy, y se renueva solo con el uso: nada
  más expira después de 90 días sin usarse.
- **Las conexiones son por usuario.** Un token da acceso a exactamente una
  cuenta de la otra app, así que cada quien conecta la suya. No hay ni debe
  haber una conexión global de la app.

La llamada va del lado del server: el navegador le pega a `/search.json` de
TinyStart y el server le pega a la otra app con el bearer token. No hay token en
el navegador ni CORS. Si la otra app no contesta, la búsqueda local sigue
funcionando igual; si el token dejó de servir, la start page te avisa para que
reconectes.

## Cómo está organizado
```
cmd/tinystart/        main: configuración por env, abrir y migrar la DB, servir, apagar limpio
internal/store/       el único paquete que sabe SQL (database/sql sobre SQLite)
internal/startpage/   el formato YAML de import/export, sin DB ni HTTP
internal/tinylinks/   el cliente de la otra app: device flow, búsqueda, visitas
internal/postmark/    mandar correo
internal/web/         rutas, handlers, plantillas, cookies, auth y los assets embebidos
```
`internal/web/routes.go` lista cada URL que contesta la app, en una sola función.

## Tests
```sh
./script/test
```
Es la puerta: gofmt, `go vet`, staticcheck, govulncheck, `go test -race` y,
si hay Chrome en la máquina, los tests de navegador (chromedp) que manejan el
editor de verdad: teclado, arrastrar, la barra de comandos. Corre en menos de
un minuto. GitHub Actions corre exactamente lo mismo (`.github/workflows/ci.yml`).

## Deploy
La primera vez que haces deploy tienes que hacer
```sh
kamal setup
```

y todas las demás veces haces
```sh
kamal deploy
```

Guardo los secretos en 1Password y Kamal se encarga de sacarlos de ahí al
momento de hacer deploy (ve `.kamal/secrets`): `TINYSTART_SECRET_KEY` y
`POSTMARK_API_TOKEN`. La imagen es un binario estático sobre Debian slim, con
`sqlite3` para el respaldo semanal a B2 (`bin/backup_db`).

Comparte servidor con TinyLinks —kamal-proxy reparte el :443 por Host header—
pero es otro servicio, otra imagen y **otro volumen** (`tinystart_storage`,
montado en `/data`). Eso último no es opcional: un volumen es almacenamiento
del host, independiente de la imagen, así que reusar el de TinyLinks pondría a
las dos apps sobre el mismo `production.sqlite3`.

## Management
Tiene algunos aliases de kamal para la gestión diaria:
```sh
kamal logs
kamal shell
kamal dbc                            # sqlite3 sobre la base de producción
kamal set-password jaime@example.com # lo que antes era `kamal console`
```
