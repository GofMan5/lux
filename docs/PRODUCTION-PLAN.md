# LUX TUI — план production и дизайн-системы

> Внутренний рабочий план после v0.1.0. Основание: read-only проверка `4381086`, текущего tour, runtime, UI, terminal adapter, CI и измерителя производительности. Это очередь работ, не заявление, что production-приёмка уже выполнена.
>
> Проверка в этой сессии: `go test ./...`, `go vet ./...`, `gofmt -l .` успешны на Windows / Go 1.25.5; тесты использовали в том числе кэш. Root-команда не охватывает вложенные модули e2e, benchmarks и SSH-пример. Новые нагрузочные, race, security-repro и ручные терминальные проверки не проводились.

## 1. Направление

**Цель:** компактный Go-стек, на котором быстро собираются красивые и удобные терминальные приложения, а корректность, совместимость и стоимость их работы подтверждены тестами и измерениями.

Рабочие допущения для первой production-матрицы:

- Основной пользовательский host — Windows Terminal; Linux/macOS PTY и SSH/tmux — обязательные переносимые сценарии.
- Первые приложения — инструменты разработчика: таблицы/логи, master-detail, редакторы, формы, команды и длительные задачи.
- Клавиатура — полноценный основной способ управления; мышь — дополнение, а не обязательное условие.
- `80×24` — полезный компактный режим, `120×40` — обычный, `240×80` — большой. Меньшие размеры не ломают ввод/рендер; содержимое раскрывается последовательно или прокручивается.
- Красота достигается композицией, плотностью, иерархией, состояниями и умеренным движением, а не рамкой вокруг каждого блока.

**Сохраняем:** immediate-mode, cell/grapheme-модель, inward dependencies, public Backend seam, небольшое ядро и текущий бюджет трёх прямых зависимостей. Не начинаем новую архитектуру рендера.

## 2. Что уже есть — не строить повторно

- Диффующий cell renderer, wide/EGC safety, цветовая деградация, width policy, wrapping.
- Idle event loop, adaptive pacing, latest-frame backpressure, frame-bound routing и sidebands.
- Kitty/win32 keyboard, mouse/focus/paste, capability negotiation, suspend/resume.
- Input/TextArea, Table/Tree, Tabs/Select, Checkbox/Radio, viewport, modal, Gauge/Sparkline/Spinner.
- Theme, scoped shortcuts, bounded undo и explicit clipboard seam.
- Unit/property/fuzz checks, VT reference model, сравнительный harness, настоящие PTY/terminal lanes.

