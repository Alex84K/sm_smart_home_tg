# tg_gateway_go — инструкции для агентов

Go-код Telegram-шлюза Simple Smart Home: прием команд пользователя и отправка уведомлений через Telegram Bot API (ADR-0017).
Шлюз подключается к ядру по сети через Go-модуль контракта `github.com/Alex84K/core_syst_go/contract`.

## Источник правды — общий OpenSpec store

Требования, ADR и планы лежат в общем store `simple-smart-home` (репозиторий `agentic_docs`, соседняя папка). `openspec/config.yaml` здесь — указатель на него.

Перед работой:
1. `openspec list` — найди change, над которой работаешь; `openspec show <change>` — её proposal, design, specs, tasks.
2. Прочитай ADR, на которые ссылается change (`../agentic_docs/adr/`), и контекст `../agentic_docs/openspec/config.yaml`.
3. Не делай ничего, что противоречит принятому ADR. Если задача требует нового архитектурного решения — остановись и предложи ADR.

Реализация change — `/opsx:apply`; отмечай выполненные задачи в её `tasks.md`.

## Порядок локальной разработки (ADR-0017)

Токен бота один на десктоп и homelab. Чтобы избежать конфликта `409 Conflict`:
1. На homelab: `docker compose stop tg-gateway`
2. На десктопе: подними локальный шлюз через `make up-dev` (или `make run`)
3. После завершения работы на десктопе: `make down` на десктопе, затем запусти бота на homelab (`docker compose start tg-gateway`)

## Проверка

- Перед коммитом: `make check` (линтер + тесты с флагом `-race`, включая архитектурный тест). Коммить только при зелёном `make check`.
- Новые команды разработки — новой целью в `Makefile`, а не инструкцией в тексте.

## Жёсткие границы

- Шлюз общается с ядром ТОЛЬКО по сети через HTTP API и шину событий (ADR-0017).
- Импорты по правилам архитектурного теста: `platform/*` импортирует только `platform/*`; `coreclient` — `platform/*` и контракт; `telegram/app` — всё внутри шлюза и контракт; `cmd/tg-gateway` — только свой `app` и `platform/*`.
- Прямых зависимостей от внутренних пакетов ядра нет — только модуль `contract/`.
- Секреты только в `.env` (он в `.gitignore`); в коде, логах, тестах и примерах — никогда.

## Соглашения о коде

Обязательно прочитай [`CONVENTIONS.md`](CONVENTIONS.md) перед написанием кода.

@CONVENTIONS.md
