# tracing

**Легковесная, чистая и готовая к использованию в продакшене библиотека трассировки OpenTelemetry для Go.**

Этот пакет предоставляет простую, продуманную обертку над OpenTelemetry (OTel), которая упрощает распределенную трассировку в сервисах Go — без лишнего шаблонного кода для наиболее распространенных сценариев использования.

Включает в себя:
- Настройка экспортера HTTP OTLP с разумными значениями по умолчанию
- Автоматическая отправка метаданных сервиса (имя, версия, окружение, идентификатор экземпляра)
- HTTP-промежуточное ПО с именами спанов по маршрутам
- Современная трассировка gRPC-клиента и сервера с использованием рекомендуемого API `StatsHandler`
- Богатый набор вспомогательных атрибутов (`TraceAny`, `TraceValue`, `Error`), работающих со структурами, картами, JSON и пользовательскими типами

Идеально подходит для микросервисов, API и любого приложения Go, которому нужны чистые, наблюдаемые трассировки без борьбы с SDK OTel.

---

## Особенности

- **Простая инициализация** с помощью функциональных параметров
- **Нулевые настройки по умолчанию** (localhost:4318, gzip, разумные атрибуты ресурсов)
- **Настройки для высокой нагрузки**: доля сэмплирования, настройка batch-процессора, TLS, заголовки авторизации, лимит размера атрибутов
- **HTTP-промежуточное ПО с ограниченной кардинальностью** — имена спанов по шаблонам маршрутов, без состояния на каждый путь
- **Современная поддержка gRPC** (сервер и клиент) с использованием текущего API `StatsHandler`
- **OTLP по HTTP или gRPC**, стандартные переменные `OTEL_*`, автоопределение ресурса (хост/процесс/контейнер)
- **Полная передача контекста**: входящие/исходящие HTTP и gRPC, W3C TraceContext + Baggage
- **Корреляция с логами** через `slog.Handler`, добавляющий `trace_id` / `span_id`
- **Удобная инъекция атрибутов** из структур с тегами `trace` и пользовательскими префиксами
- **Корректное завершение работы** вспомогательная функция
- **Крошечный и идиоматический** — никакой магии, просто чистый Go
---

## Установка

Требуется **Go 1.27.1+**.

```bash
go get github.com/jwm1rr0rb10/go-tracing
```

## Быстрый старт
### 1. Инициализация трассировки


```go
package main

import (
	"context"
	"log"

	"github.com/jwm1rr0rb10/go-tracing"
)

func main() {
	tp, err := tracing.New(
		tracing.WithServiceName("my-awesome-service"),
		tracing.WithServiceVersion("v1.2.3"),
		tracing.WithEnvName("production"),
		tracing.WithHost("otel-collector"),
		tracing.WithPort("4318"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer tracing.Shutdown(context.Background(), tp)

	// Your app code here...
}
```

---

## 2. Начальные участки

```go
func handleRequest(ctx context.Context, req MyRequest) error {
    ctx, span := tracing.Start(ctx, "handleRequest")
    defer span.End()

    tracing.TraceAny(ctx, "request", req) // automatically adds all fields

    // ... your logic
    return nil
}
```

---

## Конфигурация
Вся конфигурация выполняется с помощью функциональных параметров, передаваемых в `tracing.New():`