Источники: [обзор](<../README.md#L47-L62>), [архитектура](<ARCHITECTURE.md#L39-L55>), [performance contract](<PERFORMANCE.md#L10-L30>), [терминальная матрица](<../.verification/matrix/README.md#L23-L39>).

## 3. Пробелы, которые меняют порядок работ

| Приоритет | Факт / риск | Основание | Как закрыть |
|---|---|---|---|
| P0 | OSC 8 URI/ID проходят в wire payload без очистки. При включённых links и внешних данных возможно внедрение управляющих терминальных последовательностей. Это не утверждение об OS/RCE. | [wire sink](<../internal/terminal/encoder.go#L602-L617>), [Link](<../render/types.go#L86-L97>) | Минимальный malicious/legitimate byte-level repro; защита всех путей на encoding seam; негативные/property/fuzz проверки. Links остаются opt-in. |
| P0 | Измеренная CPU/alloc/bytes фаза не совпадает с фазой paint/draw счётчиков: warmup не вычитается. | [reset и сбор счётчиков](<../benchmarks/child.go#L100-L115>), [финальные counts](<../benchmarks/child.go#L198-L215>), [деление](<../benchmarks/metrics.go#L338-L343>) | Единые start/end snapshots для каждой метрики; тест известного числа событий/кадров; регенерация отчёта. |
| P0 | Ожидание quiet может принять старый read за завершение нового действия; отсутствие нового output превращается в latency=0. | [waitQuiet](<../benchmarks/pty.go#L150-L163>), [latency](<../benchmarks/child.go#L230-L236>) | Проверять causal acknowledgement нового состояния, а не тишину старого; no-output/timeout — отдельный результат, не нулевая задержка. |
| P0/P1 | Tour не является полным и проверенным эталоном: нет TextArea/Select/Tree, нет своих тестов; статичные вкладки постоянно запрашивают кадры. | [dispatch и scheduling](<../examples/08-tour/main.go#L334-L361>) | Исправить routing/resize/idle/motion, затем включить весь реально реализованный каталог и состояния. |
| P1 | При mid-flight spring retarget `From` берётся из старого target, а не sampled position. | [retarget](<../examples/08-tour/main.go#L275-L280>) | Перетаргетировать из текущих position/velocity; тест непрерывности. |
| P1 | Пространство клавиш не проверено через реальный router: Space на Motion/3D поглощается focused Tabs; Ctrl+Q не учитывает legacy uppercase C0 representation. | [tour handlers](<../examples/08-tour/main.go#L120-L232>), [router](<../ui/context.go#L453-L457>), [legacy decoding](<../internal/terminal/parser.go#L176-L180>) | Сценарии после успешной presentation: tab switch → Space; quit из каждого focus/modal состояния; legacy/enhanced fixtures. |
| P1 | Компактный tour теряет controls, поскольку фиксированные Y/высоты не прокручиваются; панели не ограничивают root drawing. | [Widgets layout](<../examples/08-tour/main.go#L688-L759>), [panel helper](<../examples/08-tour/main.go#L388-L393>) | Stacked layout + scrollable content + clipped subviews; reachable controls при 80×24. |
| P1 | Theme состоит из четырёх ролей; старые controls и новые widgets разрешают стили неодинаково. Disabled смешан с Danger. | [Theme contract](<../ui/theme.go#L5-L26>), [Button](<../ui/controls.go#L40-L50>) | Совместимые fixes отдельно; semantic roles и единый state vocabulary — в minor release, только по используемым паттернам. |
| P1 | PTY e2e — отдельный модуль, отсутствующий в root CI jobs. Terminal evidence есть, но не является постоянной release-регрессией. | [CI](<../.github/workflows/ci.yml#L15-L128>), [e2e](<../e2e/pty_unix_test.go#L1-L3>) | Явные lanes для nested modules; release matrix с версиями и артефактами. |
| P1 | Bounded lifecycle не полностью доказан: timeout write-slot не ограничивает собственный синхронный restore write. Зависание на реальном host не воспроизводилось в этой проверке. | [Close](<../internal/terminal/backend.go#L778-L810>), [writeAll](<../internal/terminal/backend.go#L1051-L1078>) | Fault-injection и stopped-draining PTY/pipe для enter/present/leave/suspend/resume; cleanup в согласованный срок. |
| P1 | Command возвращает terminal-oriented Event без прикладного payload, а event.Error завершает runtime. Нужен end-to-end proof для реальной асинхронной загрузки данных. | [Command](<../app.go#L19-L34>), [Event](<../event/event.go#L69-L82>), [error handling](<../app.go#L495-L518>) | Сначала reference app loading/error/retry/cancel; затем минимальный seam прикладных результатов, без side-effect mutation из goroutine. |

P0/P1 здесь обозначают порядок работ, не CVSS или подтверждённую severity. Статические findings выше нужно закрепить минимальными regression checks. Не выводить из зелёных существующих тестов, что отсутствующие сценарии уже корректны.

## 4. Этап 0 — честная база и быстрые исправления

**Результат:** безопасный patch-кандидат v0.1.1, исправленный tour и достоверный измеритель.

### 0.1. Security patch

- Защитить OSC 8 URI и ID от control-string termination/injection на общем encoding seam.
- Зафиксировать политику invalid/empty/oversized URI и ID; при отказе текст остаётся обычным текстом.
- Негативные проверки + legitimate Unicode URL + opt-out byte identity.
- Обновить security policy: поддерживаемая ветка, доставка fixes, запрет вывода секретов в artifacts.

### 0.2. Tour correctness

- Сделать page-local клавиши доступными при реальном focus, не забирая текст у редакторов.
- Регистрировать raw input до routing; показывать decoded event и факт, что его consumed конкретный control. Добавить Text и фильтры, ограничить объём preview paste.
- Исправить retarget-from-position/velocity и sampling времени: `frame.Now`, без произвольных `time.Now()` в drawing.
- Reduced motion действительно меняет поведение, а не только подпись.
- Статичные страницы не запрашивают следующий кадр; spinner/progress/motion запрашивают только ближайший нужный deadline.
- Пауза и уход с анимированной вкладки не оставляют скрытый непрерывный animation loop без причины.
- Progress state отражается независимо от выбранной вкладки; локальные notify/copy requests имеют ясную политику dropped frame. Не обещать exactly-once или OS delivery при отсутствии acknowledgement. Не менять Model напрямую из `OnPresented`: callback работает на presentation goroutine, а acknowledgement/изменения состояния должны попадать сериализованно в Update.
- Общие clipping, compact layout и scroll fallback. Ничего обязательного не исчезает без способа доступа.
- Убрать формулировки о «полных возможностях», пока полный каталог и состояния не проверены.

### 0.3. Калибровка benchmarks

- Warmup исключён из всех counters и denominators; idle window не попадает в measured-frame counts.
- Разделить View invocation, successful presentation и wire burst: это разные события.
- Сопоставимые значения считать по одинаковому delivered work; несовпадающие semantics явно маркировать.
- Input-to-observed-output измерять через sequence/ожидаемое состояние terminal model. PTY receipt не равен физическому отображению пикселей терминалом.
- RTT/slow-link fixture действительно откладывает реакцию в нужном направлении; маленькая искусственная задержка не может дать «0 ms success».
- Ошибка, stale sample или timeout не исчезают из итоговой статистики.
- Пересчитать отчёт на закреплённом host; старые спорные latency/per-frame claims в README пересмотреть, сохранив историю артефактов.

**Gate:** каждый дефект имеет regression check; API patch совместим; заявленные функции tour доступны; измеритель проходит positive/negative calibration cases; новые claims подтверждены новым отчётом.

## 5. Этап 1 — production reliability и постоянная верификация

**Результат:** CI/release ловят сбои реальных сценариев, а не только unit happy path.

- Добавить отдельные jobs для e2e на Linux/macOS; nested SSH build/tests на подходящей OS. Не надеяться на root `go test ./...`.
- В PR: быстрые tests/race/vet/lint, core fuzz smoke, tour interaction/snapshots, allocation guards.
- По расписанию/release: более долгие parser/TextInput/TextArea/wrap fuzz, terminal lanes, slow-link/resize/input stress.
- Проверить Open/Present/Close и suspend/resume при partial write, zero write, error, blocked writer, cancellation и panic.
- ~~Определить контракт SIGINT/SIGTERM на Unix: кто отвечает за cleanup — runtime adapter или documented application pattern; SIGKILL cleanup не обещать.~~ Сделано: runtime ловит пару и сам делает restore (leave-последовательности, идемпотентный latch), приложение получает `lux.ErrInterrupt`/`lux.ErrTerminated` из `Run` и само ставит код 130/143; SIGKILL вне обещаний, SIGHUP — как раньше (loss, без restore); Ctrl+C в raw-режиме остаётся Key-событием. Юнит-тесты диспетчера (`!windows`), app-уровневый тест маппинга, e2e с настоящим `kill` как оракулом (wait status обязан быть exit, не signal).
- Доказать нет блокирующего exit и утечки framework-owned goroutines; arbitrary custom Backend/Command обязан соблюдать cancellation contract.
- Fixtures win32 non-BMP/UTF-16 pair, split/dangling records; ручной emoji/CJK ввод на Windows Terminal. Отличать source hypothesis от проверенной совместимости.
- Проверки WidthPolicy по всем widgets: особенно Tabs/Checkbox custom markers, hit mapping и measured width.
- Table last-row hit mapping, сохранение wheel viewport после drawing и sorting request handling; overlay не оставляет caret скрытого редактора; focus восстанавливается после dismiss.
- Продолжать EGC/cell conformance; segmentation-версию и width-версию документировать отдельно, не выдавать overlay за обновление segmentation.
- Свести противоречивые ранние snapshot-формулировки compatibility/architecture к текущим доказательствам.

**Gate:** release candidate проходит заявленную матрицу; восстановление terminal state проверено; dropped/stale frames не меняют hit routing; every supported control has keyboard path. Для невалидированных hosts остаётся честная пометка, а не broad support claim.

## 6. Этап 2 — реальное приложение и прикладные async-сценарии

**Результат:** первая reference app делает полезную работу и испытывает framework там, где витрина бессильна.

Первый кандидат — task/process explorer с list/table + details + searchable logs + actions. OS access изолирован от UI через минимальный Adapter; mock data и deterministic tests используют тот же Interface.

- Loading → data → empty/error → retry; cancellation при выходе/переключении.
- Отдельно fatal transport/runtime failure и recoverable прикладная ошибка.
- Прикладные результаты доставляются сериализованно в Update, не через запись Model из worker goroutine.
- Request identity / last-request-wins для поиска и обновлений: старый результат не заменяет новый.
- Bounded workload; сначала простой single-flight/cancellation в приложении. Generic scheduler/pool — только если профиль/повторный use case докажет необходимость.
- Минимальное расширение application-event Seam допускается в minor release. Не выбирать Interface абстрактно до reference slice.

Архитектурный смысл: углубить runtime Module там, где его Interface сейчас переносит прикладную Implementation в callers; выигрыш — Locality обработки результатов и Leverage сериализации/cancellation. Два реальных Adapter/use case полезнее набора гипотетических interfaces.

**Gate:** приложение живёт с реальными данными, задержками и ошибками; UI не блокируется; stale results игнорируются; tests используют публичный Seam; потребность в API change подтверждена кодом, а migration example существует.

## 7. Этап 3 — практичная дизайн-система

**Результат:** изменение темы или плотности не требует переделывать весь экран.

### 3.1. Semantic tokens

Начать с реально используемых ролей:

- canvas/surface/text/muted/border;
- accent/selection/focus;
- success/warning/error;
- disabled отдельно от danger.

Состояния: normal, hover, focus, pressed, selected, disabled, read-only, loading и validation error. Они не должны отличаться только цветом.

- Проверить единое применение темы старыми и новыми controls; explicit styles продолжают побеждать defaults.
- Начать palette/style recipes в примере. Переносить повторение в библиотеку после двух реально использующих его экранов.
- Theme presets: terminal-default, dark, light, high-contrast. Mono/ANSI16 не превращают selected/focus/error в одинаковый вид.
- Плотность compact/comfortable: размеры в cells, предсказуемые gutters и content budget.
- Типографическая иерархия в TUI — вес, атрибуты, отступы и подписи, не попытка управлять шрифтом terminal host.
- Настройки persist принадлежат приложению; theme registry/config framework не нужен.

### 3.2. Composition recipes

На существующих Rect/Flex/Subview/ScrollViewport:

1. App shell: navigation + content + contextual status/help.
2. Master-detail: sidebar/list + details; на компактном размере — последовательные views.
3. Data workspace: toolbar/filter + table + optional inspector.
4. Form: label, field, hint/error, actions, сохранение.
5. Log workspace: scroll, filter, follow-tail, pause.

Border не используется по умолчанию для каждого блока. Цвет акцента — действия/выбор, а не все заголовки. Overlay имеет осмысленную область, не обрезается случайно соседней панелью.

**Gate:** одна theme restyle меняет все controls; compact и comfortable не меняют логику; 80×24/120×40/240×80 сохраняют задачи и focus; две reference compositions используют одинаковые patterns без копипасты styling.

## 8. Этап 4 — UX kit: действия и состояния, а не количество widgets

**Результат:** приложения не изобретают заново базовое взаимодействие.

Приоритетные вертикальные slices:

1. **Command palette:** простой filter сначала, команды/shortcut hints, keyboard-first, no-result, disabled reason. Fuzzy ranking только при доказанной пользе.
2. **Forms:** label/hint/error, read-only/disabled, submit-in-flight, inline validation, unsaved changes. Не весь generic form builder.
3. **Select/popover:** placement в доступном viewport, flip above near bottom, Esc/outside close, focus restore, стабильный capture. Сохранять paint-order/presentation-bound принцип.
4. **Async feedback:** пустое состояние с следующим действием, loading без layout jump, partial/stale data, retry, cancel, progress. Spinner не обязателен при каждой загрузке.
5. **Data workflows:** сортировка действительно обслуживается, поиск/фильтр, stable row identity; horizontal table scroll только если реальный набор колонок его требует.
6. **Logs:** bounded history, incremental filtering, follow-tail не перебивает ручной scroll.
7. **Discoverability:** hints относятся к текущему focus/state; help не требует запоминать скрытые сочетания.

Начать как recipes над существующими widgets. Новый глубокий Module нужен только там, где повторяющаяся Implementation расползается по нескольким приложениям.

**Gate:** полные keyboard/mouse flows; нет focus trap и невидимого caret; каждое действие имеет outcome/failure path; empty/loading/error/disabled состояния воспроизводимы в tour и tests.

## 9. Этап 5 — измеренная оптимизация

**Результат:** UI выглядит плавно и не тратит ресурсы без пользовательской причины.

После калибровки инструмента — профиль `Update → layout → draw → diff → encode → flush` на reference apps.

Порядок оптимизаций:

1. Не запрашивать кадры, которые не нужны. Spinner обновляется по glyph deadline, не 60 раз/с ради 80-ms глифа.
2. Не выполнять работу скрытых/offscreen областей; scheduling учитывает visibility, pause и reduced motion.
3. Не повторять wrap/layout неизменного текста. Cache key включает content, width, wrap mode и WidthPolicy; cache имеет предел и invalidation tests.
4. Не создавать `fmt.Sprintf`, slices/maps и repeated styles в доказанном hot path, если профиль подтверждает стоимость.
5. Виртуализировать данные: cost зависит от visible rows, не от 200k/1M total.
6. Caller-reused storage только по измеренной потребности; не расширять каждый API ради нулевых allocations.
7. Ограничить history, pending async work и caches; проверить память длительной сессии.
8. Не трогать linear routing/geometry ради spatial index без profile evidence.

### Первичные бюджеты — цели, не текущие результаты

| Сценарий | Gate |
|---|---|
| Статичный неизменный UI | 0 recurring animation requests; unchanged frame — 0 terminal bytes; idle CPU ориентир <1% одного core на закреплённом local host |
| Обычный экран 120×40 | Начальный ориентир p95 Update+layout+draw+encode ≤8 ms, без terminal/transport flush; сложные сцены имеют отдельный budget |
| Interactive reaction | Ориентир p95 input-to-observed-output ≤50 ms на local PTY; slow links оцениваются отдельно, без обещаний невозможной задержки |
| Animation | 60 FPS только когда бюджет позволяет; degradation 30/20/10 без backlog и потери корректности времени |
| Virtual data | O(visible rows) drawing; проверка 200k/1M rows; bounded memory при длительном scroll/filter |
| Regression | Стабильный alloc/byte budget; повторные A/B rounds. Примерный alarm >10% CPU p95 или >20% alloc, но не случайный noisy CI threshold |

Порог уточняется после нового baseline и выбранного host. PR CI ловит детерминированные properties; timing сравнивается на стабильном benchmark host.

**Измеренный baseline (2026-10-10, калиброванный harness, полный прогон 168 runs / 0 failed; архив — в истории git; в benchmarks/results закоммичена обновлённая батарея 174 runs от 2026-10-12; i5-10400F, golang:1.26-alpine, Docker/WSL2):**

- Статичный UI: lux — 0 bytes/frame при 289 paints (unchanged frame реально не попадает в wire); idle lux 0.09–0.14% core (tea 0.67–1.46%, tview ~0.01%).
- Причинная input→output latency (p95, 120×40 / 240×80): lux resize 3.7 / 11.6 ms; rtt 3.1 / 15.4 ms; inputburst 19.6 / 24.0 ms — в бюджете ≤50 ms. tea inputburst 160 / 709 ms, tview inputburst 676 / 3633 ms (сериализация QueueUpdateDraw — их реальное поведение, не наш blocker).
- Исторический mean CPU/frame на paced-сценах 120×40 (lux): onecell 3.0, list 2.9, subcell 2.1, damage 3.2, full 3.9, static 6.0, egc 12.8 ms — это не p95 и не основание закрыть бюджет подготовки. В slice 35 (`05dc15a`) harness уже получил per-present process-CPU p50/p95/p99 (`frame_cpu_us`); дельта включает фоновой CPU между презентациями и не равна Update+layout+draw+encode без flush. Mean также включает CPU финального settle (особенно static). Подготовка-only p95 на reference flows и профили сложных сцен остаются открыты.
- Carriage для сравнения A/B: benchstat.txt + raw/*.json закоммичены; повторяемость подтверждена 3 rounds на ячейку.

**Этап 5, item 2 — измерено частично (2026-10-12, slice 36):**

- `render.Canvas.Text` измеряет кластер один раз (общий с `Put` путь записи ячеек, `putMeasured`) и не пишет ячейки на строках вне canvas/clip и на исчерпанных столбцах; скрытые строки по-прежнему измеряются для возвращаемой ширины (widest input line). Корректность закреплена reference-алгоритм тестом и fuzz-целью по полному буферу и возвращаемому значению.
- A/B, 6 чередующихся раундов (benchstat, i5-10400F, Go 1.25.5, все allocs 0): visible-ascii −18.78% (p=0.041), visible-egc −46.95% (p=0.002), clip-band −20.12% (p=0.015), exhausted-columns −25.54% (p=0.009), tall-overscroll 2000 строк (CPU-only) −29.73% (p=0.002); geomean −29.03%. Регресс-проверка того же сеанса: CanvasFrame −19.92% (p=0.002; B/op 314.5→259.0), Put +4.22% (p=0.015, уровень шума), putcell/fill без изменений.
- PTY tall 160 строк (3 чередованных раунда, Docker golang:1.26-alpine): p50/p95 7.80/11.71→6.22/8.41 мс (120×40), 15.27/23.98→13.00/17.96 мс (240×80); idle 0.11–0.17% одного core; 0 ошибок.
- Tour: gauge/spinner запрашивают кадры только при реальной видимости (по clipped-геометрии панели на каждом View); скрытый spinner — 0 recurring frames, показ возобновляет анимацию; reduced motion замораживает декор; progress sidebands не зависят от видимости. Закреплено матричным и end-to-end тестом.
- Baseline переизвлечён на новом коде: 174 runs / 0 failed (~55 мин на закреплённом host; добавлены lux-only сцена tall и флаг `-scenes`). Кросс-сессионные lux p95 deltas против старой батареи (−14.0…+98.2%) не подтвердились как регрессии: чередованная same-session проверка (30+30 runs, 5 сцен × 2 размера × 3 раунда) дала −23.9…+9.9% при плоских per-paint allocs и стабильных paints. Причина: `frame_cpu_us` — per-present delta CPU процесса; при меньшем числе presentations значения на кадр растут. Библиотечных регрессий не выявлено; lux-only сцена rtt в confirm-набор не входила, её delta адъюстирована только микробенчмарками (честно зафиксировано).
- Честно не закрыто в item 2 (следующий slice этапа 5): `ui.Table` вызывает `rowAt` для строк внутри bounds, но обрезанных более тесным `ClipRect`; `ui.RadioGroup` форматирует/клипует скрытые labels; большинство widgets делает ранний return только по пустому bounds-intersect без проверки `ClipRect`; `TextArea.drawAreaRow` дублирует GraphemeWidth+Put измерение, которое `Text` теперь делит.

**Этап 5, item 3 — ClipRect-aware отрисовка widgets (2026-10-12, slice 37, закрывает три пункта из списка выше):**

- `render.Canvas.DrawableRect()` — публичный drawable-прямоугольник в локальных координатах canvas (clip ∩ buffer → локальные координаты → ∩ Bounds); `Clip(bounds)` сохраняет локальные координаты, поэтому пересечение с bounds корректно до/после clip. Закреплено тестом `TestCanvasDrawableRect`.
- Рядо-ориентированные widgets (`Table`, `Tree`, `List`, `RadioGroup`) переводятся на три шага: ранний return по `bounds.Intersect(canvas.DrawableRect()).Empty()`; `Fill(drawable)` (output-эквивалентно полному `Fill` — заполнение посимвольно клипуется); цикл строк по band из helper'а `drawableRowBand` — `rowAt`/`DisabledAt`/форматирование labels оплачивают только рисуемые строки. Одинично-клеточные widgets (`Button`, `Checkbox`, `Gauge`, `Sparkline`, `Tabs`, `Spinner`) — только ранний return через `DrawableRect`. Recipe (user-facing пример) — тот же паттерн.
- Инвариант регистрации не тронут: регионы регистрируются с ПОЛНЫМИ логическими bounds до раннего return; `GlobalRect` клипует опубликованный rect, `originY` хранит неклипованный логический origin, contentHeight ограничивается клипованным низом (пре-существующая семантика `RegionWithOptions`). Закреплено тестами: `originY == 0`, region.bounds == клипованный rect, contentHeight == min(логическая, клипованный низ). Скролл-клампы (`keepRowVisible`/`ensureVisible`) и `ContentHeight` считаются от `bounds`, без изменений.
- Корректность: класс reference-equivalence тестов — клипованная отрисовка сравнивается с неклипованной (свежие идентичные состояния) по ячейкам: внутри clip — byte-identical (`Cell` структурно), вне clip — `render.BlankCell`. Негативные тесты (RED до реализации): rowAt вызовов 11→6 (Table), 10→4 (Tree), DisabledAt 10→6 (List) в сценах с клипом на половину.
- A/B, 6 чередующихся раундов (benchstat, i5-10400F, Go 1.25.5, before = parent commit 690adbd в отдельном worktree): RadioGroupDrawClippedHalf 42.91→24.47 µs −42.97% (p=0.002; B/op 160→128, allocs 7→4); TableDrawClippedHalf 324.5→178.1 µs −45.13% (p=0.002; B/op 1094→651, allocs 57→32); TreeDrawClippedHalf 175.18→81.35 µs −53.56% (p=0.002; B/op 396→218, allocs 11→5). Неклипованные базовые: RadioGroupDraw ~, TableDraw ~, TreeDraw ~ (p≥0.240); geomean по шести −27.02%.
- Наблюдаемое поведение (0.x minor, отражено в CHANGELOG): колбэки `rowAt` (Table/Tree) и `List.DisabledAt` вызываются только для рисуемых строк; скрытые labels RadioGroup не форматируются. Строки остаются адресуемыми (регион держит полные bounds).
- Намеренно вне срезки: `ui.Select` popup (геометрия PLACEMENT/флиппинга, не skip), `ui.Modal` (bounds∩canvas задаёт scope BeginModal и Subview — изменение сдвинуло бы семантику), `TextArea.drawAreaRow`/`TextInput` row-draw helpers (планировались slice 38; фактически закрыты в slice 40 ниже — срез 38/39 ушёл на stress-flake в 05-widgets).

**Этап 5, item 2 (остаток) — single-measurement row painting (2026-10-12, slice 40, закрывает последний пункт списка item 2 выше):**

- `render.Canvas.PutMeasured(x, y, grapheme, width, style)` — публичный write-путь для кластера, чью ширину caller уже измерил через `GraphemeWidth` под политикой этого canvas. Контракт: один printable кластер + точная измеренная ширина; width==0 — merge в предыдущую ячейку (`appendCombining`), непечатаемые/control и пустая строка — no-op (как у `Put`), отрицательная ширина — no-op. `Put`/`PutCell` остаются общими derive-and-validate входами.
- Корректность: класс reference-equivalence в render — `TestPutMeasuredMatchesPut` (батарея кластеров: ASCII/wide/emoji-ZWJ/flags/standalone- и trailing-combining/VS16/ambiguous/control/invalid/empty × 8 политик ширины × 6 геометрий canvas: nil, zero, seeded wide+links, clip, subview+clip, пустой clip × origin, включая off-canvas; сравнение полного буфера по ячейкам + равенство advance) и fuzz row walk `FuzzPutMeasuredMatchesPut` (произвольный текст, включая invalid UTF-8; 178k execs, 0 фейлов).
- Конверсия: `TextArea.drawAreaRow`, `TextInput.drawInput`, `TextInput.drawPlaceholder` — видимый кластер измеряется один раз (`GraphemeWidth`), ширина передаётся в `PutMeasured`. Семантика веток сохранена: `drawPlaceholder` пропускает zero-width, `drawAreaRow`/`drawInput` пишут их в merge-ветку; tab-стопы и trailing-caret пробелы — `PutMeasured(" ", 1)`. До этого каждый видимый кластер платил двойную сегментацию+измерение (`GraphemeWidth` + `firstGrapheme` внутри `Put`).
- A/B, 6 чередующихся раундов (benchstat, i5-10400F, Windows Go 1.25.5, before = parent commit 60140f1 в отдельном worktree; bench-файлы скопированы в before-tree, только публичный API): TextAreaDrawClippedHalf 89.34→75.98 µs −14.96% (p=0.002; B/op 0→0, allocs 0→0); TextInputDraw 81.00→79.24 µs −2.18% (p=0.041; 0→0/0→0 — строка ~40 кластеров, доминируют фиксированные per-frame издержки: region-регистрация, caret-column проход, window bookkeeping); geomean −8.79%.
- Наблюдаемое поведение не изменилось (byte-identical по эквивалентности) — CHANGELOG не затронут. Render-level `BenchmarkPutMeasured` (ascii/wide/combining) добавлен для регресс-трекинга.
- На момент slice 40 оставались `ui.Select` popup и `ui.Modal` paint (закрыты slice 41), deterministic alloc/byte guards (расширены slice 42), phase-level profiling, p95 подготовки без flush, bounded-memory и финальный regression report. Это задачи бюджета/gate, не новая нумерация items 5–8 выше; отдельный damage API без профиля не требуется.

**Этап 5, item 2 (финал) — Select popup и Modal в drawable band (2026-10-12, slice 41, закрывает два осознанных остатка slice 37/40):**

- `ui.Select`: anchor-строка и popup рисуются только в drawable band — скрытая clip'ом anchor-строка пропускает своё `clipTextEllipsis` (усечение текущего значения), popup-`Fill` покрывает band вместо полного rect, цикл опций идёт по `drawableRowBand` и скрытые строки не платят усечение label/`themedStyle`. Геометрия PLACEMENT/флиппинга и регистрация региона не тронуты: решение остаётся на `SelectPlacement` против `canvas.Bounds()` (public API для modal-обёрток), `anchorRow`/press-rows контракт сохранён (нажатая строка == нарисованная).
- `ui.Modal`: `Fill` только `bounds.Intersect(canvas.DrawableRect())`, title-строка — guard по band (скрытая title-строка не платит `clipText`), а `Border(bounds)` и `BeginModal`/`Subview(bounds.Inset(1,1))` — без изменений: scope модалки остаётся полным `bounds∩canvas.Bounds()`, рамка держит полный rect (сжатие rect сдвинуло бы углы). Скрытые ячейки по-прежнему отбрасываются per-cell.
- Корректность: reference-equivalence тесты — клипованная отрисовка == неклипованная по ячейкам (внутри clip — identical, вне — `render.BlankCell`): Select в обоих placements (below-anchor и flipped), Modal с сохранением полного интерьера 12×6 из Subview; alloc-gate `TestSelectClippedHiddenRowsCostNoTruncation` (`AllocsPerRun`): 12 опций с усечением, 6 видимых из 12 — budget ≤ 40 allocs (до конверсии 65: ~5 allocs на каждое из 13 усечений).
- Попутно закрыт пре-существующий дефект: `BenchmarkSelectDrawOpen` мерил ЗАКРЫТЫЙ select (`Open:false` при комментарии про открытый popup) — теперь `Open:true`.
- A/B, 6 чередующихся раундов (benchstat, i5-10400F, Windows Go 1.25.5, before = parent commit 389561e в отдельном worktree; bench-файл скопирован в before-tree, только публичный API): SelectDrawClippedHalf 555.1→268.9 µs −51.57% (p=0.002; B/op 2.277 Ki→1.143 Ki, allocs 75→36 — половина усечений скрытых строк исчезла); ModalDrawClippedHalf 38.63→29.21 µs −24.40% (p=0.002; B/op 105→105, allocs 2→2 — скрытая стоимость была fill-walk'ом, не аллокацией); geomean −39.49%.
- Наблюдаемое поведение не изменилось (byte-identical) — CHANGELOG не затронут. С этим срезом family drawable-row-band полностью закрыта: Table/Tree/List/RadioGroup (slice 37) + Select popup/Modal (slice 41).
- Drawable-row-band family закрыта, но это не закрытие всего stage 5: phase-level profiling на reference flows, p95 подготовки без flush, bounded-memory и итоговый regression report остаются. Deterministic alloc guards расширены следующим срезом; caller-reused storage/spatial index/new damage API — только при подтверждённой потребности.

**Этап 5, Regression gate — deterministic allocation budgets (slice 42):**

- `ui/alloc_guard_test.go`: `TestWidgetDrawAllocationBudgets`, 20 warmed draw-сценариев по 15 public draw-методам Context; обычный root `go test` включает guard в существующую Go 1.23/stable × 3 OS/race матрицу. Без новой CI-службы, dependencies или runtime/API изменений.
- Калибровка Windows/amd64, i5-10400F, Go 1.25.5: Input/TextArea warm = 0; allocating-сценарии 1–41 allocs/draw; 5 uncached повторов с `AllocsPerRun(100)` дали идентичные counts. ≤4 — точный ceiling, большие counts допускают не более 20% роста с округлением вниз (пример: SelectClipped 36≤43, TableMillion 41≤49). Ceilings явные, baseline не переобновляется автоматически.
- Negative control: временные 10 escaping allocations в каждом draw дали FAIL всех 20 subtests; инъекция удалена до зелёной батареи. Это проверка чувствительности gates, не исправление существующего дефекта и не A/B ускорение.
- Scope: warm painting; Context unbegun (Modal включает scope); region registration/commit, edits, cold wrapping, loading данных, bytes allocated и whole-frame costs не заявлены покрытыми. Table включает fresh slice caller'а на видимую строку.
- Уже существующие render/terminal guards (`alloc_guard_test.go`, frame/scroll/unchanged-byte тесты) переиспользуются: canvas paint, unchanged diff, reused-scratch encode, idle Present = 0 alloc, unchanged frame = 0 wire bytes. Они отдельно перепроверены в этом срезе; новые byte-тесты не дублируются.
- Локальные гейты: `go vet ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./ui ./render ./internal/terminal`, gofmt и golangci-lint — PASS. Cross-host/toolchain — только после push-CI verdict, не по локальному PASS.
- Уточнены устаревшие подписи плана: per-present CPU p95 уже существует с slice 35; это не preparation-only p95. Фактический TextArea уже имеет dirty-range wrap-cache (content invalidation, width/mode/policy), поэтому новый generic cache не строится без профиля.

**Этап 5, Phase timing — preparation-only метрики кадра (slice 49):**

- `lux.Metrics` получил фазовые поля: `UpdateTime`, `ViewTime`, `DiffEncodeTime`, `EncodeTime`, `FlushTime`, `PreparationTime` (= Update+View+DiffEncode+Encode, без flush, scheduling, parsing и callbacks) и `BackendTiming` (фазы backend'а известны только встроенному `terminal.Backend` через внутренний seam `PresentWithTiming`; внешний Backend получает `false` и нулевые backend-фазы, а не выдуманные значения). `PresentTime` теперь включает annex I/O своего кадра.
- `Frame.OnPresentedWithMetrics(func(Metrics))` отдаёт метрики именно этого кадра по значению; dropped/failed кадры колбэк не вызывают. `Frame.Metrics` — последний подтверждённый кадр, но `TargetFPS`/`DroppedFrames` остаются live и не идут назад: RED-регрессия незавершённого diff'а (подтверждение старого in-flight кадра откатывало `DroppedFrames` к 0) закреплена `TestFrameMetricsDroppedFramesNeverDecrease`; мутант (копия метрик без live-перезаписи) пойман: `went backwards at view 8: [0 0 0 0 1 2 3 4 0 6 ...]`.
- `TestPresentedMetricsBelongToExactFrameAndExcludeFlush` (legacy и timed backend, sentinel Flush = 1 s не попадает в preparation) и `internal/terminal` `TestPresentWithTimingSeparatesPreparationFromFlush` фиксируют разделение фаз.
- Benchmarks lux-lane пишет `frame_prep_us` через `OnPresentedWithMetrics` (только при `BackendTiming`, сброс в `resetCounts`); отчёт/benchstat получили `prep p50/p95/p99 (us)` / `prep_us_per_frame_p50/p95/p99` рядом с process-CPU распределением; tea/tview — `—`. Тесты: `TestLuxLanePrepSamplerWindow`, расширенный `TestRebuildOutputsRegeneratesArtifacts`.
- Локальные гейты: gofmt, `go vet ./...`, `go test -count=1 ./...`, `go test -race -shuffle=on -count=10 . ./internal/terminal`, golangci-lint (root 0 issues), кросс-сборки linux/darwin/windows/freebsd amd64+arm64, nested e2e/07-ssh/benchmarks `GOOS=linux go vet`. Linux-only тесты benchmarks в этом срезе только скомпилированы (`go test -c`), не выполнены: локальные Docker/WSL недоступны — их подтверждает push-CI.
- Это измерительный seam, не закрытие бюджета: фактический p95 подготовки 120×40 на reference flows, phase-профиль сложных сцен, long-session bounded memory и итоговый regression report остаются открыты.

**Этап 5, Phase profile — reference flows и p95 подготовки без flush (slice 50):**

- `internal/terminal.ProfileBackend` — in-memory интерактивный backend: каждый кадр diff-encode'ится как у настоящего `Backend` (TrueColor, synchronized output, scroll regions), байты отбрасываются, flush = 0; следующий scripted event выдаётся только после Present предыдущего кадра (pacing не склеивает кадры). `internal/phaseprofile`: `Run` (реальный runtime + `OnPresentedWithMetrics`), nearest-rank `Percentile`, `Benchmark` с метриками `prep-p50/p95/p99-us`, `update/view/encode-p95-us`, `bytes/frame`, `frames/op`, `clock-step-us`. Тесты: каждый scripted event дал кадр, `FlushTime == 0`, `PreparationTime` = сумма фаз, nearest-rank на 1..20, clock step в (0, 100ms].
- `BenchmarkPreparation120x40` в 08-tour, 09-procs, 14-workspace, 15-jobs — scripted reference flows (страницы/оверлеи tour, 500 строк procs с фильтром, editor+palette workspace, log-follow/фильтры jobs).
- Профиль нашёл hotspot: `Canvas.Fill`/`Clear` — 43% CPU procs (каждая ячейка заново сегментировала и мерила тот же графем через `Put`). Fill меряет графем один раз; для single-cell графема видимый span строки заливается slice'ом, а через per-cell wide-cell repair идут только две крайние ячейки. Эквивалентность: `TestFillMatchesPerCellReference` (4000 рандомизированных буферов с wide-ячейками, subview, clip; эталон — старый per-cell цикл), оба мутанта (без левого/правого края) пойманы. `BenchmarkCanvasFill`: Clear 120×40 821→22 µs −97.3%, clipped panel 274→5 µs −98.1%, wide fill 697→150 µs −78.5% (p=0.002, n=6), 0 allocs.
- Чередующийся A/B reference flows (3×10, i5-10400F, Windows Go 1.25.5, 120×40), p95/p99 ms: tour 2.01→1.04 / 3.00→1.30; procs 2.15→1.01 / 3.14→1.13; workspace 1.67→0.51 / 2.11→0.67; jobs 1.39→0.51 / 2.02→0.91. bytes/frame без изменений. Бюджет 8 ms p95 на reference flows выполнен с запасом.
- Ограничение измерения: на Windows-хосте шаг `time.Now` 0.5–1 ms — фазы короче шага читаются 0, p95 — верхняя оценка, квантованная шагом (отсюда метрика `clock-step-us`). Абсолютные числа зависят от нагрузки хоста (при 88% CPU до-фикс p95 был 4.5–16 ms) — сравнивать только чередующиеся прогоны; Linux-прогон даёт sub-µs шаг.
- Поведение byte-identical (эквивалентность по ячейкам) — CHANGELOG не затронут. Остаются long-session bounded memory и итоговый regression report.

**Этап 5, Long-session bounded memory на reference flows (slice 51):**

- `phaseprofile.Soak` гоняет модель через N повторов цикла событий (один event на presented кадр) и после каждого завершённого цикла снимает live heap после `runtime.GC()` и число горутин. `CheckBounded` падает, если heap от конца warmup до конца прогона вырос больше slack или выросли горутины. Тест механизма: модель, удерживающая 1 KiB на клавишу, даёт 40 samples и рост ≥ 70 KiB; counter-модель проходит `CheckBounded`.
- `TestLongSessionMemoryIsBounded` в 08-tour (60 циклов: все страницы, focus, оверлеи), 09-procs, 14-workspace (ввод/стирание строки, palette, undo history до своего cap) и 15-jobs (по 150 циклов); каждый цикл ещё и ресайзит 100×30 → 160×50 → 120×40. Рост после warmup: tour +21 KB / 49 циклов, procs +54 KB / 129, workspace +9 KB / 109, jobs +30 KB / 129; горутины 4 → 4 (`.verification/slice51-soak.txt`).
- Остаточный рост разобран heap-профилем (MemProfileRate=1, diff циклов 50 и 600 procs): test double `scriptedSource.filterLog`, строки приложения, сами samples, `runtime.malg` и pprof; framework-владельцев (очереди, кэши, history) в нём нет, темп падает (~250 → 140 B/цикл). Negative control: утечка 64 B на event в `applyEvent` ловится во всех четырёх тестах (+240…293 KB при slack 128 KB).
- Soak — тысячи кадров с pacing 240 FPS (15–25 s на приложение), поэтому opt-in через `LUX_SOAK=1`: обычный и `-race -count=25` прогон examples их пропускает, CI-job `soak` (ubuntu + windows, Go stable, без race — инструментация искажает heap) запускает их явно.
- Публичный API не менялся — CHANGELOG не затронут.

**Этап 5, итоговый regression report (slice 52, закрывает этап 5):**

- [PERFORMANCE.md → Stage 5 regression report](PERFORMANCE.md#stage-5-regression-report): каждый первичный бюджет сопоставлен с evidence — static UI, 120×40 prep p95, interactive reaction, animation, virtual data, bounded memory, regression; все выполнены.
- Interleaved A/B всей micro battery (`render`, `ui`, `internal/terminal`, `scene3d`): `66aefa6` (до slice 36) против конца этапа, 6 rounds (`.verification/slice52/`). Значимые дельты только улучшения: CanvasFrame −56%, TableDraw −54% (B/op −38%), SelectDrawClosed −56%, TreeDraw −43%, Renderer/160x48 −18%; allocs/bytes без регрессий.
- Ложная тревога `SelectDrawOpen` (+449%, allocs 1→9) разобрана: в baseline бенчмарк забывал `Open: true` и мерил закрытый Select; open против open — −61.63% (p=0.002), allocs 9 = 9.
- Свежий профиль reference flows: prep p95 1.0–1.8 ms при шаге часов ≈1 ms — в бюджете 8 ms.
- Код и публичный API не менялись.

**Gate:** before/after profiles и повторяемые цифры; snapshots и error paths не ухудшились; никакого «оптимизировано» без проверки конечного сценария.

## 10. Этап 6 — дизайн-workbench и reference applications

**Результат:** хорошие дизайны можно не только увидеть, но и быстро повторить.

### Tour как ежедневный инструмент

- Каталог всех controls и их состояний, а не только happy path.
- Переключение theme/density/reduced-motion и preview capability profiles.
- Небольшая diagnostic область: focus/target, frame budget, allocations (только в diagnostic режиме), dimensions/policy.
- Отличать реальную capability от preview/simulated profile. Graphics enum не выдавать за реализованный image adapter.
- Детерминированное время/данные в tests; cell snapshots сравнивают text + styles + caret + geometry, не только plain strings.
- Saved fixtures + ограниченный визуальный проход в реальных hosts; не бесконечный цикл косметических переделок.

### Workbench catalog, slice 43

- `08-tour` / Widgets / F2: добавлен editing stand Tree + Select + TextArea, без новой top-level вкладки, public API/dependencies. Старый каталог сохранён; переключатель виден в footer/help и работает из focused editor, но не сквозь modal scope.
- 80×24: дерево над picker/editor; 120×40 и 240×80: дерево рядом с редактором. Для слишком маленькой области — enlargement hint; zero-size не ждёт несуществующего Present и восстановление сохраняет drafts.
- Два in-memory samples с независимыми draft/caret/scroll/history, MaxBytes 16KiB / UndoBytes 32KiB. Tree expand/open; Select preview/Enter commit/Escape+outside cancel+focus trap/restore; TextArea editable/read-only/disabled/empty, multiline paste и undo/redo. Никаких disk writes или обещаний настоящего filesystem editor.
- Committed-routing tests через существующий probeBackend: beacon после Context.End commit, последовательный causal ack на каждый input; callback не пишет model. Fixtures 80×24/120×40/240×80: exact text positioning, hash всех body cells (grapheme/width/style/link), representative cells, caret/focus/geometry. Hidden decoration не планирует animation; tiny/zero/restore проверены. Это cell/backend evidence, не ручной live-terminal host pass.
- Открыто после slice 43: theme/density/reduced-motion switches (закрыты slice 44 ниже), simulated capability previews и diagnostics (закрыты slice 45 ниже), loading/error каталога (закрыт slice 47 ниже); реальные project/config и jobs/log flows ниже всё ещё обязательны. Stand не подменяет reference application и не закрывает stage 6 целиком.

### Workbench settings, slice 44

- F3 открывает отдельный app-owned screen theme/density/reduced-motion из любого stand, в том числе редактора; F3/Escape восстанавливает исходный focus/caret/draft. Open picker/help сначала закрываются; settings не открывает nested help и не пропускает скрытые page-local Space shortcuts.
- Per-tour palette без mutable color globals + существующий Context.UseTheme: dark/light/terminal-default/high-contrast. Explicit chrome styles читают ту же palette; focus отличается underline, selected — marker/bold. Terminal-default settings не задаёт fg/bg; демонстрационные gradients/3D materials остаются собственными данными, не обещанием monochrome emulation.
- Compact/comfortable: bounded body gutters, дополнительная высота только при наличии; geometry/редактирование проверены на 80×24/120×40/240×80. Local reduced motion объединяется OR с runtime policy; decorative schedule гасится, progress policy сохраняется. Preferences session-local, disk persist не вводится.
- Committed input-to-frame tests сохраняют public Backend seam, frame callback не пишет Model. Новые saved settings fixtures всех трёх размеров содержат четыре темы, text/styles hashes/focus/caret; старые catalog fixtures проходят без регенерации. Это cell evidence, не manual live-host pass.
- Следующее: полноценный jobs/log reference; project/config flow и tour loading/error catalog закрыты ниже. Stage 5 profiling/prep-only p95/long-session memory и весь stage 7 остаются открыты.

### Workbench inspection, slice 45

- F4 открывает app-owned compact inspection поверх текущего stand через единственный modal scope. F4/Escape возвращает исходный stand, editor focus/caret/draft; while inspection is open скрытые decorative/page-local shortcuts и scheduling подавлены, Ctrl+Q остаётся доступен.
- Diagnostics показывают immutable snapshot последнего committed `ui.Action`: source focus, target, handled, `Action.Bounds`, viewport; callback не мутирует model. `Context.End`/`OnPresented` causal barrier не подменён простым фактом `Backend.Present`.
- `Frame.Capabilities` показаны как negotiated actual facts; рядом явно помечены четыре `SIMULATED capability profiles` (Monochrome/ANSI16/ANSI256/TrueColor). Preview labels не изменяют backend capabilities и не заявляют terminal protocol/image adapter support.
- Metrics используют существующие `Frame.Metrics`: target FPS, total `PresentTime`, dropped frames, с явной подписью, что Present включает backend I/O и не является preparation-only latency. Нового public API и speculative region introspection не добавлено.
- Causal tracer `TestWorkbenchInspectionReturnsToEditor` проверяет committed route/geometry, simulated-versus-actual copy, hidden caret, no hidden schedule и exact restoration. Tour loading/error catalog закрыт slice 47; полноценные project/config + jobs/log reference flows остаются обязательными; Stage 5/7 и v1.0 gates не закрыты.

### Loading/error catalog, slice 47

- `examples/08-tour` / Widgets / F2 получил существующий Tree leaf `data states`; отдельная top-level вкладка и новые public API не нужны. Выбор leaf показывает deterministic in-memory state-preview с кнопками Loading, Ready, Empty, Error и Retry.
- Состояния явно различаются текстом и action affordance: Loading объясняет, что это preview request state; Ready показывает доступные rows; Empty — успешный ответ без rows; Error — permission-denied copy; Retry переводит Error обратно в Ready. Это не backend request и не claim о capability/protocol support.
- RED→GREEN tracer `TestWidgetsCatalogShowsLoadingErrorAndRetryStates` переиспользует causal `probeBackend`: F2 → Tab → Tree navigation → Enter, проверяет все labels, затем выбирает Error и Retry и проверяет diagnostic copy/recovery. Старый compact reachability test сохранён.
- Fixtures 120×40 и 240×80 регенерированы только после intentional layout change; `go test ./examples/08-tour -count=1` и targeted tracer зелёные. User-facing README/CHANGELOG обновлены; project/config и jobs/log flows, Stage 5/7 и v1.0 gates остаются открыты.

### Project/config workspace, slice 46

- `examples/14-workspace` добавляет runnable reference flow поверх реального filesystem source: список файлов, async loading, ready/empty/error/retry states и чтение выбранного файла. `LUX_WORKSPACE_ROOT` задаёт root; default — текущий каталог.
- `config.lux` получает минимальную validation boundary (`name = ...`, положительный `workers = ...`); Ctrl+P показывает unsaved read-only preview, Ctrl+S валидирует и сохраняет, Esc защищает dirty draft через один modal scope. Ctrl+S/Ctrl+R не обходят modal; Escape в modal оставляет редактирование.
- Writes staged в том же каталоге, `Sync` и replace; Windows fallback делает backup/restore. Source отклоняет path traversal. Deterministic seam tests проверяют loading/recovery, validation, preview, unsaved protection; filesystem tests проверяют replacement и отсутствие backup leak.
- User-facing guide: `examples/14-workspace/README.md`; root catalog и changelog обновлены. Push CI run `38748929728` на `4a6d629` завершён success, 21/21 jobs (lint, fuzz, vuln, root/nested/race/PTy/SSH lanes и cross-build). Это реальный project/config reference, но не заменяет jobs/log dashboard и не закрывает Stage 5/7 или v1.0 gates.

### Jobs/log dashboard, slice 48

- `examples/15-jobs` добавляет runnable deterministic jobs/log reference: три job rows, queued/running/succeeded/failed/canceled states, progress steps через существующий `lux.Command`/`event.Message`, bounded history в 80 строк и dropped-line telemetry.
- Follow-tail следует последней строке, ручной `l`/`L` scroll отдаёт tail, `f` возвращает его; `x` отменяет только текущий per-job context, `r` запускает retry. Monotonic request ID и `activeJob` отбрасывают stale result после смены selection.
- RED→GREEN tracer `TestJobsProgressTelemetryRetryAndBoundedHistory` проверяет progress/completion, cap/dropped counts, pause/resume follow-tail, cancellation и retry. Source seam намеренно demo queue; это не claim о transport, persistence или host integration.
- User-facing `README.md`, root example catalog и CHANGELOG обновлены. Slice закрывает jobs/log reference flow, но не Stage 5 profiling/long-session evidence, Stage 7 audit/RC или v1.0 gate.


### Три reference designs

1. **Task/process explorer** — virtual data, async, filtering, master-detail, recovery.
2. **Project/config workspace** — Tree, TextArea, diff/preview, form validation, unsaved changes.
3. **Jobs/log dashboard** — progress, live telemetry, follow-tail, cancel/retry, latency/slow-link mode.

Первое приложение строится ещё на этапе 2; остальные добавляются по мере проверки соответствующих patterns. Каждое достаточно небольшое для изучения, но выполняет настоящую задачу.

Извлекать общий code после повторения, не создавать scaffolding CLI раньше одного реально пригодного starter. Starter — обычный runnable Go example, понятные theme/layout/Update/View, commands для cmd/PowerShell/Unix, включая `cd /d` в cmd.

**Gate:** новый экран собирается из существующих recipes; theme/density меняются без переписывания state; examples компилируются/тестируются; screenshots не скрывают compact/failure states.

## 11. Этап 7 — RC и v1.0

**Результат:** стабильный контракт для выбранных сценариев, а не бесконечное обещание «всех фич».

- Аудит публичного Interface: zero-value semantics, nil/invalid inputs, concurrency/cancellation ownership, stable IDs, width policy, frame lifetime.
- Compile-check примеров старой версии + migration guide для minor breaks.
- Нужные API changes закончены; несколько RC итераций и хотя бы две независимые реальные app flows не требуют новых breaking fixes.
- Release checks root + nested modules, PTY + реальные Windows/macOS/Linux hosts в заявленной матрице.
- Длительная нагрузка на логи/редактор/resize/slow link: нет framework-induced роста bounded queues/caches/history и потери terminal state.
- Документы support/version/security/matrix согласованы с фактическим release; user-facing документация — English, внутренний план — Russian.
- Reproducible benchmark report, changelog, tagged artifact, checksums для demo binaries если они распространяются.
- Нет известных неисправленных P0/P1 в заявленных сценариях; непокрытые платформы/IME/RTL/native screen reader остаются explicit limits.

**Этап 7, аудит nil/zero-value публичного API (slice 53):**

- `internal/apiaudit`: reflection-sweep вызывает каждый экспортированный метод и каждую функцию пакетов `lux`, `capability`, `event`, `layout`, `motion`, `render`, `scene3d`, `ui` на receiver'ах `T{}`, `&T{}` и `(*T)(nil)`, с nil- и zero-аргументами. Таймаут на вызов — 2 s. Value-методы через nil-указатель пропускаются: паника там языковая, и метод не может её поймать. `TestSweepReportsPanicsAndHangs` — negative control: пойманы и паника, и зависание.
- Нашлись две дыры. Zero values чисты.
  - Nil `*ui.Context` паниковал в `Region`, `Focused`, `Handle`, `Begin`, `End`, `ScrollViewport` и др. Из-за этого `Button`, `List` и другие интерактивные виджеты падали на непустом прямоугольнике, `Gauge` и `Spinner` работали, а `Input` и `TextArea` молча ничего не рисовали.
  - `(*lux.Frame)(nil).RequestFrame` и `RequestFrameAfter` падали, хотя остальные методы `Frame` nil-safe.
- Правило одно: nil `*Context` ничего не делает. Он не маршрутизирует события, запросы возвращают false/0, виджеты рисуются без регистрации. Guard'ы стоят в общих точках входа (`RegionWithOptions`, `Focused`, `revealDelta`, `focusPending` и т.д.), поэтому лишние проверки в `Input`/`TextArea` удалены.
- Покрытие: `TestNilContextPaintsWithoutRouting` (все виджеты на реальном canvas) и `TestSurfaceSweep` как постоянная проверка в `go test ./...`. Мутанты отловлены: без guard в `RegionWithOptions` падает widget-тест, без guard в `RequestFrameAfter` — sweep.
- Изменение поведения для nil-случаев внесено в CHANGELOG в раздел Fixed. Сигнатуры не менялись.

**Этап 7, migration guide и согласование docs (slice 54):**

- `docs/MIGRATION.md` (v0.1.0 → next minor): правка исходников не нужна. Доказательство: `examples/` из v0.1.0 без изменений собраны в scratch-модуле с `replace` на текущее дерево; build и vet проходят, тесты примеров зелёные, SSH-пример проходит vet под Linux. Дифф `go doc -all` между v0.1.0 и HEAD содержит только добавления. Отдельно перечислено то, что всё-таки может задеть: позиционные литералы структур с новыми полями, новое значение `event.Kind`, пять изменений поведения и новый API.
- В CHANGELOG `[Unreleased]` сведён к одному блоку на каждую категорию (Added/Changed/Fixed/Performance). Ни один bullet не потерян, это проверено assert'ом. Добавлена ссылка на migration guide.
- В SECURITY.md формулировку «pre-release» заменила таблица поддержки: `main` и последний `0.x` minor получают патчи, старые minor — нет.
- COMPATIBILITY.md: статусы «validation pending / planned validation» заменены на «matrix-validated» с версиями, и только там, где в `.verification/matrix/` (прогон 2026-10-09) есть пройденный лейн. Строка SSH/mosh разделена: mosh матрицей не покрыт и остаётся planned. ConHost отмечен как validated headless: интерактивное окно не покрыто.
- README: в блок статуса добавлена строка про unreleased-работу на `main` и ссылки на migration guide.

**Этап 7, аудит concurrency/cancellation/frame lifetime (slice 55):**

- Контракт был в коде, но в docs описан только частично. `Model` обновляется и рисуется последовательно на runtime-горутине. `Frame` валиден только во время `View`. `OnPresented*` вызываются на presentation-горутине и не должны трогать модель. Команды живут на своих горутинах, `Run` их не ждёт: на любом пути возврата он отменяет их ctx, а результат, пришедший после возврата, отбрасывается (`runCommand` делает select по `ctx.Done`).
- Godoc `Command` теперь называет «не ждёт / отменяет / отбрасывает». Godoc `ui.Context` говорит «используйте там же, где модель»: виджеты мутируют state и canvas вызывающего, поэтому mutex внутри Context — не обещание concurrent-safety. README дополнен абзацем про async, отмену и lifetime.
- `TestRunDoesNotWaitForCommands`: 100 команд, игнорирующих отмену (больше буфера результатов на 64), и одна, ждущая `ctx.Done`. `Run` возвращается сразу, у команды есть cause, поздние результаты до модели не доходят, число горутин возвращается к baseline. Мутант (голый `results <- next` без select) пойман: горутины застревают.

**Gate v1.0:** доказанная стабильность выбранного контракта + production evidence + usable recipes. «Нет никогда никаких багов» не является обещанием.

## 12. Порядок релизов и первые десять slices

### Релизные ориентиры

- **v0.1.1:** security/correctness patches, tour hotfixes, calibrated measurements; без публичных breaking changes.
- **v0.2.x:** hardening CI/lifecycle, подтверждённый application-event seam, первая real app и semantic styling improvements. Все breaks перечислены.
- **v0.3.x:** reusable composition/UX recipes, полноценный workbench, два-три reference flows, performance gates.
- **v1.0.0:** после RC/matrix/stability gates, не по искусственной календарной дате.

### Очередь старта

1. Минимальный OSC 8 regression + fix + legitimate controls.
2. Benchmark counters: unified measurement window и denominators.
3. Benchmark causal latency и calibration fixtures, затем новый report.
4. Tour routing: Space, legacy Ctrl+Q, raw inspector; regression через public Backend.
5. Tour compact/clip/scroll и controls reachability.
6. Motion continuity + actual reduced motion + event-driven scheduling.
7. Table/WidthPolicy/overlay-caret boundary tests и fixes.
8. E2E CI и blocked-write/signal lifecycle checks.
9. Первая async reference app: результат/ошибка/retry/cancel/stale.
10. Semantic palette/state recipes + второй использующий их экран, затем вынос общего API.

Каждый slice: минимальный failing check → fix/feature → end-to-end sample → snapshot/behavior/perf check → docs → отдельный небольшой commit. Security fixes не должны ждать окончания дизайн-работ.

## 13. Что сознательно не входит

Существующая optional stage 11 не запускается этим планом: [demand gate](<ROADMAP.md#L408-L429>) сохраняется.

- Нет GPU runtime, CSS/Yoga/DOM/React-подобного framework или новой retained-mode системы.
- Нет Kitty/Sixel/iTerm image adapters, WASM, agent JSON transport и native a11y/IME bridges без отдельного измеренного спроса и плана.
- Keyboard access, visible focus, mono/high-contrast и reduced motion обязательны уже сейчас. Это не обещание screen-reader accessibility full support.
- Нет drag/drop, charts zoo, custom widgets ради списка возможностей.
- Нет universal scheduler/cache/theme registry/form DSL до подтверждённого повторения/профиля.
- Нет чтения OSC 52 clipboard и скрытых side effects без consent.
- Нет обещания screen pixel-identical результата: шрифт, terminal palette и renderer принадлежат host; гарантируется содержимое/геометрия/state, а visual experience проверяется на заявленной матрице.

## 14. Производственный цикл дизайна экрана

1. Назвать задачу и основное действие; определить реальные объёмы данных.
2. Выбрать одну composition recipe; сначала keyboard flow и compact режим.
3. Нарисовать hierarchy через spacing/labels/attributes; restrained semantic color.
4. Добавить empty/loading/error/disabled и cancel/retry paths.
5. Проверить focus, editor shortcuts, overlay/caret и small/large viewport.
6. Применить theme/density/reduced-motion, mono/ANSI fallback.
7. Измерить реальный workload и устранить только доказанные bottlenecks.
8. Один пакетный visual/behavior review → исправления → один confirm pass; зафиксировать fixtures.

**Итоговая стратегия:** надёжность и измерения → полезное приложение → единый язык дизайна → повторяемые UX-паттерны → профильная оптимизация → стабильный релиз. Красивый tour должен быть доказательством качества, а не ширмой поверх отсутствующих production-сценариев.
