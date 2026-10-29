# Разработка и команды проверки

[English](../en/development.md) | [Участие в разработке](../../CONTRIBUTING.ru.md)

## Окружение и исходники

Модуль требует Go 1.24 или новее. Проверенная версия сборки - Go 1.27.3. Клиентские тесты используют Node 24 (CI закрепляет 24.18.0; изолированная проверка использует 24.22.1), Python 3 и зависимость `@xsense/autoblow-sdk` 2.6.0 из lock-файла. Подготовка требует сети или готовых кешей. Собранному ядру и автономным тестам официальный сервис не нужен.

| Путь                                          | Назначение                                                             |
| --------------------------------------------- | ---------------------------------------------------------------------- |
| `cmd/atblse-server`, `core/`                  | CLI сервера, TLS/WSS, HTTP/SSE, DNS, хранение и встроенная панель      |
| `internal/protocol/`                          | Команды, проверка ответов и преобразование сценариев                   |
| `cmd/atblse-simulator`, `internal/simulator/` | Виртуальное loopback-устройство и временный стенд                      |
| `cmd/atblse-catalog-import`                   | Автономный импорт штатного каталога                                    |
| `cmd/atblse-windows`, `windows/`              | Windows-приложение и управление сетью/процессами                       |
| `deploy/`                                     | Docker-конфигурация с явным запуском                                   |
| `tests/clients/`                              | Закреплённые зависимости SDK и адаптер Studio для тестов               |
| `research/`, `reference/`                     | Инструменты формата, времени, статического анализа и образец протокола |
| `.standards/`, `scripts/`                     | Закреплённые проверки репозитория, тесты и упаковка                    |

Команды сборки приведены в [установке](guide.md) и [Windows](windows.md). Для изолированного стенда используйте [быстрый запуск README](../../README.ru.md). Симулятор принимает `--duration` от `1s` до `1h`. Режим `--lab` создаёт временное состояние и проверяемый loopback TLS; его нельзя сочетать с собственным URL/CA. Подключение к отдельно настроенному loopback-серверу:

```sh
./out/atblse-simulator --url wss://127.0.0.1:20443/device-backend/ws --ca local-server.crt --duration 60s
```

Сертификат должен соответствовать имени узла в URL и принадлежать этому серверу. Адреса вне loopback отклоняются. Симулятор не моделирует физическую механику; для автономной модели времени используйте `research/vacuglide_tick_model.py`.

## Обязательные программные проверки

Тяжёлые проверки выполняются на изолированном сборочном окружении. В POSIX shell при подготовленных зависимостях:

```sh
go test -race ./...
go vet ./...
python -m unittest discover -s research -p 'test_vacuglide_binary.py' -v
python -m unittest discover -s research -p 'test_vacuglide_tick_model.py' -v
python scripts/test_package_release.py -v
node scripts/panel-contracts.mjs
python scripts/prepare-client-contracts.py
npm ci --prefix out/clients-integration --ignore-scripts --no-audit --no-fund
ATBLSE_TEST_NODE=$(command -v node) ATBLSE_TEST_CLIENTS="$PWD/out/clients-integration" go test -race -tags integration ./core
go build -o out/atblse-simulator ./cmd/atblse-simulator
python scripts/test-simulator.py --binary out/atblse-simulator
node scripts/check-project.mjs
```

Подготовьте зависимости до отключения сети. Интеграционный набор проверяет SDK и адаптер Studio через HTTP/HTTPS с виртуальным устройством. Отдельный тест симулятора проверяет CLI, доверие сертификату, ограниченную сессию и обмен командами/файлами. Windows-проверкам вспомогательных функций нужен Windows PowerShell 5.1; подмена процессов/сети в тестах не подтверждает работу реальной точки доступа.

В закреплённом Linux-контейнере `scripts/verify-offline.sh` запускает Go race/vet/клиентские проверки и собирает сервер, импортёр для Linux/Windows и Windows GUI. Задайте доступный для записи `ATBLSE_RESULTS`, исполняемый `ATBLSE_TEST_NODE` и подготовленный `ATBLSE_TEST_CLIENTS`; подключите исходники только для чтения, кеши Go и отключите сеть контейнера. Квитанция сборки связывает бинарные файлы с хешами исходников. Отдельные проверки Python, панели и симулятора из списка выше также обязательны.

## Проверки репозитория

```sh
npm ci --prefix .standards --ignore-scripts --no-audit --no-fund
node .standards/install-gitleaks.mjs
node .standards/install-hooks.mjs
node .standards/check.mjs structure
node .standards/check.mjs staged
node scripts/test-standard-adoption.mjs
```

Hook проверяет добавленные в индекс байты и сохраняет предыдущие hooks. Используйте форматтер Go и закреплённую конфигурацию Prettier; не меняйте встроенный стандарт ради прохождения проверки. Запускайте закреплённый Gitleaks для индекса, добавленных коммитов и всей истории, включая изменения merge-коммитов. Отсутствие сканирования блокирует публикацию. Не запускайте непроверенный код публичного PR на частном runner автоматически.

GitHub `repository-standard` - публичная проверка инструментов с доступом только для чтения. `software-verification` фиксирует проверки точной версии исходников на изолированном окружении. Ни один статус не подтверждает установку, физическое поведение или стабильный выпуск. PR сливает владелец после обязательных проверок.

## Исследовательские инструменты и упаковка

`research/vacuglide_binary.py` декодирует штатные записи; `research/vacuglide_tick_model.py` моделирует время. `research/inspect-esp32-image.py` и `research/ghidra/` анализируют предоставленный оператором образ без прошивки устройства. `reference/vg_protocol.py` - компактный образец кодека. Образы прошивки и закрытые захваты в проект не входят.

`research/virtual-cloud-probe.mjs` - дополнительный облачный эксперимент вне автономных тестов. По умолчанию работает dry-run. Выполнение требует закрытых входных файлов, явного разрешения оператора и `VACUGLIDE_PROBE_POWER_OFF=CONFIRMED` только после физического отключения принадлежащего ему устройства. Не используйте настоящие токены и этот инструмент в CI.

`scripts/package-release.py` принимает `--build-dir`, `--output-dir`, `--source-manifest`. Он упаковывает готовые артефакты после проверки хешей исходников, квитанций сборки, метаданных образа и обязательных уведомлений. Выходной каталог не должен существовать. Пакет не означает опубликованный или физически принятый выпуск. Открытые проверки приведены в [статусе проверки](verification.md), условия распространения - в [статусе лицензии](../../LICENSE.md).
