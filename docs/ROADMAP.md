# LUX TUI — дорожная карта до «идеальной» библиотеки

> Рабочий стратегический документ (октябрь 2026), составлен по итогам полного
> аудита кодовой базы (`59f21a0`, ~11.8k строк Go, 10 пакетов) и свежего среза
> конкурентного ландшафта. Не является обязательством API; до v0.1.0 всё может
> меняться. Язык — русский: репозиторий локальный (remote не настроен), и
> очередь работ живёт здесь. Решение зафиксировано: русский вариант
> коммитится сейчас как рабочий; полный английский проход — gate публикации
> (стадия 10), переводить раньше означало бы поддерживать две копии.

---

## 1. Резюме

LUX TUI уже сейчас обладает ядром, редким в Go-мире:

- корректная **EGC-модель ячеек** (extended grapheme clusters + continuation
  cells) — Bubble Tea v1/Lip Gloss рендерят «строки с ANSI»; v2 догнал
  EGC-корректность (ultraviolet, cursed renderer);
- **переговоры возможностей** (DA1, DECRQM) со безопасным downgrade —
  без exec'а `tput`/terminfo (tcell шёл к этому же в v3, но tview
  остался на v2.8.1);
- **shortest-form диффующий энкодер** + DECSTBM-скролл + (opt-in) mode 2026;
- **absolute-time motion** и adaptive pacing 60→30→20→10 с latest-frame
  backpressure — ни одна Go-библиотека этого не делает целиком;
- immediate-mode UI с **presentation-bound хит-тестингом** и портом
  `Backend`, делающим приложение тестируемым без TTY;
- 3 зависимости, Metrics, plain-output fallback, дисциплина верификации.

Важно честно зафиксировать (свежий срез, октябрь 2026): **Bubble Tea v2
уже закрыл kitty-клавиатуру и EGC-рендер**. Значит, побеждать «первым
kitty-протоколом» нельзя — можно и нужно побеждать на четырёх измеримых
осях: **корректность, совместимость, производительность, DX** — и
доказывать каждую цифрой, которую можно воспроизвести. План ниже построен
как шесть стадий (6–11) поверх закрытых стадий 1–5b, каждая — вертикальные
срезы с привычным ритуалом верификации.

**Порядок обоснован зависимостями:** правда ввода (6) → правда текста (7) →
правда виджетов (8) → правда реальных терминалов (9) → доказанные цифры и
релиз (10) → опциональные дифференциаторы (11). Так каждый следующий срез
опирается на фундамент предыдущего, а не на гипотезу.

---

## 2. Что уже сильное (аудит устоев)

| Область | Состояние на `59f21a0` | У кого из конкурентов так же/лучше |
|---|---|---|
| Юникод-ядро | EGC-ячейки, ширина 0/1/2, `uniseg` (Unicode 15.0), safe last-column | так же: notcurses, vaxis, bubbletea v2 (ultraviolet); хуже: bubbletea v1/lipgloss |
| Цвет | `Color{Default/ANSI/Indexed/RGB}`, деградация TrueColor→256→16→mono в энкодере, `NO_COLOR`, платформенные дефолты | так же: bubbletea (colorprofile), tcell v3; termenv заморожен |
| Диффинг вывода | row-run diff, shortest SGR delta, EL-хвосты, DECSTBM+I/D lines, idle-битсет | ratatui, bubbletea v2 (cursed renderer) — сопоставимо; tview — хуже |
| Переговоры | DA1 + DECRQM 6/2026, probe-only-downgrade, 150 ms бюджет, replay ввода | vaxis, crossterm — сопоставимо; bubbletea v2 — capability-запросы (uv), 2026 по positive-probe; tcell v2 — terminfo/exec (v3 уже отказался, но tview сидит на v2.8.1); tview — нет |
| Runtime | Elm-архитектура, Command, latest-frame, adaptive FPS, idle без таймера, Metrics | нет ни у кого в Go целиком |
| Motion | absolute time, easing, damped spring, ReducedMotion | textual — есть; в Go — нет |
| Immediate UI | Context (focus/capture/modal), EGC-редактор, undo/redo, O(1) virtual scroll | уникально |
| Тестируемость | Backend-порт, plain-output fallback, vtgrid conformance | ни у кого публичного Backend-порта: tcell v3 удалил SimulationScreen (остался внутренний Mock), teatest/golden у Charm — экспериментальные |
| Безопасность ввода | 1 MiB paste cap, sanitization, bounded parser 128 B, fuzz | редкость |
| Дисциплина | .verification-артефакты, ритуал, rollback-репетиции | уникально для solo-проекта |
| Зависимости | 3 шт. (uniseg, x/sys, x/term), чистый Go | минимум на рынке |

