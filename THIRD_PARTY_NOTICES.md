# Third-party notices / Сторонние компоненты

- Go runtime and Gorilla WebSocket: notices in `licenses/`; Go modules are locked by go.sum.
- The copied repository-standard toolkit is MIT-licensed under `.standards/LICENSE` and pinned by `.standards/lock.json`.
- `@xsense/autoblow-sdk` 2.6.0 is an optional development dependency installed by its lockfile, not copied into this source tree. Its package retains its own notices.
- `tests/clients/studio-*.ts` is a bounded interoperability fixture from the owner's Studio adapter. Only its request behavior and types are included; no installed application, state or credentials are copied.
- `core/catalog/factory.json` contains manufacturer catalog identifiers and encoded reference timelines; protocol golden vectors contain bounded interoperability observations. No project license is asserted for manufacturer material. They are separate from independently authored source and are not the downloaded video-library collection. No vendor firmware images or media files are included.
- Autoblow and VacuGlide names identify compatibility. This is an independent project and does not claim manufacturer endorsement.

Go, Gorilla WebSocket и инструменты стандарта сохраняют свои лицензии. SDK устанавливается отдельно как зависимость разработки. Снимок адаптера Studio ограничен HTTP-контрактом и типами. ID/бинарные эталоны каталога производителя и небольшие наблюдения протокола отделены от собственного кода; лицензия проекта на них не заявляется. Собранная видеотека, прошивки и медиа не публикуются. Названия Autoblow/VacuGlide используются для указания совместимости, проект независимый.
