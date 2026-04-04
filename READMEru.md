# tracing

**Легковесная, чистая и готовая к использованию в продакшене библиотека трассировки OpenTelemetry для Go.**

Этот пакет предоставляет простую, продуманную обертку над OpenTelemetry (OTel), которая упрощает распределенную трассировку в сервисах Go — без лишнего шаблонного кода для наиболее распространенных сценариев использования.

Включает в себя:
- Настройка экспортера HTTP OTLP с разумными значениями по умолчанию
- Автоматическая отправка метаданных сервиса (имя, версия, окружение, идентификатор экземпляра)
- Высокопроизводительное HTTP-промежуточное ПО с кэшированием обработчиков
- Современная трассировка gRPC-клиента и сервера с использованием рекомендуемого API `StatsHandler`
- Богатый набор вспомогательных атрибутов (`TraceAny`, `TraceValue`, `Error`), работающих со структурами, картами, JSON и пользовательскими типами

Идеально подходит для микросервисов, API и любого приложения Go, которому нужны чистые, наблюдаемые трассировки без борьбы с SDK OTel.

---

## Особенности

- **Простая инициализация** с помощью функциональных параметров
- **Нулевые настройки по умолчанию** (localhost:4318, разумные атрибуты ресурсов)
- **Кэшируемое HTTP-промежуточное ПО** — отсутствие накладных расходов на каждый запрос после первого вызова
- **Современная поддержка gRPC** (сервер и клиент) с использованием текущего API `StatsHandler`
- **Удобная инъекция атрибутов** из структур с тегами `trace` и пользовательскими префиксами
- **Корректное завершение работы** вспомогательная функция
- **Крошечный и идиоматический** — никакой магии, просто чистый Go
---

## Установка

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
| WithPort(port)          | OTLP collector port    | 4318      |
| WithServiceName(name)   | Service name           | (empty)   |
| WithServiceVersion(ver) | Service version        | (empty)   |
| WithServiceID(id)       | Unique instance ID     | (empty)   |
| WithEnvName(env)        | Deployment environment | (empty)   |

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

---

## HTTP Middleware

```go
mux := http.NewServeMux()
mux.HandleFunc("/api/users", handler)

http.ListenAndServe(":8080", tracing.Middleware(mux))
```

- Имя Span = `"METHOD /path"` (например, POST `/api/users`)
- Автоматическое распространение контекста трассировки
- Кэширование — оболочка `otelhttp` создается только один раз для каждого маршрута (высокая производительность)

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

## Shutdown
```go
defer tracing.Shutdown(context.Background(), tp)
```

Корректно завершает работу поставщика трассировки и очищает оставшиеся сегменты.

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