| Option                  | Description            | Default   |
|:------------------------|:-----------------------|:----------|
| WithHost(host)          | OTLP collector host    | localhost |
| WithPort(port)          | OTLP collector port    | 4318 (HTTP) / 4317 (gRPC) |
| WithServiceName(name)   | Service name           | (empty)   |
| WithServiceVersion(ver) | Service version        | (empty)   |
| WithServiceID(id)       | Unique instance ID     | (empty)   |
| WithEnvName(env)        | Deployment environment | (empty)   |
| WithSampleRatio(r)                  | Доля сэмплируемых новых трейсов (0..1), решение родителя соблюдается | 1.0 (ParentBased) |
| WithSampler(s)                      | Свой sampler (приоритетнее ratio) | — |
| WithTLS(cfg)                        | Включить TLS для экспортёра | insecure |
| WithHeaders(map)                    | Заголовки запросов экспорта (авторизация) | — |
| WithExportTimeout(d)                | Таймаут запроса экспорта | 10s |
| WithCompression(bool)               | Gzip-сжатие экспорта | true |
| WithBatchOptions(opts...)           | Настройка batch-процессора (очередь, размер батча, таймаут) | SDK defaults |
| WithAttributeValueLengthLimit(n)    | Макс. длина строковых атрибутов (<0 — без лимита) | 4096 |
| WithProtocol(p)                     | `ProtocolHTTP` или `ProtocolGRPC` | HTTP (или `OTEL_EXPORTER_OTLP_PROTOCOL`) |
| WithResourceAttributes(kv...)       | Доп. атрибуты ресурса (команда, регион, ...) | — |
| WithPropagator(p)                   | Заменить пропагатор (например, добавить B3 для legacy-сервисов) | TraceContext + Baggage |
| WithExporter(e)                     | Свой экспортёр вместо OTLP (stdout, in-memory для тестов) | OTLP |
| WithSpanProcessor(sp)               | Дополнительный span processor | — |
| WithErrorHandler(fn)                | Получать внутренние ошибки OTel (неудачный экспорт, потерянные спаны) | стандартный логгер |

Пример с полной конфигурацией:

```go
tp, err := tracing.New(
	tracing.WithHost("collector.prod.example.com"),
	tracing.WithServiceName("payments-api"),
	tracing.WithServiceVersion("2.4.1"),
	tracing.WithEnvName("production"),
	tracing.WithServiceID(os.Getenv("POD_NAME")),
)
```

### Переменные окружения

Поддерживаются стандартные переменные OpenTelemetry, явные опции имеют приоритет:

| Переменная | Когда используется |
|:--|:--|
| `OTEL_EXPORTER_OTLP_(TRACES_)ENDPOINT` | не задан ни `WithHost`, ни `WithPort` |
| `OTEL_EXPORTER_OTLP_(TRACES_)PROTOCOL` | не задан `WithProtocol` |
| `OTEL_EXPORTER_OTLP_(TRACES_)HEADERS` | не задан `WithHeaders` |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | не задан ни `WithSampler`, ни `WithSampleRatio` |
| `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES` | соответствующая опция пустая |
| `OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT`, `OTEL_SPAN_*_COUNT_LIMIT` | не задан `WithAttributeValueLengthLimit` |

Ресурс также содержит автоматически определённые `host.*`, `os.type`, `process.pid`, `process.runtime.*` и `container.id`.

### Рекомендации для высокой нагрузки

```go
tp, err := tracing.New(
	tracing.WithServiceName("payments-api"),
	tracing.WithSampleRatio(0.05), // сэмплировать 5% новых трейсов
	tracing.WithBatchOptions(
		sdktrace.WithMaxQueueSize(8192),
		sdktrace.WithMaxExportBatchSize(1024),
	),
)
```

- По умолчанию сэмплируется **100% трейсов** — под нагрузкой задайте `WithSampleRatio`.
- При переполнении очереди спаны отбрасываются (без блокировки) — подбирайте размер очереди под пиковый RPS.

---

## HTTP Middleware

```go
mux := http.NewServeMux()
mux.HandleFunc("/api/users", handler)

http.ListenAndServe(":8080", tracing.Middleware(mux))
```

- Имя Span = шаблон маршрута `http.ServeMux` (например, `GET /users/{id}`), либо только HTTP-метод, если маршрут не найден — сырые пути не используются, поэтому кардинальность спанов и память ограничены
- Автоматическое распространение контекста трассировки (W3C TraceContext + Baggage)
- Для других роутеров передайте свой форматтер: `tracing.NewMiddleware(otelhttp.WithSpanNameFormatter(f))`
- Паники помечают спан как ошибку и пробрасываются дальше
- Исключение шумных эндпоинтов: `tracing.NewMiddleware(tracing.WithSkipPaths("/healthz", "/metrics"))`
- Для эндпоинтов, открытых в интернет, добавьте `otelhttp.WithPublicEndpoint()`: недоверенный входящий `traceparent` тогда начинает новый трейс (со ссылкой на входящий), а не навязывает решение о сэмплировании

