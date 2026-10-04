# tg_gateway_go — инструкции для агентов

Go-код Telegram-шлюза Simple Smart Home: прием команд пользователя и отправка уведомлений через Telegram Bot API (ADR-0017, ADR-0018).
Шлюз подключается к ядру по сети через Go-модуль контракта `github.com/Alex84K/sm_smart_home_core_go/contract`.

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

## Одновременная правка ядра и шлюза (ADR-0018, design D4)

Для локальной разработки без публикации промежуточных версий контракта:
1. В шлюзе: `make work CORE=../core_syst_go` (создаёт локальный `go.work`, связывающий шлюз с `$(CORE)/contract`).
2. Внеси правки в ядро и шлюз.
3. В ядре: выпусти релиз контракта `make contract-release VERSION=vX.Y.Z`.
4. В шлюзе: `make unwork` и обнови зависимость `make contract-update VERSION=vX.Y.Z`.
5. Закоммить изменения.

## Требования к сборке и ~/.netrc (ADR-0018, design D5)

Репозиторий ядра `sm_smart_home_core_go` — приватный.
- Для Docker-сборки образа шлюза на машине сборки (десктоп, homelab) требуется файл `~/.netrc` с правами `600` (`chmod 600 ~/.netrc`).
- Содержимое: запись `machine github.com login <github-user> password <token>` с fine-grained токеном GitHub (права `Contents: Read-only` на репозиторий `sm_smart_home_core_go`).
- Токен передаётся через BuildKit secret и никогда не попадает в образ или в `.env`.

## Проверка

- Перед коммитом: `make check` (линтер + тесты с флагом `-race`, включая архитектурный тест). Коммить только при зелёном `make check`.
- Новые команды разработки — новой целью в `Makefile`, а не инструкцией в тексте.

## Деплой на homelab

Вручную по SSH, автоматики нет (`../agentic_docs/brainstorm/open-questions.md`, раздел GitOps). Клон — `~/homelab/simple_smart_home/sm_smart_home_tg`; `.env` лежит только в клоне.
1. Ядро уже запущено (`smarthome_backend` создаёт его compose).
2. `git pull`
3. `make up` (`docker compose up -d --build`: образ пересобирается из свежего кода)

## Жёсткие границы

- Шлюз общается с ядром ТОЛЬКО по сети через HTTP API и шину событий (ADR-0017).
- Импорты по правилам архитектурного теста: `platform/*` импортирует только `platform/*`; `coreclient` — `platform/*` и контракт; `telegram/app` — всё внутри шлюза и контракт; `cmd/tg-gateway` — только свой `app` и `platform/*`.
- Прямых зависимостей от внутренних пакетов ядра нет — только модуль `contract/`.
- Секреты только в `.env` (он в `.gitignore`); в коде, логах, тестах и примерах — никогда.

## Соглашения о коде

Обязательно прочитай [`CONVENTIONS.md`](CONVENTIONS.md) перед написанием кода.

@CONVENTIONS.md
