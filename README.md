# TinyStart!

Ahora publicado en [start.pati.to](https://start.pati.to)!

Es la página de inicio de mi navegador: una barra de comandos y una reja de
tiles escogidos a mano, organizados en grupos repartidos en columnas. Antes
vivía dentro de [TinyLinks](https://links.pati.to), pero las dos cosas se
fueron separando en la práctica —en TinyLinks guardo un archivo, aquí guardo el
puñado de destinos que uso a diario— así que la saqué a su propia app, con su
propia base de datos y sus propios usuarios.

Es una aplicación de Rails 8.1 sobre Ruby 4.0.6, bien vainilla. De DB usa
SQLite, así que no necesita más que un server. Usa Kamal 2 para el deploy a
DigitalOcean, en el mismo droplet que TinyLinks pero como app aparte.

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

El primer usuario que se registra queda como admin y aprobado automáticamente;
los demás necesitan que un admin los apruebe desde Settings.

## Instalación
Asumiendo que tienes la versión correcta de Ruby instalada (ve `.tool-versions`),
puedes simplemente hacer:
```sh
bundle
bin/rails db:prepare
```
para instalarlo, y cuando necesites correr un server local haces
```sh
bin/dev
```

En desarrollo los correos no se mandan: `letter_opener` te los abre en una
pestaña del navegador. Es la manera de ver el link de recuperar contraseña sin
configurar nada. En producción sí se mandan, por Postmark.

## Conexiones
La búsqueda federada es el único punto de contacto con otra app. La conexión se
hace desde **Settings → Conexiones**, que arranca el device flow de la otra app:
le das Connect, apruebas en la otra pestaña, y la página se recarga sola cuando
el token llega.

Dos cosas que importan:

- **El token vive en la base de datos de TinyStart**, no en las credentials. Por
  eso rotarlo nunca necesita un deploy, y se renueva solo con el uso: nada más
  expira después de 90 días sin usarse.
- **Las conexiones son por usuario.** Un token da acceso a exactamente una
  cuenta de la otra app, así que cada quien conecta la suya. No hay ni debe
  haber una conexión global de la app.

La llamada va del lado del server: el navegador le pega a `/search.json` de
TinyStart y Rails le pega a la otra app con el bearer token. No hay token en el
navegador ni CORS. Si la otra app no contesta, la búsqueda local sigue
funcionando igual; si el token dejó de servir, la start page te avisa para que
reconectes.

## Tests
```sh
bin/rails test    # tests rápidos
./script/test     # todo: test:all + rubocop + brakeman
```

## Deploy
La primera vez que haces deploy tienes que hacer
```sh
kamal setup
```

y todas las demás veces haces
```sh
kamal deploy
```

Guardo las variables de entorno en 1Password y Kamal se encarga de sacarlas de
ahí al momento de hacer deploy (ve `.kamal/secrets`). Son nada más dos:
`RAILS_MASTER_KEY` y `POSTMARK_API_TOKEN`.

Comparte servidor con TinyLinks —kamal-proxy reparte el :443 por Host header—
pero es otro servicio, otra imagen y **otro volumen** (`tinystart_storage`).
Eso último no es opcional: un volumen es almacenamiento del host,
independiente de la imagen, así que reusar el de TinyLinks pondría a las dos
apps sobre el mismo `production.sqlite3`.

## Management
Tiene algunos aliases de kamal para la gestión diaria:
```sh
kamal console
kamal logs
kamal shell
kamal dbc
```