Это и есть плацдарм. Ни одна из этих позиций не требует переделки — требуется
**достроить недостающие этажи и доказать превосходство измерениями**.

---

## 3. Пробелы против state-of-the-art (gap analysis)

### 3.1 Ввод (самый болезненный пробел)

| Пробел | Кто уже умеет | Цена отсутствия |
|---|---|---|
| Kitty keyboard protocol (CSI u, flags 0b1…0b10000), progressive enhancement | crossterm/ratatui, vaxis, ultraviolet (bubbletea v2), foot/kitty/wezterm | Esc ждёт 35 ms (escapeDelay-эвристика), Ctrl+I≡Tab, нет release/repeat, нет media keys |
| Клавиши за пределами legacy-набора (F13+, media, caps/num state) | все перечисленные | F13+ приходят как `KeyUnknown` — в шорткатах непригодны |
| Mouse 1003 (all-motion) для hover | все | hover только при зажатой кнопке (сейчас `1002`) |
| Windows-дезамбигуация клавиш (win32-input-mode) | tcell v3, Charm x/input | kitty-протокола на Windows нет — без этого Ctrl+I≡Tab останется и там |
| Переговоры через мультиплексор (DA1 от tmux ≠ от хоста, kitty требует `allow-passthrough`) | vaxis (документировано), bubbletea | слепое включение kitty внутри tmux без passthrough = сломанный ввод |
| Pixel-мышь (kitty) | vaxis, crossterm | точность UI ограничена клетками |
| `event.Key`: нет Press/Release/Repeat-семантики на уровне API | — | заложит долг по API, лучше сделать до v0.1 |

### 3.2 Вывод

