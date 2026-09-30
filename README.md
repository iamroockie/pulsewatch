# Pulsewatch

Небольшой сервис мониторинга доступности сайтов на Go. Добавляете URL и задаёте интервал,
а Pulsewatch регулярно его проверяет и записывает статус, задержку и ошибки.

## Стек

Go, PostgreSQL, Redis, Prometheus, Grafana.

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

## Локальный запуск

```sh
make prepare
make docker-up
make migrate-up
make run-api
make run-worker
```

```sh
curl -X POST localhost:8080/monitors \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","interval_seconds":30,"timeout_ms":5000,"max_retries":2}'

curl 'localhost:8080/monitors/<id>/checks?limit=10'
curl 'localhost:8080/monitors/<id>/uptime'
```

Дашборд Grafana: http://localhost:3000/d/pulsewatch (без логина, для правки — `admin`/`admin`).
Prometheus: http://localhost:9090.

`make test` запускает unit-тесты, `make test-full` — ещё и тесты, которым нужен Docker.

## Статус

- [x] HTTP-сервер, конфиг, graceful shutdown
- [x] PostgreSQL и миграции
- [x] CRUD мониторов
- [x] HTTP-проверка
- [x] Планировщик и пул воркеров
- [x] Лимит на хост в Redis
- [x] Uptime и история проверок
- [x] Метрики Prometheus и дашборд Grafana
- [ ] Сквозные тесты
- [ ] Docker-образы и весь стек одной командой `docker compose up`