### HTTP-клиент

```go
client := &http.Client{Transport: tracing.NewTransport(nil)} // nil = http.DefaultTransport
```
Исходящие запросы получают клиентские спаны и передают контекст трассировки.


---

## gRPC Interceptors
### Server

```go
server := grpc.NewServer(tracing.WithServerTracing())

// or using the convenience function:
server := grpc.NewServer(tracing.WithAllTracing()...)
```

### Client
```go
conn, err := grpc.NewClient(
target,
tracing.WithClientTracing(),
// other options...
)
```
```text
Примечание: В этом пакете используется текущий рекомендуемый API otelgrpc.NewServerHandler / otelgrpc.NewClientHandler (старые функции Unary*Interceptor были удалены из официального пакета contrib).```
```

---

## Attribute Helpers
### `TraceAny` – the star of the show
```go
type User struct {
    ID    int    `trace:"id"`
    Name  string `trace:"name"`
    Email string `trace:"-"` // skipped
    _     struct{} `trace:"user"` // custom prefix
}

func doSomething(ctx context.Context, u User) {
    tracing.TraceAny(ctx, "", u) // attributes: user.id, user.name
}
```

- Работает со структурами (только с экспортируемыми полями)
- Поддерживает `trace:"-"` для пропуска полей
- Поддерживает `trace:"custom_name"` для переименования
- Анонимное поле `_` с тегом `trace` устанавливает пользовательский префикс
- В случае карт/срезов/массивов/структур используется JSON
- Реализует интерфейс `Attributed` для пользовательских типов

---

## Other helpers
```go
tracing.TraceValue(ctx, "key", value)           // single value
tracing.Error(ctx, err)                         // record error + set status
attrs := tracing.AttributesFrom("prefix", obj)  // get []attribute.KeyValue
```

---

## Корреляция с логами

```go
slog.SetDefault(slog.New(tracing.NewSlogHandler(slog.NewJSONHandler(os.Stdout, nil))))

slog.InfoContext(ctx, "payment processed") // добавит trace_id и span_id
```

`tracing.TraceIDFromContext(ctx)` / `tracing.SpanIDFromContext(ctx)` возвращают идентификаторы для других логгеров или заголовков ответа.

---

## Тестирование

```go
exp := tracetest.NewInMemoryExporter()
tp, _ := tracing.New(tracing.WithExporter(exp))
// ... тестируемый код ...
_ = tracing.ForceFlush(ctx, tp)
spans := exp.GetSpans()
```

---

## Shutdown
```go
defer tracing.Shutdown(context.Background(), tp)
```

Корректно завершает работу поставщика трассировки и выгружает оставшиеся спаны. Используйте контекст с таймаутом, чтобы завершение не зависло.
`tracing.ForceFlush(ctx, tp)` выгружает накопленные спаны без остановки (например, в serverless-обработчиках).

---

## Расширенные возможности
### Создание пролетов вручную

```go
ctx, span := tracing.Start(ctx, "expensive-operation", trace.WithAttributes(...))
defer span.End()
```

### Продолжить существующую трассировку
```go
ctx, childSpan := tracing.Continue(ctx, "sub-operation")
```

---

## License

[MIT License](https://github.com/jwm1rr0rb10/go-tracing/blob/main/LICENSE) – © Raman Zaitsau [@jwm1rrr0rb10](https://github.com/jwm1rr0rb10)


---

## Contributing

Приветствуются запросы на добавление изменений (pull requests)! Не стесняйтесь создавать запросы на исправление ошибок (tags) или предложения по улучшению функционала (finally feature requests).

---

Создано с ❤️ для чистых и наглядных сервисов Go.