| Пробел | Кто уже умеет | Примечание |
|---|---|---|
| OSC 8 hyperlinks | bubbletea, textual, tcell, vaxis | `capability.Hyperlinks` уже есть, но `Cell`/энкодер не эмитят |
| Стили подчёркивания: `4:3/4:4/4:5`, цвет подчёркивания `58`/`59` | textual, vaxis, tcell v3 | нужно расширить `render.Attr`/`Style` |
| Sync output до сих пор opt-in через `LUX_TUI_SYNC_OUTPUT` | bubbletea v2 включает по positive probe (PR #1542) | 5b уже умеет vetoes; осталось включать по probe — направление подтверждено конкурентом |
| Заголовок окна OSC 0/2 с restore-on-exit, уведомления OSC 9/777, нативный прогресс-бар OSC 9;4 | bubbletea (титул чинят до сих пор, #1797), tcell v3 (уведомления) | мелочи для полноты; сделать правильнее с первого раза |
| Графика Kitty/Sixel/iTerm | notcurses, vaxis, textual | capability-enum готов, реализации нет (намеренно, стадия 11) |

### 3.3 Текст и юникод

| Пробел | Кто уже умеет | Примечание |
|---|---|---|
| uniseg = Unicode 15.0 (сейчас актуальнее 16/17) | tcell тянет свежие таблицы | отслеживать rivo/uniseg; при лаге — оценка overlay-таблицы ширины |
| Unicode core mode 2027 (терминал сам меряет ширины EGC) | lipgloss v2.0.6 (Ultraviolet) авто-включает | добавить как capability-gated режим в стадию 7 |
| Configurable width policy (East-Asian Ambiguous, VS15/VS16) | tcell (partial), notcurses | в RESEARCH.md уже заявлено требованием |
| Word wrap / измерение многострочного текста | все | блокирует TextArea и Table |
| Multiline TextArea | все зрелые | **крупнейший виджет-пробел** |
| UAX #9 bidi | textual (partial), prompt_toolkit | честно отложено; не обещать до v0.2+ |
| IME preedit | textual (эксперим.); в Go — никто | отложено сознательно; белое поле (bubbles #361) — кандидат в стадию 11 |

### 3.4 Виджеты

Сейчас: Button, List, Modal, ScrollViewport, Input, Shortcut. Нет: **Table,
Select/Dropdown, Checkbox/Radio, Tabs, Gauge/Progress, Spinner, Sparkline,
Tree, TextArea**. У tview ~20+ контролов, у bubbletea через bubbles ~18.
Догонять «все» не нужно — нужен набор, закрывающий 90% CLI-приложений
(см. стадию 8) + документированный паттерн «свой контрол за вечер».

### 3.5 Платформа и жизненный цикл

| Пробел | Кто уже умеет |
|---|---|
| Suspend/resume (SIGTSTP/SIGCONT, восстановление + repaint) | tcell, bubbletea, tview |
| SIGHUP/закрытие терминала как first-class событие | tcell |
| SSH-адаптер (пример) | bubbletea (gliderlabs), tcell |
| PTY-интеграционные тесты в CI | нет у Go-конкурентов (преимущество, если сделать) |

### 3.6 Инженерия вокруг

- CI: нет кросс-компиляционной джобы, coverage, govulncheck, fuzz-smoke, benchstat-гейта; матрица Go только 1.23.x.
- Нет релизов/тегов/CHANGELOG; модуль не опубликован → нет pkg.go.dev.
- Нет godoc-примеров (`Example*`), нет `examples/` каталога, нет туториалов.
- **Документация местами протухла после 5b**: COMPATIBILITY.md всё ещё
  говорит «No active terminal queries are emitted yet» (стало неверно),
  README «Direction» называет переговоры «следующим шагом». Это первое, что
  видит посетитель.

---

## 4. Конкурентный ландшафт (срез, октябрь 2026; источники — приложение А)

| Библиотека | Версия (окт. 2026) | Состояние | Сильное | Слабое против LUX |
|---|---|---|---|---|
| Bubble Tea (Charm) | v2.0.10 (2026-09-24) | очень активная | kitty-клавиатура, EGC-рендер (ultraviolet), экосистема, доки, звёзды | view = string-композиция; нет motion-слоя/adaptive pacing; большой dep-tree; нет Metrics/plain-вывода как у нас |
| tcell + tview | tcell v3.5.0, tview v0.42.0 | tcell активная; tview — maintenance-режим: один мейнтейнер, 839 открытых issues, без релизов, сидит на tcell v2.8.1 | самый полный каталог контролов (tview), tcell v3: без terminfo, win32-input-mode, styled underlines, уведомления, WASM | tview: старый retained API, нет negotiation, долг; tcell v3 — не батарейки, слом совместимости v2/v3 |
| ratatui (Rust) | v0.29.0 (v0.30.0-alpha.1) | очень активная | техническое SOTA ввода/вывода через crossterm | не Go; нет motion/адаптивного pacing; виджеты — есть |
| libvaxis (Zig) | v0.5.1 | активная | лучший в классе negotiation + kitty | не Go; маленькая экосистема |
| termui, gocui, termbox | termui: спит ~15 мес (на termbox); gocui: тихо, наследников нет; termbox: «somewhat not maintained» | почти неактивны | — | фактически вне гонки |

Стратегический вывод, обновлённый после выхода Bubble Tea v2: Race за
«первым в Go с kitty/EGC» проигран. Ось отрыва — комбинация:

1. **Runtime-слой**: Elm-цикл с absolute-time motion, adaptive pacing,
   latest-frame backpressure, ReducedMotion, Metrics — ни у кого в Go нет.
2. **Фундамент вместо батареек**: 3 зависимости, cell-модель как публичный
   API, presentation-bound immediate UI, plain-output fallback,
   тестируемость без TTY, воспроизводимые бенчмарки с открытой методикой.
3. **White space, подтверждённый ресёрчем (5 шт.)**: a11y, IME/CJK,
   immediate-mode, «tcell-v3-класс без долга tview», опубликованные
   бенчмарки — ни одно из пяти не занято ни одной Go-библиотекой.

Окно возможности сужается (v2 уже закрыл capability-запросы/медиа-клавиши),
поэтому приоритет у стадий 6–10, а не у экзотики (11).

---

## 5. Стратегия: на чём побеждаем

1. **Корректность как продукт.** «Правильный» графемный рендер, правильные
   клавиши, правильная деградация — это то, что чинят у конкурентов issue'ами
   годами. Каждая корректность-фича сопровождается property/fuzz/golden
   тестами и таблицей «терминал → поведение».
2. **Совместимость, проверенная на реальных терминалах.** Не «работает у
   меня», а исполняемая validation-матрица с версиями и артефактами.
3. **Производительность, доказанная публичным бенчмарк-контрактом.**
   PERFORMANCE.md уже определяет сцены/метрики — надо исполнить и
   опубликовать. Контекст ландшафта: **ни одна Go-TUI-библиотека не публикует
   цифр** — Charm заявляет «orders of magnitude» без чисел. Первые честные
   воспроизводимые цифры в экосистеме = готовый дифференциатор. Честно:
   там, где bubbletea быстрее, так и писать.
4. **DX для Go-разработчика.** Runnable-примеры в godoc, `examples/`-приложения,
   туториалы 15 минут, предсказуемый API без глобального состояния.
5. **Идентичность.** Не клонировать lipgloss/CSS/Yoga. Малый, быстрый,
   честный фундамент — «small Go foundation for fast, Unicode-correct
   terminal interfaces».

---

## 6. Стадии 6–11 (каждая = вертикальные срезы + полный ритуал)

Формат среза прежний: задача → реализация → gofmt/vet/test/lint/race/fuzz
парсера-энкодера → кросс-сборки → `.verification/` артефакты →
adversarial-ревью → при необходимости rollback-репетиция → коммит
fix-forward. Названия стадий продолжают нумерацию (после 5b).

### Стадия 5c — «Docs & CI sync» (быстрая гигиена, ~2–4 среза)

Закрывает протухшие места из §3.6 без нового кода:

- COMPATIBILITY.md: переписать параграф о переговорах под 5b-реальность
  (DA1/DECRQM, probe-only-downgrade, `LUX_TUI_PROBE_TIMEOUT`, cost 150 ms,
  «late report → cosmetic event» честность).
- README: обновить «Direction» (переговоры — сделано), добавить раздел
  «Capabilities» с лестницей деградации.
- CI: добавить джобы cross-compile (linux/windows/darwin × amd64/arm64),
  govulncheck, fuzz-smoke (`-fuzztime 30s` по seed'ам), матрицу
  Go `1.23.x + текущий stable`, coverage-артефакт.
- Завести CHANGELOG.md (Keep a Changelog), внести всё сделанное в стадиях 1–5b.

**Приёмка:** ни одного стейтмента в доках, противоречащего коду; зелёный CI
с новыми джобами; `git diff` только docs/CI.

### Стадия 6 — «Keyboard truth» (~4–6 срезов)

Цель: ввод без двусмысленностей на всех терминалах, где это достижимо.
Контекст: после выхода Bubble Tea v2 это не «опережение», а восстановление
паритета — но с нашей дисциплиной: negotiated-downgrade, fuzz, golden-таблица
и без утечки kitty-стека при панике/выходе (баг, который чинили у v2 в
v2.0.9, мы обязаны исключить design'ом).

1. **Kitty keyboard protocol.** Progressive flags (disambiguate → event
   types → alternate keys → all keys as CSI u), запрос `CSI ? u` /
   `CSI > 1 u` push/pop, чистый отказ на legacy-терминалах. Парсинг CSI u
   (unicode codepoint + shift/alt/ctrl modifiers, `;`-encoded). На выходе —
   обязательно pop и fallback-режим.
2. **Семантика событий.** `event.Key` получает `Action`
   (press/release/repeat) с защитой legacy-режима (всегда press). Esc как
   отдельная клавиша vs Alt+X. Ctrl+I≠Tab, Ctrl+M≠Enter при disambiguate.
   F13–F20, media keys, CapsLock/NumLock state (bitfield).
3. **Mouse 1003** (all-motion → hover everywhere) + режим kitty pixel mouse
   опционально, гейт по переговорам.
4. **Стили подчёркивания** (SGR `4:3/4:4/4:5` + `58`/`59` цвет подчёркивания)
   и **OSC 8 hyperlinks**: `render.Cell` получает `Link struct{URI, ID string}`,
   энкодер эмитит по `capability.Hyperlinks`, дифф считает смену ссылки
   частью стиля.
5. Sync output: включение по positive probe (DECRQM 2026 supported),
   `LUX_TUI_SYNC_OUTPUT` остаётся override (force-on/off). Bubbletea v2
   делает так же (PR #1542) — направление уже проверено рынком.
6. **Windows-нога той же правды**: win32-input-mode (расширенный VT-input)
   как Windows-эквивалент дезамбигуации — так делают tcell v3 и Charm
   x/input. Kitty-путь на Windows не работает; честная деградация
   (legacy → понятные ограничения, задокументированные в COMPATIBILITY.md).
7. **Мультиплексоры**: negotiate обязан различать DA1 от tmux и от хоста;
   kitty включать только при подтверждённом passthrough (или query до
   запуска tmux). Негативные тесты «tmux без passthrough → ввод жив».

**Приёмка:** таблица «клавиша × терминал × протокол → событие» (golden-тесты,
прогон на vtgrid + реальные терминалы стадии 9); fuzz CSI u; kitty-режим
даёт Esc немедленно (сегодня — 35 ms escapeDelay), legacy-путь без
регрессий; `presented` фреймы не ломаются при отказе протокола.

### Стадия 7 — «Text truth» (~4–5 срезов)

1. **uniseg → актуальный Unicode** (16/17 при наличии релиза rivo/uniseg;
   если лаг — отдельный срез «width overlay» с таблицей диффов). Это же
   обновляет `THIRD_PARTY_NOTICES`.
2. **Width policy**: `render.WidthPolicy` (Ambiguous=1/2, VS16
   presentation, emoji default), опция на `lux.Run`/Backend. Задокументировать
   поведение терминалов (CJK-локали берут ambiguous=2).
3. **Unicode core mode 2027** (capability-gated, слежение за
   contour-terminal/terminal-unicode-core): где терминал подтверждает режим —
   доверить измерение ширины ему («measures widths the same way the
   terminal does», как Ultraviolet у lipgloss); fallback — наш uniseg.
4. **Word wrap**: `Canvas.TextWrap(rect, text, WrapOptions)` (word/char,
   hyphen-off, preserve EGC), measurement `render.TextMetrics`. Golden-тесты
   на граничные кластеры (ZWJ-эмодзи в точке переноса!).
5. **TextArea** — multiline-редактор: EGC-корректный, soft-wrap из (4),
   вертикальный скролл через существующий ScrollViewport, bounded undo
   (паттерн TextInput), caret placement, selection, paste-санитизация.
   Это самый большой кусок стадии.

**Приёмка:** UAX #29 conformance-прогон; wrap на 64 KiB текста ≤ budget из
PERFORMANCE.md; TextArea выдерживает fuzz-редактирование; пример
`examples/textedit`.

### Стадия 8 — «Widget truth» (~5–8 срезов, растянется)

Порядок — по спросу реальных приложений, каждый срез = контрол + пример +
клавиатурная полнота + фокус-модель:

1. **Table** (виртуализованные строки, колонки с фикс/резиновыми ширинами,
   сортировка хуками, EGC-обрезка колонок) — самый запрашиваемый контрол в
   TUI-мире.
2. **Select/Dropdown** + **Checkbox/Radio** (клавиши, rofi/fzf-подобный
   быстрый фильтр в Select).
3. **Tabs**, **Gauge/Progress**, **Spinner** (последний — готовый потребитель
   motion-слоя, витрина absolute-time).
4. **Sparkline** и **Tree** (последний — виртуализация как у List).
5. Документ «свой контрол за вечер»: паттерн Region+Handle+draw на примере.
6. **Theme**: иммутабельная дефолт-палитра + override через Context —
   единый стиль для всех контролов стадии; никакого глобального
   мутабельного состояния (анти-паттерн `tview.Styles`).

Параллельно runtime-мелочи: заголовок окна — **сделано** (`WithTitle`, OSC 0
со стеком XTWINOPS `CSI 22;0 t`/`CSI 23;0 t`, restore-on-exit с первого
раза и без capability-гейта, потому что у стека нет детекта — честная
матрица в COMPATIBILITY.md; у bubbletea это чинят до сих пор, #1797);
остальное уходит в стадию 9: нативный прогресс-бар (OSC 9;4),
уведомления (OSC 9/777, opt-in — есть и у
tcell v3), clipboard OSC 52 как **опциональный адаптер с consent-гейтом**
(по умолчанию off — текущая позиция в INTERACTION.md сохраняется).

**Приёмка:** каждый контрол — godoc `Example*`; каталог примеров
`examples/widgets`; showcase пополняется живой демкой новых контролов.

### Стадия 9 — «Real-terminal truth» (~4–6 срезов + ручные лейны) — ✅ ЗАКРЫТА

Цель: превратить «implemented, validation pending» в таблицу с версиями.

1. **Suspend/resume** ✅ (срезы 1–2, `25a22cd` + срез 2): SIGTSTP →
   восстановить cooked-терминал → SIGCONT → re-raw + full repaint; на
   Windows — обработка `CTRL_CLOSE`, буфер консоли. SIGHUP/EOF → событие,
   не паника. Реализация A2 (bubbletea-канон): lux никогда не Notify'ит
   `SIGTSTP` — disposition остаётся SIG_DFL, и re-raise по-настоящему
   стопит группу; Ctrl+Z приходит байтом `0x1A` (raw ISIG off) и
   перехватывается run-loop'ом до applyEvent; `WithSuspend(false)`
   отдаёт байт приложению. Alt+u — undo-псевдоним в редакторах на случай,
   если suspend-хендшейк забрал ctrl+z.
2. **Матрица валидации** ✅ (срез 3, `3155b8f`; расширена в срезе 5):
   двенадцать лейнов с самопроверяющимися скриптами и закоммиченными
   артефактами — tmux 3.7c, GNU Screen 5.0.2, pipe/dumb на Linux и
   Windows, CJK- и NO_COLOR-лейны, реальная пара sshd/ssh (`-tt`),
   netem-shaped ssh (200 мс, 512 кбит, strace-бюджет пробы),
   интерактивный scripted Windows Terminal 1.24 (скриншоты), headless
   conhost lifecycle, Windows pipe, examples/ssh-сервер под реальным
   OpenSSH-клиентом в tmux, и annex-лейн (raw-wire правда OSC
   9;4/9;777/52 + поведенчески декодированный OSC 52 через tmux
   `set-clipboard`). Непокрытое честно вынесено в таблицу «Not
   validated, on purpose» (macOS/iTerm2/kitty-хосты, drawing-терминалы
   для 9;4/9;777) с путём закрытия в README матрицы.
3. **SSH-адаптер** ✅ (срез 4, `d55cf17`): `examples/ssh` (свой go.mod) —
   gliderlabs/ssh + creack/pty, дизайн B: движок LUX в child-процессе на
   настоящем pty, SSH-слой — чистая байтовая сантехника; ноль изменений
   библиотеки. Resize → `TIOCSWINSZ`, exit-status туда-обратно,
   disconnect убивает process group. README модуля — дизайн и caveats.
4. **PTY-интеграционные тесты** ✅ (срез 2): модуль `e2e/` (свой go.mod,
   linux/darwin) — creack/pty + скриптованные сессии: 4 теста (байтовая
   правда выхода, job control с живым shell-оракулом `Stopped`, replay
   клавиш против реального kernel input buffer, double-stop discard).
   CI-подключение (ubuntu-lane) — на стадии релиза. Ни один Go-конкурент
   этого не публикует — доказуемое преимущество. Факты, вскрытые
   реализацией: (a) осиротевшие process group'ы DISCARD'ят kernel stop
   сигналы — потому нужен shell-лейн; (b) Go 1.21+ runtime глотает
   kill-посланный TSTP после Notify→Reset — потому A2; (c) Linux флашит
   master input при закрытии последнего slave-fd — child держит pty
   150 мс после выхода; (d) SIGCONT сбрасывает отложенные stop-сигналы —
   double-stop безопасен.
5. **Сайдбенд-каналы** ✅ (срез 5, `093aaab` — перенесено сюда из стадии 8):
   OSC 9;4 (прогресс: 5 состояний, проценты 0–100, leave всегда
   закрывается сбросом `9;4;0` после pop заголовка), OSC 9;777
   (уведомления: поля санитизированы), OSC 52 (clipboard: write-only,
   read-направление не строится никогда, 32 KiB cap). Все opt-in
   (`WithProgress`/`WithNotifications`/`WithClipboard`), едут на кадре
   (Present → annex → OnPresented), проваленный/дропнутый кадр роняет
   свои annex'ы, нулевой `&Frame{}` не ставит ничего в очередь, дефолтная
   сборка байт-в-байт неотличима. Consent многослойный: терминальный
   гейт (kitty prompt, tmux `set-clipboard`) остаётся решением терминала.
   Annex-лейн: raw pipe capture (byte truth) + tmux buffer (behavioral).

**Приёмка:** таблица матрицы заполнена до последней клетки либо клетка
честно помечена «not validated» с путём закрытия (выполнено: README
матрицы + таблица «Not validated, on purpose»); suspend/resume
закреплён на реальном PTY-харнессе Linux (e2e-модуль, macOS-лейн
остаётся на CI стадии релиза); сайдбенды закреплены raw-wire и
поведенческим доказательством.

### Стадия 10 — «Prove it» (~4–5 срезов)

1. **Бенчмарк-контракт**: отдельный модуль `benchmarks/` (свой go.mod —
   зависимости конкурентов НЕ загрязняют ядро!). Адаптеры сцен 1–10 из
   PERFORMANCE.md на: lux, bubbletea+lipgloss, tview+tcell; ratatui-лейн
   через crossterm (Rust-харнесс, опционально). Метрики: p50/p95/p99
   frame-time, байты/кадр, аллокации, idle CPU, resize convergence.
   Прогон — чередующиеся раунды, медианы (урок stage-5 про drift!),
   выкладка сырых JSON + benchstat + методика + спецификация машины.
   Машина — выделенная/закреплённая (не общий CI-раннер), один хост для
   всех раундов; ОС зафиксировать в методике.
   Контекст: это будут **первые опубликованные воспроизводимые цифры в
   Go-TUI-экосистеме** — claim Charm «orders of magnitude» чисел не имеет.
2. **README-обновление по фактам**: там, где lux быстрее — сказать и
   сослаться; там, где нет — не сказать. Честность как фича.
3. **Релиз v0.1.0**: prerequisite — org/repo `lux-tui` на GitHub: **имя
   свободно** (проверено по GitHub API 2026-10-07: 404 на user, org и repo)
   — клеймить в начале стадии, не тянуть. Создать org+repo, настроить
   remote, запушить историю, тег, CHANGELOG, issue-шаблоны
   (bug/feature/question), семвер-политика для v0.x («minor = возможные
   ломающие»), pkg.go.dev индексация — godoc уже с `Example*`; проверка
   `go doc` читаемости.
4. **Контент**: 3 туториала (quick-start → анимация → свой контрол),
   `examples/` каталог (01-counter … 07-ssh), vhs-скринкасты README,
   ROADMAP-перевод на английский и коммит (или решение держать internal).

**Приёмка:** тег v0.1.0; pkg.go.dev индексирует; бенчмарк-отчёт
воспроизводим одной командой из `benchmarks/`; в README нет ни одного
невоспроизводимого сравнения.

### Стадия 11 — «Differentiators» (опционально)

Гейт «по измеренной потребности» конкретен: тегированные issues/запросы
после v0.1.0, спрос на showcase-приложения. До появления этого сигнала —
не начинать (кроме slice-прототипов в research-ветках).

- **Agent-driven mode (headless)**: Backend-порт + plain-output уже дают
  управление без TTY; кандидат — режим «JSON-события наружу, ввод внутрь»
  для AI-агентов. Спрос-сигнал подтверждён: Charm строил v2 под агентов
  (Crush, Plural на bubbletea v2) — см. docs/go-tui-landscape-2026.md.
- **Графика**: Kitty/Sixel/iTerm-путь для scene3d при positive probe, иначе
  half-blocks (лестница уже спроектирована в capability.Graphics).
- **A11y-мост**: announcer-порт (focus-change → текст для скринридера),
  семантические роли контролов. Проверено (окт. 2026): **в Go-мире пусто**
  — у bubbletea #780 и tview #1047 открыты годами без движения. Самое
  чистое white space ландшафта.
- **IME preedit (CJK)**: тоже никем не занято (bubbles #361 открыт 3 года,
  пиньинь-баг); текст исследования — в docs/go-tui-landscape-2026.md.
  Оценивать после стадии 8, только с PTY-харнессом для проверки.
- **WASM**: Backend-адаптер для js/wasm — только если появится реальный
  юзкейс (браузерный терминал-песочница).
- **GPU**: никогда (принцип «no GPU runtime» — часть идентичности).

---

## 7. Анти-цели (что НЕ делать, чтобы не потерять идентичность)

1. Не клонировать lipgloss (string-styling) — наш слой `render`.
2. Не CSS/stylesheet-язык и не Yoga-лайаут — cell constraints + Flex.
3. Не terminfo-exec и не зависимость от tput — только probing.
4. Не «контролы ради количества» — каждый из потребности примера/шейма.
5. Не невоспроизводимые маркетинговые цифры — только контрактом из (10.1).
6. Не GPU/JS runtime в основном модуле — чистый Go, 3 зависимости.
7. Не гнаться за ★ — за корректность; экосистема придёт за честностью.

---

## 8. Definition of Done: «идеальная» в измеримых терминах

| Критерий | Порог | Как измеряем |
|---|---|---|
| Idle-кадр | 0 байт (уже ✓) | encoder test + A/B harness |
| Дифф обычного кадра | ≤ 2× от ratatui на сценах 1–10 | benchmarks/, benchstat |
| Клавиатурная правда | 0 двусмысленностей на kitty-терминалах; legacy без регрессий | golden-таблица стадии 6 |
| Юникод | UAX #29 conformance 100%; 0 broken-кластеров в fuzz | conformance-suite |
| Совместимость | матрица ≥ 12 терминалов, ≥ 90% клеток validated | COMPATIBILITY.md |
| Стартап | ≤ 150 ms probing (или documented off) | negotiation test |
| Зависимости | 3 (uniseg, x/sys, x/term) | go.mod |
| Аллокации в кадре | 0 в steady-state hot path | alloc benchmarks |
| DX | ≥ 15 runnable Example*, 7 examples/, 3 туториала | pkg.go.dev |
| Стабильность | fuzz-parser ≥ 1 h CPU без паник; race-detector чист | ритуал |
| Релиз | v0.1.0 + CHANGELOG + семвер | git tag |

«Лучше конкурентов» фиксируется **только** через эту таблицу и публичный
бенчмарк-отчёт (стадия 10) — по культуре, уже закреплённой в PERFORMANCE.md.

---

## 9. Риски и снятие

| Риск | Вероятность | Снятие |
|---|---|---|
| Ползучий фич-крип виджетов разрушит идентичность | высокая | анти-цели §7; каждый контрол из потребности примера |
| Bubble Tea v2 закрыл kitty-ввод и EGC-рендер (уже случилось, окт. 2026) | высокая | ставки §4: runtime-слой, фундаментальность, честные цифры; скорость стадий 6–10; их view остаётся string-композицией |
| uniseg отстаёт от Unicode 16/17 | высокая | overlay-срез в стадии 7; следить за релизами rivo/uniseg |
| Kitty-протокол тонок (release/repeat) | средняя | staged flags, негативные тесты, чистый pop |
| Время на ручную матрицу | высокая | скриптованные лейны + PTY-автоматизация в CI; ручные — только то, что не автоматизируется |
| Бенчмарк-споры (unfair!) | средняя | открытая методика, сырые данные, интерливинг-медианы, публикация чужых сильных мест |
| Один мейнтейнер | — | срезы ≤ недели, ритуал уже отработан, ROADMAP как очередь |

---

## 10. Механика исполнения (без изменений)

- вертикальные срезы по 1–5 коммитов, fix-forward без amend;
- полный ритуал на каждый срез + `.verification/` артефакты;
- adversarial-ревью каждого среза (зафиксировано в ритуале);
- бенчмарки только интерливингом/медианами (урок stage-5 о ±50% drift под
  OBS/cargo-нагрузкой);
- обновление memory-нод после закрытия стадии.

---

## Приложение А. Конкурентный срез — версии и источники (2026-10-06/07)

Полный отчёт с цитатами/ссылками: [go-tui-landscape-2026.md](go-tui-landscape-2026.md)
(все факты верифицированы по первоисточникам: GitHub API, raw README/CHANGES,
блоги; недоказуемое помечено UNVERIFIED). Краткая выжимка:

| Проект | Версия | Состояние |
|---|---|---|
| [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea/releases) | v2.0.10 (2026-09-24), GA 2026-02-24 | 45.3k★; v2: cursed renderer (ncurses-алгоритм), kitty-клавиатура, positive-probe 2026, `tea.View`-декларативность, OSC 52, inline-режим; цифр производительности не публикует; IME/a11y открыты годами |
| [gdamore/tcell](https://github.com/gdamore/tcell/tags) | v3.5.0 (2026-09-11) | v3 (дек. 2025) выкинул terminfo, legacy-console, termbox; win32-input-mode, WASM, styled underlines, уведомления; v2/v3-раскол (tview сидит на v2.8.1) |
| [rivo/tview](https://github.com/rivo/tview) | v0.42.0 (только теги, релизов нет) | 14.1k★; один мейнтейнер, 839 открытых issues; на tcell v2 |
| [ratatui/ratatui](https://github.com/ratatui/ratatui/tags) | v0.29.0 / v0.30.0-alpha.1 | Rust, очень активная |
| [rockorager/libvaxis](https://github.com/rockorager/libvaxis/tags) | v0.5.1 | Zig; эталон negotiation/kitty |
| termui / gocui / termbox-go | — | termui спит ~15 мес (на termbox); gocui тихо; termbox «somewhat not maintained» |

Дельта к RESEARCH.md (2026-08): выход стабильного Bubble Tea v2.0.x —
kitty-протокол и EGC-рендер больше не дифференциатор; tview глубже в
maintenance-режиме, чем казался. Подтверждённые white space: a11y, IME,
immediate-mode, воспроизводимые бенчмарки. UNVERIFIED в отчёте:
поклеточная матрица адопшена kitty/2026/2027 по терминалам и количественные
Go-TUI-бенчмарки — закрыть матрицей стадии 9 и отчётом стадии 10.

