# archai retrieval-сервис — implementation plan

> Реализация дизайна из `2026-06-22-code-search-retrieval-service.md`.
> Граница неизменна: **archai = знание о коде** (граф + эмбеддинги + поиск),
> оркестратор (loom) = что делать дальше. Здесь — *как* доделать retrieval в
> archai: ноды → эмбеддинги (brute-force cosine) + BM25 → RRF → graph-expand,
> за новым JSON/MCP API, с freshness поверх существующего model-cache.

## Принятые решения (развилки сняты)

1. **Энкодер — порт `Embedder`.** Дефолт-адаптер: локальный **Ollama** по HTTP
   (без CGo, без секретов на диске, оффлайн). Провайдер абстрагирован — remote
   API подключается позже как ещё один адаптер, ядро его не знает.
2. **chunk == node.** Отдельного чанкера нет. Единица retrieval — нода графа
   (символ). На ноду — предикат `embeddable` (зависит от гранулярности). Текст
   для эмбеддинга = `сигнатура + docstring + тело` (тело читается с диска по
   span'у в момент эмбеддинга, в домене не хранится). Переростки > бюджета в v1
   усекаются (сигнатура остаётся заголовком); AST-sub-chunking внутри ноды —
   отложен, контракт «1 нода = 1 чанк» не меняет.
3. **Dense — brute-force cosine** в памяти. HNSW отложен (на 50k нод × 768 dims
   перебор — единицы мс; зависимость не нужна, пока не упрёмся).

## Что уже есть (подтверждено по коду)

- Граф: `internal/domain` + `internal/adapter/uigraph` — символы (iface/struct/
  func/type/const/var/error), стабильные ID `{PackagePath}.{SymbolName}`, `Doc`
  на ноду, рёбра `uses/returns/implements/calls` (`SymbolRelation`), package-deps
  (`Edge`), статические call-edges из AST (`FunctionDef.Calls`, `MethodDef.Calls`).
- HTTP/MCP-поверхности: `internal/serve` + `internal/adapter/http` +
  `internal/adapter/mcp` (11 тулзов, `POST /api/mcp/tools/call` диспетчер).
- Freshness-фундамент: `internal/serve/model_cache.go` — `.archai/cache/
  go-model.json`, per-file mtime+size, инкрементальный re-parse изменённых
  пакетов; `watcher.go` (fsnotify) → `ReloadPackage` → перезапись кэша.

## Чего нет (строим с нуля)

- Эмбеддингов / vector-store / BM25 — нет. Текущий `search.go` = substring-скоринг,
  пересборка на запрос, **HTML/HTMX-only**, без JSON-API.
- Позиций символов: reader хранит только basename файла; offset'ы/директория
  теряются (`getSourceFile`). Нужны для чтения тела и для `snippet`.
- Per-node freshness: cache работает per-file, не per-node.

---

## Архитектура (Ports & Adapters, как в проекте)

Новый пакет `internal/retrieval` (service-слой) + порты + адаптеры:

```
Порты (interfaces):
  Embedder      Embed(ctx, []string) ([][]float32, error); Dim() int; ID() string
  VectorIndex   Upsert(id, vec); Remove(id); Search(vec, k) []Scored; Save/Load
  LexicalIndex  Upsert(id, text); Remove(id); Search(query, k) []Scored; Save/Load

Адаптеры:
  adapter/embed/ollama    Ollama HTTP (/api/embeddings) — дефолт
  adapter/embed/noop      детерминированный стаб для тестов (хэш→вектор)
  adapter/vindex/brute    in-memory []float32 + cosine top-K + персист в .archai/
  adapter/lindex/bm25     inverted index (термы→ноды) + BM25 + персист

Service:
  retrieval.Service       Index(model) / Refresh(changed) / Search / SearchGraph / Expand / Node
```

- Ядро (service) зависит только от портов и `internal/domain`. Адаптеры зависят
  от домена, не наоборот. Wiring — в `cmd/archai` / `internal/serve` (DI).
- **Graceful degradation:** если `Embedder` недоступен (ollama не запущен) —
  dense отключается, BM25 работает, `/search` возвращает lexical-результаты с
  флагом `dense:false`. Это явный контракт, не падение.

### Retrieval-нода

Источник — `domain.PackageModel` (там есть символы, доки, рёбра для expand). ID —
та же схема, что в uigraph (`{Path}.{Name}`), чтобы node_id совпадал между
`/api/uigraph` и search API.

```go
type Node struct {
    ID         string   // {PackagePath}.{SymbolName}
    Kind       string   // iface|struct|func|type|const|var|error
    Package    string
    Signature  string   // уже собирается (Signature()/display helpers)
    Doc        string
    Span       Span     // file(rel path) + StartByte/EndByte  ← новое в reader
    Embeddable bool      // предикат по гранулярности
}
```

`embedText(node)` = enclosing-заголовок (`package X` + имя типа) + `Signature` +
`Doc` + тело (читается по `Span` с диска, усекается до бюджета). `contentHash` =
hash(embedText) → ключ freshness.

`Embeddable` (v1): `func/method/iface/struct/type` → true; `const/var/error` →
false по умолчанию (конфигурируемо). Предикат изолирован, легко крутить.

---

## Итерации

### Iteration 1 — Span в reader + retrieval-нода + порт Embedder

- **reader:** перестать терять позиции. На символ положить `Span{File string
  (rel path от root), StartByte, EndByte int}` (из `token.Pos`/`fset.Position`).
  Прокинуть в домен-структуры (новое поле, домен — data container).
- **retrieval.Node + builder:** проекция `[]PackageModel` → `[]Node` (reuse
  ID-схемы uigraph; вынести общий helper, чтобы ID не разъезжались). Предикат
  `Embeddable`. `embedText()` + чтение тела по span (+ усечение до бюджета).
- **порт `Embedder` + адаптеры** `ollama` (HTTP, конфиг endpoint+model) и `noop`
  (тесты). Конфиг в `archai.yaml` (`retrieval.embedder.{provider,endpoint,model}`).
- Тесты: span корректен (offset'ы → реальный текст), `embedText` детерминирован,
  предикат, ollama-адаптер за фейковым HTTP.

### Iteration 2 — Dense brute-force + freshness per-node

- **`adapter/vindex/brute`:** `map[id][]float32`, cosine top-K, персист в
  `.archai/cache/vectors.json` (ключ: node_id + contentHash + embedder ID).
- **`retrieval.Service.Index/Refresh`:** при старте/reload эмбедить только ноды
  с изменившимся `contentHash` (поверх уже инкрементального model-cache). Удалять
  вектора пропавших нод.
- **Wiring в `internal/serve`:** в `ReloadPackage`/watcher дёргать
  `Service.Refresh(changedPackages)`. Эмбеддинг — асинхронно, не блокирует reload.
- Тесты: re-embed только изменённого; восстановление индекса из персиста;
  смена embedder ID инвалидирует вектора.

### Iteration 3 — BM25 lexical

- **`adapter/lindex/bm25`:** токенизация `embedText` + split идентификаторов
  (camelCase/snake → подтокены), inverted index, BM25 (k1/b дефолты), top-K,
  персист. Не зависит от Embedder (работает всегда).
- Тесты: ранжирование по identifier-совпадениям, обновление при reload.

### Iteration 4 — Fusion + graph-expand + JSON/MCP API

- **RRF-fusion** dense+lexical в `Search`. `SearchGraph` = `Search` →
  graph-expand (N hops по `SymbolRelation`/call-edges/`Edge`; reuse существующей
  derivation рёбер из uigraph, не дублировать). `Expand` — от заданных node_ids.
- **JSON-ручки** (поверх существующего сервера):
  ```
  POST /api/search        {query,k,filters}      → [{node_id,kind,file,doc,score,snippet}]
  POST /api/search_graph  {query,k,hops}          → {nodes,edges} подграф
  POST /api/expand        {node_ids,hops,edges}   → соседние ноды
  GET  /api/node/{id}                              → тело+рёбра ноды
  POST /api/refresh                                → переиндексация изменённого
  ```
  `snippet` = из sig+doc(+первые строки тела по span).
- **MCP-тулзы**, зеркалящие ручки (`search`, `search_graph`, `expand`, `get_node`,
  `refresh`) — через существующий tool-реестр.
- Тесты: RRF-слияние двух списков, expand на фикстуре-графе, e2e через handlers.

### Iteration 5 — отложено (фиксируем как не-делаем-сейчас)

- AST-sub-chunking внутри переростков (сейчас усечение).
- rerank (cross-encoder на `(query,chunk)`).
- HNSW вместо brute-force.
- remote-embedder адаптер (порт уже готов — добавляется без изменений ядра).

---

## Риски / открытые места

- **Зависимость от запущенного Ollama** — закрыта graceful degradation (BM25-only
  при отсутствии энкодера) + noop-адаптер в тестах.
- **Размер персиста векторов** (50k × 768 × 4B ≈ 150 МБ) — приемлемо для
  `.archai/cache`; при росте сжать (float16) или вынести в mmap — позже.
- **Согласованность node_id** между uigraph и retrieval — закрывается общим
  helper'ом построения ID (iteration 1).
- **Стоимость первичного эмбеддинга** — разовая, инкрементально после; вынести в
  фон, не блокировать serve.
