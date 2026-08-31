# Рекомендательная записка: verdict и scoring в AIQA

## Решение

**AIQA не следует показывать один общий процент как равноправный спутник итогового verdict.** Для conformance-отчёта основной результат — категориальный вердикт, а числа должны описывать разные измерения:

1. **Core conformance** — насколько выполнен минимальный обязательный профиль.
2. **Optional capability coverage** — какая доля дополнительных возможностей подтверждена.
3. **Evidence coverage** — для какой доли требований вообще получен однозначный результат.

Итоговое решение от 2026-09-01: удалить `Score` из Markdown, JSON и schema без переходного legacy-поля. Проект находится в alpha, и поддержка потребителей старого процентного поля не требуется.

Причина уже материализовалась в приложенных отчётах:

* Qwen: `PARTIAL`, `76.5%`, при этом обязательные проверки пройдены `8/8`; результат состоит из 26 pass, 1 fail, 7 unsupported и 1 skip.
* Vibecode: `FAIL`, `77.1%`, потому что обязательные проверки пройдены только `7/8`; результат состоит из 27 pass, 6 fail и 2 unsupported.

Число `77.1%` визуально обещает превосходство над `76.5%`, хотя категорический вывод говорит обратное. Процент здесь попросту измеряет другую ось, но притворяется общим качеством — маленькая метафизическая диверсия в одной колонке.

До реализации canonical assessment прежняя реализация:

* считает `score = pass / (pass + fail + warn + unsupported)`;
* исключает из знаменателя `skip` **и `error`**;
* считает любой required-результат кроме `pass` и `skip` как `required_fail`;
* следовательно, required `error` превращается в `FAIL`, а required `skip` может вообще не помешать `PASS`;
* добавляет результаты внешнего checker в те же счётчики, что и нативные проверки, поэтому добавление ещё одного инструмента само по себе меняет score.

Это расходится с заданной семантикой: `ERROR` должен означать неопределённость, а `SKIP` — отсутствие проверки, но никак не доказательство совместимости.

---

## 1. Что делают существующие стандарты и инструменты

