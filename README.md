# auth-center

![baner](/tools/baner-center.webp)

Сервер аутентификации. Принимает пользователя, проверяет личность через auth провайдер , выдаёт одноразовый код приложению.

Stateless — нет базы данных, нет хранения сессий между запросами.

## Переменные окружения

| Переменная | Обязательно | Описание |
|---|:---:|---|
| `PORT` | | Порт сервера (по умолчанию `8886`) |
| `BOT_TOKEN` | ★ | Токен Telegram-бота из [@BotFather](https://t.me/BotFather) |
| `BOT_USERNAME` | ★ | Username бота без `@` |
| `MINIAPP_SHORT_NAME` | ★ | Short name мини-аппа на том же боте (@BotFather → `/newapp`). Через него идёт весь Telegram-вход — см. auth-miniapp (отдельный репозиторий). Без него Telegram-ветке некуда вести |
| `APP_TOKENS` | ★ | Секреты приложений через запятую — кто может вызывать `/exchange` |
| `DIRECT_REDIRECT` | | Куда редиректить пользователя если он открыл auth-center напрямую без `?redirect=` |
| `MINIAPP_DIRECT_REDIRECT` | | То же самое, но для второй парадной двери: Telegram-пользователь, открывший мини-апп сам, без начатого где-либо входа. Не задан — `POST /miniapp/home` выключен, гость видит ошибку, как раньше |
| `GOOGLE_CLIENT_ID` | | Client ID из Google Cloud Console |
| `GOOGLE_CLIENT_SECRET` | | Client Secret из Google Cloud Console |
| `GOOGLE_CALLBACK_URL` | | Полный URL callback'а, должен совпадать с настройкой в Google Cloud (`https://your-domain/google/callback`) |

## Где получить токены

**Telegram**
1. [@BotFather](https://t.me/BotFather) → `/newbot` → скопировать `BOT_TOKEN`
2. Username бота → `BOT_USERNAME`
3. [@BotFather](https://t.me/BotFather) → `/newapp` на том же боте → указать домен auth-miniapp (отдельный репозиторий) и short name → `MINIAPP_SHORT_NAME`

Webhook регистрировать не нужно: личность приходит от мини-аппа, а не от бота. Исходящий доступ к Telegram auth-center тоже больше не требуется.

**Google**
1. [Google Cloud Console](https://console.cloud.google.com/) → APIs & Services → Credentials → Create OAuth 2.0 Client ID
2. Тип: Web application
3. Authorized redirect URIs: `https://your-auth-center-domain/google/callback`
4. Скопировать Client ID и Client Secret

**APP_TOKENS**
Придумать произвольные строки — по одной на каждое приложение, которое будет использовать auth-center. Те же строки прописать как `APP_TOKEN` в настройках каждого приложения.

## Локальная разработка

```bash
cp .env.example .env   # заполнить переменные
docker-compose -f dev-compose.yml up auth
```

Сервис доступен на `http://localhost:8886`. Горячая перезагрузка при изменении файлов в `build/`.

## Сборка продакшн-бинаря (linux/amd64)

```bash
docker-compose -f prod-compose.yml run --rm release
```

Бинарь окажется в `bin/auth-center`.

## Деплой

GitHub Actions → `Deploy` (ручной запуск). Одна кнопка выкатывает обе зоны — `ru` и `com`, каждую на свой self-hosted runner (`production-ru` / `production-com`) со своим environment. Шаг сборки на сервере не нужен: перед деплоем пересобрать и закоммитить `bin/auth-center`.

`bin/deploy.sh` ставит бинарь в `/opt/$APP`, пишет `/opt/$APP/$APP.env` и systemd unit, перезапускает сервис, проверяет `http://127.0.0.1:$PORT/` и при неудаче откатывается на прошлую версию (`/opt/$APP/last-deploy-backup`).

**Настройка GitHub:**

| Где | Что |
|---|---|
| repo variable | `APP=auth-center` |
| environment variables (`production-ru`, `production-com`) | `PORT`, `BOT_USERNAME`, `MINIAPP_SHORT_NAME`, `MINIAPP_DIRECT_REDIRECT`, `DIRECT_REDIRECT`, `GOOGLE_CLIENT_ID`, `GOOGLE_CALLBACK_URL` |
| environment secrets (`production-ru`, `production-com`) | `BOT_TOKEN`, `APP_TOKENS`, `GOOGLE_CLIENT_SECRET` |

Все перечисленные переменные обязательны — пустая роняет деплой до установки. `REGION` берётся из matrix.

## Локальная разработка приложения против прод auth-center

Если auth-center уже развёрнут в проде, а приложение разрабатывается локально — ничего дополнительно поднимать не нужно.

Схема работает потому, что редирект после аутентификации происходит в браузере разработчика, а не на сервере.

**Настройки приложения:**

```
AUTH_URL=https://your-auth-center-domain     # публичный URL прод auth-center
AUTH_INTERNAL=https://your-auth-center-domain  # тот же URL для /exchange с бэкенда
APP_URL=http://localhost:<порт>              # локальный адрес приложения
APP_TOKEN=local-dev-secret                   # произвольная строка
```

**Добавить токен в прод auth-center:**

В переменную `APP_TOKENS` на проде добавить `local-dev-secret` через запятую, перезапустить сервис.

## Подключение приложения

### 1. Отправить пользователя на auth-center

```
https://your-auth-center-domain/?redirect=https://yourapp.com/callback
```

### 2. Принять code

После аутентификации пользователь вернётся на:

```
https://yourapp.com/callback?code=<one-time-code>
```

### 3. Обменять code на данные пользователя

Только с бэкенда, не из браузера:

```http
POST https://your-auth-center-domain/exchange
Content-Type: application/json

{ "code": "<one-time-code>", "app_token": "<твой APP_TOKEN>" }
```

Ответ:

```json
{
  "ok": true,
  "method": "telegram",
  "user": { "id": 123456789, "first_name": "Ivan", "username": "ivan" }
}
```

```json
{
  "ok": true,
  "method": "solana",
  "user": { "id": "5ZX8wKF..." }
}
```

```json
{
  "ok": true,
  "method": "google",
  "user": { "id": "1170...", "email": "ivan@gmail.com", "name": "Ivan" }
}
```

`code` одноразовый, живёт 60 секунд. После успешного `/exchange` удаляется.

### 4. Создать сессию в своём приложении

Auth-center не помнит пользователя — это задача приложения. Сохранить `user.id` и `method` в сессию.

`user.id` — постоянный уникальный идентификатор.

## Cross-app login (delegate)

Залогиненный пользователь приложения A может открыть приложение B без повторной аутентификации. Приложение A запрашивает одноразовый код через `POST /delegate` и редиректит браузер на `https://<app-B>/?code=<code>`, где B обменивает код обычным `POST /exchange`.

Так как auth-center stateless и не хранит профиль, приложение A обязано переслать `method` + имя (`name` / `first_name` / `last_name`) в теле `/delegate` — иначе B покажет только id и «no name».

Полная спецификация для разработчиков: [`tools/delegate.md`](tools/delegate.md).

## Вход через Telegram Mini App

`POST /miniapp/auth` — основная дорога в Telegram-ветку (вторая, `POST /miniapp/home`, — ниже). Вызывает её auth-miniapp (отдельный репозиторий): мини-апп открывается внутри Telegram, проверяет подпись `initData` и сообщает сюда личность, называя сессию токеном, который приехал в `?startapp=`.

Дальше всё как обычно: сессия переходит в `authenticated`, вкладка забирает код на `/poll/{token}`, приложение меняет его на `/exchange`. `method` остаётся `telegram` — клиентские приложения разницы не видят. Связка с ботом и `POST /webhook` удалена.

```
POST /miniapp/auth
{"session_token": "<токен сессии>", "secret": "<общий секрет>", "user": {"id": 507717647, "first_name": "…", "last_name": "…", "username": "…"}}
```

Ответы: `200 {"ok": true, "redirect": "<адрес приложения>", "code": "<одноразовый код>"}` — из них мини-апп собирает кнопку возврата. Код здесь **второй**, отдельный от того, что лежит в сессии: тот ждёт вкладка на `/poll`, и одноразовый код, потраченный дважды, у кого-то не сработает. Дальше `403` секрет не сошёлся, `404` сессии нет или истекла, `409` сессия уже использована, `503` у auth-center не задан `BOT_TOKEN`.

### Парадная дверь: `POST /miniapp/home`

`/miniapp/auth` отвечает на вопрос «идёт вход, вот кто это». Есть второй вопрос: мини-апп открыли **сами** — из бота, по ссылке `t.me/<bot>/<app>`, через menu button, — и никакого входа нигде не начиналось. Раньше это заканчивалось ошибкой «в ссылке нет сессии входа», что довольно грубое приветствие для единственной точки входа, куда человек может забрести случайно.

```
POST /miniapp/home
{"secret": "<общий секрет>", "user": {"id": 507717647, "first_name": "…", "last_name": "…", "username": "…"}}
```

Ответы: `200 {"ok": true, "redirect": "<MINIAPP_DIRECT_REDIRECT>", "code": "<одноразовый код>"}`. Дальше `403` секрет не сошёлся, `503` не задан `BOT_TOKEN` **или** не задан `MINIAPP_DIRECT_REDIRECT`, `400` нет `user.id`.

Ни сессии, ни `/poll` здесь нет — привязывать нечего и ждать некому. Для принимающего приложения при этом ничего необычного: `method` остаётся `telegram`, маркера `via` нет, код гасится тем же `POST /exchange`. Это обычный телеграм-вход, просто начавшийся в Telegram, а не в браузере.

**Адрес назначения живёт здесь, а не в auth-miniapp** — и это не мелочь. Общий секрет позволяет мини-аппу утверждать личности, чью подпись он проверил, и ровно этим полномочия должны исчерпываться. Если бы он ещё и называл адрес, он мог бы направить действительный одноразовый код куда угодно. Назначения остаются решением auth-center — так же, как для любой сессии с `redirect`.

`MINIAPP_DIRECT_REDIRECT` — телеграмный близнец `DIRECT_REDIRECT`, который отвечает на тот же вопрос для браузера, пришедшего без `?redirect=`. Значения намеренно разные: двери разные, и вести они могут в разные приложения.

**Что сюда НЕ проваливается.** Пустой `session_token` в `/miniapp/auth` остался ошибкой `400` и не превращается в гостя: вызывающий, потерявший токен по дороге, — это баг, а баг, оканчивающийся успешным входом, худший из возможных. По той же причине испорченная или протухшая логин-ссылка на стороне мини-аппа продолжает честно сообщать, что сломалась, вместо тихой поездки в menu.

### Флаг источника в ссылке

QR и кнопка ведут на одну сессию и различаются одним символом перед токеном в `?startapp=`:

| Флаг | Откуда | Что из этого следует |
|---|---|---|
| `b` | нажата кнопка «open in telegram» | ждущий браузер — на этом же устройстве, мини-апп предлагает кнопку «ДАЛЕЕ» |
| `q` | отсканирован QR | сканировали телефоном, а ждёт браузер на другой машине — вести пользователя некуда, кнопки нет, мини-апп сам закрывается |

Флаг занимает ровно один символ и присутствует всегда, поэтому токен — это всё, что после первого символа. Префикс, который мог бы отсутствовать, читался бы неоднозначно: токены сессий это base64url и сами могут начинаться с любой буквы.

Для `q` auth-center не выписывает мини-аппу второй код — он всё равно некому тратить, а лишний живой одноразовый код держать незачем. Мини-апп по отсутствию кода и понимает, что кнопку показывать нечему, и закрывает себя сам.
