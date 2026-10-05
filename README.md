# Pulsewatch

Небольшой сервис мониторинга доступности сайтов на Go. Добавляете URL и задаёте интервал,
а Pulsewatch регулярно его проверяет и записывает статус, задержку и ошибки.

## Стек

Go, PostgreSQL, Redis, Prometheus, Grafana, Docker Compose.

## Как устроено

Два бинарника из одной кодовой базы:

- `cmd/api` обслуживает HTTP API;
- `cmd/worker` запускает планировщик и пул проверок.

PostgreSQL хранит мониторы и историю проверок и одновременно служит очередью задач.
Воркеры забирают просроченные мониторы через `FOR UPDATE SKIP LOCKED` и берут каждый в аренду.
Если воркер упал, его мониторы вернутся в очередь, когда истечёт аренда.
Проверки старше 30 дней воркер удаляет раз в час.

Redis хранит короткоживущее состояние для координации. Пока это семафор на каждый хост:
все воркеры вместе ведут не больше пяти проверок одного хоста одновременно.
Если Redis недоступен, проверки продолжаются без этого лимита.

Оба бинарника отдают метрики Prometheus на `/metrics`: API на своём порту, воркер на отдельном
`METRICS_PORT`. По ним видно, сколько проверок идёт и сколько они длятся, насколько опаздывают,
сколько мониторов ждёт в очереди и хватает ли воркеров. Метрики описывают систему, а не мониторы:
uptime отдельного монитора отдаёт API.

## Запуск

```sh
make prepare
make docker-up
```

`make prepare` создаёт `.env` из `.env.example`. `make docker-up` собирает образы, применяет
миграции и поднимает весь стек: PostgreSQL, Redis, API, воркер, Prometheus и Grafana.

- API: http://localhost:8080
- дашборд Grafana: http://localhost:3000/d/pulsewatch (без логина, для правки — `admin`/`admin`)
- Prometheus: http://localhost:9090

```sh
curl -X POST localhost:8080/monitors \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","interval_seconds":30,"timeout_ms":5000,"max_retries":2}'

curl 'localhost:8080/monitors/<id>/checks?limit=10'
curl 'localhost:8080/monitors/<id>/uptime'
```

Воркеров можно запустить несколько. Проверки они не дублируют, а лимит на хост у них общий:

```sh
docker compose up -d --scale worker=3
```

`make docker-down` останавливает стек и удаляет его данные.

## API

| Запрос                        | Что делает                                                    |
| ----------------------------- | ------------------------------------------------------------- |
| `POST /monitors`              | создаёт монитор                                               |
| `GET /monitors`               | список мониторов, параметры `limit` и `after`                 |
| `GET /monitors/{id}`          | один монитор                                                  |
| `PATCH /monitors/{id}`        | меняет настройки; `is_active` ставит на паузу и снимает с неё |
| `DELETE /monitors/{id}`       | удаляет монитор вместе с историей                             |
| `GET /monitors/{id}/checks`   | история проверок, новые первыми; параметры `limit` и `before` |
| `GET /monitors/{id}/uptime`   | доля успешных проверок за час, сутки и неделю                 |
| `GET /healthz`, `GET /readyz` | пробы                                                         |
| `GET /metrics`                | метрики Prometheus                                            |

Интервал — от 10 секунд до суток, таймаут — от 100 мс до минуты и меньше интервала,
повторов — до пяти.

## Разработка

```sh
make docker-deps
make migrate-up
make run-api
make run-worker
```

Так в Docker работают только PostgreSQL и Redis, а API и воркер запускаются из исходников.
Prometheus и Grafana собирают метрики только с контейнеров, поэтому в этом режиме их нет.

`make test` запускает unit-тесты, `make test-full` — ещё и тесты, которым нужен Docker.
`make lint` запускает линтер.