| Конвенция                                                                                                                                                                              | Представление результата                                                                                                               | Числовой показатель                                                                                                    | Что взять AIQA                                                                    | Ограничение аналогии                                                                                                                 |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| [TAP 14](https://testanything.org/tap-version-14-specification.html)                                                                                                                   | `ok` / `not ok`; отдельные `SKIP` и `TODO`. Failing SKIP/TODO не считается обычным failure                                             | Harness может показать pass rate, но verdict остаётся `FAILED`; спецификация буквально приводит `FAILED … 50.00% okay` | Процент является диагностикой, а не заменой verdict; skip нельзя смешивать с fail | TAP не определяет required/optional compatibility profiles ([testanything.org][1])                                                   |
| [JUnit Platform](https://docs.junit.org/6.1.1/api/org.junit.platform.engine/org/junit/platform/engine/TestExecutionResult.html) и [xUnit XML v2](https://xunit.net/docs/format-xml-v2) | JUnit разделяет `successful`, `failed`, `aborted`; xUnit отдельно считает test failure, skipped/not-run и environment error            | Обычно показываются counts, а не единый score; запуск завершается ошибкой при failed tests                             | `ERROR`/aborted следует отделить от доказанного incompatibility                   | Это test-runner conventions, не стандарт API conformance ([docs.junit.org][2])                                                       |
| [W3C EARL](https://www.w3.org/TR/EARL10-Schema/)                                                                                                                                       | `passed`, `failed`, `cantTell`, `inapplicable`, `untested`                                                                             | Универсальный процент не предписан                                                                                     | Почти прямое соответствие: pass, fail, inconclusive, N/A, skip                    | EARL — словарь результатов, а не методика агрегирования API-профиля ([W3C][3])                                                       |
| [W3C WCAG conformance](https://www.w3.org/WAI/WCAG22/Understanding/conformance)                                                                                                        | Уровень conformance достигается только при выполнении **всех** критериев уровня; прогресс по более высоким уровням сообщается отдельно | Не усредняет обязательные и дополнительные критерии в одну оценку                                                      | Лучший прецедент для `Core conformance` отдельно от optional progress             | Предметная область — accessibility, а не API, но структура профиля подходит очень хорошо ([W3C][4])                                  |
| [OpenID Foundation Conformance Suite](https://openid.net/certification/connect_rp_testing/)                                                                                            | Certification определяется статусами полного профиля; `FAILED` и `INTERRUPTED` блокируют сертификацию                                  | Сертификационного score нет                                                                                            | Профиль — hard gate; counts и warnings остаются доказательной частью              | Допустимость `WARNING`/`SKIPPED` определяется именно политикой OpenID, её нельзя буквально перенести в AIQA ([OpenID Foundation][5]) |
| [MDN Browser Compat Data](https://github.com/mdn/browser-compat-data/blob/main/docs/data-guidelines/README.md)                                                                         | Feature-by-feature matrix; есть отдельный `partial_implementation`                                                                     | Нет одного «browser compatibility score»                                                                               | Optional-возможности естественнее показывать как capability matrix                | Нет единого verdict всего browser/runtime ([GitHub][6])                                                                              |
| [Schemathesis](https://schemathesis.readthedocs.io/en/latest/reference/cli/) и [Postman](https://learning.postman.com/docs/monitoring-your-api/viewing-monitor-results)                | Schemathesis разделяет failed check и aborted run; Postman отделяет failed assertion от network/TLS/runtime error                      | Counts и exit status; единой conformance-оценки нет                                                                    | `ERROR` не должен доказывать отсутствие или несовместимость capability            | Не определяют required/optional и модельную applicability ([schemathesis.readthedocs.io][7])                                         |
| [OpenTelemetry trace status](https://opentelemetry.io/docs/specs/otel/trace/api/)                                                                                                      | Status хранится отдельно от метрик и событий                                                                                           | Метрики агрегируются независимо от status                                                                              | Полезный архитектурный принцип: verdict и количественные сигналы — разные поля    | Это observability convention, не conformance standard ([OpenTelemetry][8])                                                           |

Для определения самих уровней требований подходит терминология [IETF BCP 14 / RFC 2119](https://www.rfc-editor.org/rfc/rfc2119): обязательное `MUST/REQUIRED` и действительно необязательное `MAY/OPTIONAL`. Но RFC 2119 не задаёт scoring — оно только помогает строго сформировать профиль. ([RFC Editor][9])

**Вывод обзора:** ни один из рассмотренных первичных источников не предлагает универсальную формулу weighted conformance score. Повторяющийся паттерн — **hard-gated verdict + раздельные counts/coverage**. TAP допускает процент рядом с verdict, но прямо демонстрирует, что `50% okay` всё равно является `FAILED`. ([testanything.org][1])

---

# 2. Рекомендуемая модель AIQA

## 2.1. Оценивать requirements, а не количество probes

Сейчас нативные проверки и проверки official SDK попадают в общий score как самостоятельные равновесные единицы. Это делает показатель нестабильным:

* добавили новый checker — изменился score;
* разбили один тест на три — изменился score;
* сделали retry отдельным result — изменился score;
* сама совместимость endpoint при этом могла вообще не измениться.

Правильная структура:

```text
profile
  └── canonical requirement
        ├── native probe
        ├── official SDK probe
        └── schema/semantic probe
```

Например:

```json
{
  "requirement": {
    "id": "chat.streaming.sdk-parseable",
    "level": "optional"
  },
  "subject": {
    "kind": "model",
    "id": "qwen3.8-flash"
  },
  "status": "fail",
  "evidence": [
    {
      "probe_id": "official-sdk.go.chat_completions_stream",
      "status": "fail",
      "summary": "stream chunk missing choices"
    }
  ]
}
```

Каждый canonical requirement учитывается в coverage **один раз**. Probes являются доказательствами, а не голосами на выборах совместимости.

Если wire-level SSE и official-SDK parseability считаются двумя самостоятельными обещаниями профиля, это должны быть два явно названных requirement:

```text
chat.streaming.sse-valid
chat.streaming.openai-go-sdk-parseable
```

А не два случайно равных балла.

---

## 2.2. Точные статусы отдельных проверок

| JSON          | Markdown | Точное значение                                                           | Для core requirement                                                              | Для optional requirement                              |
| ------------- | -------- | ------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | ----------------------------------------------------- |
| `pass`        | `PASS`   | Ожидаемое поведение доказано                                              | Requirement выполнен                                                              | Capability подтверждена                               |
| `fail`        | `FAIL`   | Проверка исполнена и выявила несовместимое поведение                      | Overall `FAIL`                                                                    | Overall не выше `PARTIAL`                             |
| `unsupported` | `UNSUP`  | Optional capability явно отклонена подходящим 4xx                         | Для core не использовать: записывать `fail` с `failure_kind=required_unsupported` | Capability gap, но не defect                          |
| `warn`        | `WARN`   | Проверка выполнена, но семантический эффект или корректность не доказаны  | Overall `INCONCLUSIVE`                                                            | `PASS-WITH-GAPS`                                      |
| `skip`        | `SKIP`   | Проверка не выполнялась                                                   | Overall `INCONCLUSIVE`                                                            | `PASS-WITH-GAPS`; снижает evidence/execution coverage |
| `error`       | `ERROR`  | Runner/network/provider/infrastructure error; результата о capability нет | Overall `INCONCLUSIVE`                                                            | `PASS-WITH-GAPS`; не считать unsupported или fail     |

`N/S` в Markdown лучше заменить на **`UNSUP`**. `N/S` читается и как “not supported”, и как “not specified”, и как «не спрашивай». В JSON стабильное `unsupported` остаётся без изменений.

Ещё одно полезное правило:

* Если проверка фактически прошла, но есть некритичное замечание, использовать `status: "pass"` и отдельный массив `warnings`.
* `status: "warn"` резервировать для **неопределённого результата**, а не для «pass, но я решила добавить жёлтого цвета».

В следующей major-схеме полезно добавить:

```text
not_applicable / N/A
```

Такой результат исключается из знаменателей. Он особенно важен для distinction между:

* gateway-level requirement;
* model-level capability;
* requirement, который к этой модели вообще не применим.

W3C EARL ровно для этого разделяет `inapplicable`, `untested` и `cantTell`. ([W3C][3])

---

## 2.3. Итоговый verdict

Рекомендую расширить набор итоговых verdict новым значением `INCONCLUSIVE`:

```text
if any core requirement is FAIL:
    FAIL

else if any core requirement is WARN, SKIP, or ERROR:
    INCONCLUSIVE

else if any optional requirement is FAIL:
    PARTIAL

else if any optional requirement is UNSUP, WARN, SKIP, or ERROR:
    PASS-WITH-GAPS

else:
    PASS
```

Семантика:

| Verdict          | Значение                                                                                                  |
| ---------------- | --------------------------------------------------------------------------------------------------------- |
| `PASS`           | Все применимые core и optional requirements подтверждены                                                  |
| `PASS-WITH-GAPS` | Core полностью подтверждён; optional capabilities отсутствуют или не получили достаточного доказательства |
| `PARTIAL`        | Core полностью подтверждён, но хотя бы одна optional capability была проверена и повела себя несовместимо |
| `FAIL`           | Минимальный OpenAI compatibility profile нарушен                                                          |
| `INCONCLUSIVE`   | По core profile недостаточно надёжных данных для PASS или FAIL                                            |

Ключевое исправление: **optional `ERROR` больше не означает `PARTIAL`**. Ошибка сети не доказывает, что API частично несовместим. Она доказывает лишь то, что в этот раз боги TCP не подписали акт приёмки.

`INCONCLUSIVE` не следует помещать между `FAIL` и `PARTIAL`. Это не уровень качества, а отсутствие полного вывода.

---

# 3. Рекомендуемые числовые меры

Пусть:

```text
Rc = число применимых core requirements
Pc = число core requirements со status=pass

Ro = число применимых optional requirements
Po = число optional requirements со status=pass

R  = Rc + Ro
K  = число requirements с однозначным результатом:
     pass + fail + optional unsupported

X  = число фактически запущенных requirements:
     R - skip
```

`not_applicable`, если появится, предварительно исключается из `Rc` и `Ro`.

## Формулы

```text
core_conformance_percent =
    100 × Pc / Rc
```

```text
optional_capability_coverage_percent =
    100 × Po / Ro
```

```text
evidence_conclusive_percent =
    100 × K / R
```

```text
execution_percent =
    100 × X / R
```

Если optional requirements в профиле отсутствуют, `optional_capability_coverage_percent` должен быть `null`, а не искусственными `100%`.

### Почему `WARN` не получает половину балла

Половина балла выглядит математично, но означает произвольное убеждение, что неопределённость равна 50% совместимости. Это не следует ни из протокола, ни из статистики. `WARN`, `ERROR` и `SKIP` должны влиять на **evidence coverage**, а не получать выдуманную долю pass.

---

# 4. Предлагаемые JSON-поля

Текущий `aiqa.report.v1` хранит raw counts и независимые assessment measures без общего score:

```json
{
  "summary": {
    "pass": 26,
    "fail": 1,
    "warn": 0,
    "unsupported": 7,
    "skip": 1,
    "error": 0,
    "required_pass": 8,
    "required_fail": 0,
    "status": "partial",

    "assessment": {
      "verdict": "partial",
      "verdict_reason": "optional_failure",

      "core": {
        "total": 8,
        "pass": 8,
        "fail": 0,
        "warn": 0,
        "skip": 0,
        "error": 0,
        "conformance_percent": 100.0
      },

      "optional": {
        "total": 27,
        "pass": 18,
        "fail": 1,
        "unsupported": 7,
        "warn": 0,
        "skip": 1,
        "error": 0,
        "capability_coverage_percent": 66.7
      },

      "evidence": {
        "total": 35,
        "executed": 34,
        "conclusive": 34,
        "execution_percent": 97.1,
        "conclusive_percent": 97.1
      },

      "comparison": {
        "policy": "verdict-core-optional-evidence/v1",
        "rankable": true,
        "verdict_rank": 1
      }
    }
  }
}
```

Для conclusive verdict:

```text
FAIL            rank 0
PARTIAL         rank 1
PASS-WITH-GAPS  rank 2
PASS            rank 3
```

Для `INCONCLUSIVE`:

```json
{
  "rankable": false,
  "verdict_rank": null
}
```

Рекомендуемый comparison tuple:

```text
(verdict_rank, core_conformance, optional_coverage, evidence_coverage)
```

Сначала сравнивается verdict, и лишь внутри одной категории — числовые измерения.

Текущая JSON Schema не содержит `score`, `score_method` или `score_deprecated`. Вложенный `assessment` является авторитетным результатом, а raw `status` и counts остаются диагностикой.

---

# 5. Расчёт на двух примерах

Расчёты ниже используют текущие raw test entries из приложенных отчётов. После введения canonical requirements и устранения дублирования probes абсолютные количества могут измениться, но метод останется тем же.

## Пример A: `PARTIAL`

Исходные данные:

```text
26 pass
1 fail
7 unsupported
1 skip
8/8 required passed
```

Всего requirements:

```text
26 + 1 + 7 + 1 = 35
```

Core:

```text
core pass  = 8
core total = 8

Core conformance = 8 / 8 = 100.0%
```

Optional:

```text
optional total       = 35 - 8 = 27
optional pass        = 26 - 8 = 18
optional fail        = 1
optional unsupported = 7
optional skip        = 1

Optional capability coverage =
18 / 27 = 66.7%
```

Evidence:

```text
conclusive = pass + fail + unsupported
           = 26 + 1 + 7
           = 34

Evidence coverage = 34 / 35 = 97.1%
Execution         = 34 / 35 = 97.1%
```

Verdict:

```text
Core полностью пройден
+ есть optional fail
= PARTIAL
```

Удалённый показатель из старого отчёта:

```text
26 / (26 + 1 + 7) = 76.5%
```

Именно такую противоречивую картину показывал старый Qwen report; текущий формат этот процент не выводит.

---

## Пример B: `FAIL`

Исходные данные:

```text
27 pass
6 fail
2 unsupported
0 skip
7/8 required passed
```

Core:

```text
core pass  = 7
core fail  = 1
core total = 8

Core conformance = 7 / 8 = 87.5%
```

Optional:

```text
optional total       = 35 - 8 = 27
optional pass        = 27 - 7 = 20
optional fail        = 6 - 1 = 5
optional unsupported = 2

Optional capability coverage =
20 / 27 = 74.1%
```

Evidence:

```text
conclusive = 27 + 6 + 2 = 35

Evidence coverage = 35 / 35 = 100.0%
Execution         = 35 / 35 = 100.0%
```

Verdict:

```text
Есть core fail
= FAIL
```

Удалённый показатель из старого отчёта:

```text
27 / (27 + 6 + 2) = 77.1%
```

Это соответствует старому Vibecode report; текущий формат этот процент не выводит.

### Что теперь видно без обмана

| Пример | Verdict   |       Core | Optional | Evidence |
| ------ | --------- | ---------: | -------: | -------: |
| A      | `PARTIAL` | **100.0%** |    66.7% |    97.1% |
| B      | `FAIL`    |  **87.5%** |    74.1% |   100.0% |

Второй endpoint проверен несколько шире и лучше поддерживает optional-возможности, но минимальный профиль он не прошёл. Это вполне содержательное сравнение. Его не нужно насильно превращать в единую кашу из процентов.

---

# 6. Предлагаемая Markdown-таблица

```markdown
## Summary

| Endpoint | Overall | Core conformance | Optional coverage | Evidence | Pass | Fail | UNSUP | Warn | Skip | Error |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `qwen3.8-flash` | PARTIAL | 8/8 (100.0%) | 18/27 (66.7%) | 34/35 (97.1%) | 26 | 1 | 7 | 0 | 1 | 0 |
| `gpt-5.4-mini` | FAIL | 7/8 (87.5%) | 20/27 (74.1%) | 35/35 (100.0%) | 27 | 6 | 2 | 0 | 0 | 0 |

Comparison policy: Overall → Core conformance → Optional coverage → Evidence.
Percentages describe separate dimensions and are not an overall compatibility score.
```

Удалённый pass-rate не следует сохранять даже в collapsed block: его присутствие продолжает приглашать к сравнению несовместимых verdicts по одному числу.

Колонку `Score` из первой таблицы следует убрать. Именно её близость к `Overall` заставляет читателя воспринимать два противоречащих друг другу сигнала как одну оценку.

---

# 7. Жизнеспособная альтернатива: verdict-banded index

Когда dashboard или API-клиент непременно требует **одно sortable number**, можно дать ему policy-specific `compatibility_index`. Но не называть его процентом conformance.

Сначала рассчитывается внутреннее качество:

```text
C = core_conformance / 100
O = optional_coverage / 100

q = 0.8 × C + 0.2 × O
```

Core имеет 80% веса, optional — 20%. Эти веса не являются стандартом; это явная AIQA policy, которую необходимо закрепить в версии профиля.

Затем `q` помещается в непересекающийся verdict band:

| Verdict          | Диапазон index |
| ---------------- | -------------: |
| `FAIL`           |       0.0–49.9 |
| `PARTIAL`        |      50.0–74.9 |
| `PASS-WITH-GAPS` |      75.0–99.9 |
| `PASS`           |          100.0 |
| `INCONCLUSIVE`   |         `null` |

Формула внутри band:

```text
index = band_min + (band_max - band_min) × q
```

### Пример A

```text
q = 0.8 × 1.0 + 0.2 × 0.6667
  = 0.9333

PARTIAL index =
50 + (74.9 - 50) × 0.9333
= 73.2
```

### Пример B

```text
q = 0.8 × 0.875 + 0.2 × 0.7407
  = 0.8481

FAIL index =
0 + 49.9 × 0.8481
= 42.3
```

Теперь любой `FAIL` математически ниже любого `PARTIAL`.

### Почему это только альтернатива

Недостатки banded index:

* `73.2` не означает «73.2% требований выполнено»;
* границы диапазонов и веса — политика AIQA, а не стандарт;
* одинаковая разница в 5 пунктов имеет разный смысл в разных bands;
* `INCONCLUSIVE` всё равно нельзя честно ранжировать.

Поэтому рекомендованный публичный интерфейс — **verdict + отдельные measures**. Banded index пригоден только для внутреннего dashboard, сортировки или трендов.

Обычный weighted score **без verdict bands** проблему не решает: при другом количестве core/optional требований endpoint с core failure снова может обогнать endpoint с пройденным core.

---

# 8. Итоговый план реализации

Переходный legacy cycle не нужен. Текущий alpha contract:

1. Не содержит `score`, `score_method` или `score_deprecated`.
2. Использует `summary.assessment.verdict` как авторитетный результат.
3. Хранит core, optional и evidence measures отдельно.
4. Сохраняет raw status counts как диагностические данные.
5. Показывает `UNSUP`, `SKIP`, `ERROR` и `N/A` как разные состояния.
6. Добавляет к результатам:

   ```json
   {
     "requirement_id": "chat.tools.forced",
     "requirement_level": "optional",
     "subject": {
       "kind": "model",
       "id": "qwen3.8-flash"
     },
     "probe_id": "aiqa.native.chat.tools.forced"
   }
   ```

Будущий `aiqa.report.v2` может переместить authoritative verdict на верхний уровень или изменить представление requirements, но не должен возвращать единый numeric score.

## Обязательные regression properties

```text
required FAIL              => verdict всегда FAIL
required UNSUP             => verdict всегда FAIL
required ERROR/WARN/SKIP   => verdict всегда INCONCLUSIVE
optional UNSUP             => никогда не FAIL и не PARTIAL
optional ERROR             => никогда не доказывает incompatibility
optional FAIL              => verdict не выше PARTIAL
добавление duplicate probe => не меняет requirement coverage
любая FAIL-запись          => не ранжируется выше любой PARTIAL-записи
```

---

# Итоговая рекомендация

В основной части отчёта оставить:

```text
Overall
Core conformance
Optional capability coverage
Evidence coverage
Raw status counts
```

Отчёт не содержит:

```text
Score: 76.5%
```

Ключ сравнения:

```text
verdict → core → optional → evidence
```

Это соответствует наиболее устойчивой практике conformance suites: **обязательный профиль является gate, optional progress показывается отдельно, а ошибки выполнения не маскируются под несовместимость**. AIQA не поддерживает weighted или legacy overall score.

[1]: https://testanything.org/tap-version-14-specification.html "https://testanything.org/tap-version-14-specification.html"
[2]: https://docs.junit.org/6.1.1/api/org.junit.platform.engine/org/junit/platform/engine/TestExecutionResult.html "https://docs.junit.org/6.1.1/api/org.junit.platform.engine/org/junit/platform/engine/TestExecutionResult.html"
[3]: https://www.w3.org/TR/EARL10-Schema/ "https://www.w3.org/TR/EARL10-Schema/"
[4]: https://www.w3.org/WAI/WCAG22/Understanding/conformance "https://www.w3.org/WAI/WCAG22/Understanding/conformance"
[5]: https://openid.net/certification/connect_rp_testing/ "https://openid.net/certification/connect_rp_testing/"
[6]: https://github.com/mdn/browser-compat-data/blob/main/docs/data-guidelines/README.md "https://github.com/mdn/browser-compat-data/blob/main/docs/data-guidelines/README.md"
[7]: https://schemathesis.readthedocs.io/en/latest/reference/cli/ "https://schemathesis.readthedocs.io/en/latest/reference/cli/"
[8]: https://opentelemetry.io/docs/specs/otel/trace/api/ "https://opentelemetry.io/docs/specs/otel/trace/api/"
[9]: https://www.rfc-editor.org/rfc/rfc2119 "https://www.rfc-editor.org/rfc/rfc2119"
