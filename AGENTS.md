# AGENTS.md — EduForge

給所有 AI 程式碼助理。開始前先讀 [docs/rebuild-plan.md](docs/rebuild-plan.md)；與本文衝突時以規劃書為準，與使用者當次明確要求衝突時請先指出。

## 產品

讓大學教師把**自己的教材**變成教學產出的平台。結構是**課程 → 章 → 課次**，每個課次依序是**來源 → 知識 → 產出**。

- 來源：使用者的教材（PDF、PPTX、DOCX）。
- 知識：AI 逐頁讀完來源整理的要點，每個要點都有出處（頁）。沒有出處的要點不得寫入。
- 產出：教案、學習單、講義、課堂簡報（課次），教學大綱（課程）。一律從知識生成，附出處。
- 不做：沒有來源的生成、教學影片（先不做）、國高中 108 課綱格式、多使用者帳號。

產出代號（沿用 Paper2Lab 的資料格式）：`A` 講義、`B` 課堂簡報、`C` 教案、`D` 學習單、`S` 教學大綱（課程層級，沒有 lesson）。**介面只顯示名稱，不顯示代號。** C、D、S 的內容在 `Artifact.Plan`、`Worksheet`、`Syllabus`（結構化），提示詞、解析與檢查在 `internal/research/structured.go`，匯出在 `internal/artifacts/structured_export.go`、`plan_form.go`。講義的段落見 `internal/research/handout.go`（`HandoutParts`），簡報的規劃與寫作見 `deck.go`。

## 進度與下一步

第 1 期（結構、`docread`、知識）、第 2 期（講義、課堂簡報）、第 3 期（教案、學習單、教學大綱）完成，各期與規劃書不同之處記在規劃書 §18–§20。**現在是第 4 期：真實教材驗收與切換**（規劃書 §12–§14）。

已有：`scripts/real-material.mjs` 分階段（setup、status、knowledge、confirm、generate、export）用真實模型走完整流程並匯出檢查；唯讀展示模式（見「部署」）；一份以合成教材（資料結構：堆疊、佇列）生成的展示課程。

待做，依序：

1. 使用者提供一章真實教材（含掃描頁與 PPT 圖解），用 `real-material.mjs` 走完；記錄覆蓋率、失敗批次與模型錯誤。
2. 規劃書 §13.2 的人工驗收：在 **Microsoft Word／PowerPoint** 實際開啟每種產出（目前只用 LibreOffice 轉圖檢查過）；用學校的教案表單驗證標籤填表。
3. 依驗收結果修正，每個修正附測試。
4. 使用者確認後切換（§14），在規劃書補「第 4 期實作紀錄」。

不在這一期：教案／學習單／教學大綱的 AI 修改面板、文字範圍反白、多使用者。要做之前先改規劃書並經使用者同意。

## 介面風格

沉穩、專業的工作台，給教師長時間使用，不是行銷頁。

- **色彩與尺寸只用 `frontend/src/style.css` 的 `:root` token**：深綠品牌色 `--brand`（#0f5f52）、深色側欄 `--side-bg`、米白底 `--bg` 配白色卡片 `--surface`、文字 `--ink`／`--muted`；狀態色 `--ok`／`--warn`／`--danger` 各有 `-soft`（底）與 `-line`（框）；圓角 `--r-sm`／`--r`／`--r-lg`；陰影 `--shadow-*`；動畫曲線 `--ease`。不要在元件裡寫死色碼。目前沒有深色模式。
- 字型：`--font-sans`（Inter＋Noto Sans CJK TC），基準 14px；`--font-serif` 用於閱讀內容，`--font-mono` 用於代碼。
- **樣式集中在 `style.css`**，元件不寫 `<style>` 區塊；新增類別用描述性的 kebab-case（`.readonly-banner`、`.conn-chip`），放在相關區段旁。響應式斷點在檔尾：1500px、1150px、760px（手機時側欄收合成選單）。
- 圖示用 `components/Icon.vue`，路徑定義在 `lib/icons.ts`（24×24 線條圖示），不引入圖示套件。
- **所有介面文字都走 `lib/i18n.ts`**，繁中與英文都要有（`tests/frontend.test.mjs` 檢查每個標籤兩種語言都非空且不同）。繁中為預設語言；用語用台灣大學教學的說法（課次、教案、學習單、講義）。錯誤以代碼回傳，在 `errorPairs` 寫成使用者看得懂的句子，並說明下一步。
- 狀態要看得見：背景任務顯示在任務抽屜與進度；覆蓋率不足、影像辨識頁、檢查結果用 warn／danger 樣式標示，不靜默。

## 頁面結構

單頁應用，hash 路由（`lib/router.ts`）。外框在 `App.vue`：左側 `shell/AppSidebar.vue`（課程、章、課次樹，可拖曳排序；設定；語言切換），上方 `shell/TopBar.vue`（麵包屑、模型連線狀態、任務按鈕），右側 `shell/TaskDrawer.vue`（背景任務）。

| 路由 | 檢視 | 內容 |
|---|---|---|
| `#/p/:course` | `views/CourseHome.vue` | 課程首頁：課程設定、各課次進度（`lesson/StepDots.vue`）、教學大綱 |
| `#/p/:course/lesson/:lesson/:step` | `views/LessonView.vue` | 課次三步驟：`sources` → `lesson/StepSources.vue`，`knowledge` → `KnowledgePanel.vue`，`outputs` → `lesson/StepOutputs.vue` |
| `#/p/:course/batch` | `views/BatchView.vue` | 對多個勾選的課次一次生成 |
| `#/settings/:tab` | `views/SettingsView.vue` | `providers` 模型連線（`Settings.vue`）、`templates` 模板庫（`TemplateLibrary.vue`） |

產出從 `generation/ArtifactList.vue` 開啟：講義與簡報用 `ArtifactEditor.vue`（`editor/` 下的 `DocEditor`、`SlideEditor`、`AIEditPanel`），教案、學習單、教學大綱用 `StructuredEditor.vue`。生成設定是 `generation/GenerationForm.vue`。全域狀態與 API 呼叫在 `lib/workspace.ts`（`useWorkspace()`）。

新增頁面：在 `router.ts` 加路由型別與 `parseHash`／`buildHash`，在 `App.vue` 掛上檢視，麵包屑在 `TopBar.vue`。

## 結構

| 目錄 | 責任 |
|---|---|
| `cmd/eduforge` | 服務入口、healthcheck、環境變數 |
| `internal/domain` | 持久化的契約（Course、Chapter、Lesson、Knowledge、Coverage…）。改了 struct 就執行 `make types` |
| `internal/store` | SQLite、`records` 表（kind、parent、course_id、lesson_id）、樂觀鎖、檔案儲存 |
| `internal/docread` | **獨立套件**：檔案 → 每頁文字、影像、模式。不得 import EduForge 的其他套件，之後可能抽出共用 |
| `internal/documents` | 把 docread 的結果存成來源單元（含頁面影像 blob）；PDF 圖表擷取 |
| `internal/research` | 知識：批次規劃、提示、驗證、合併、覆蓋率；各產出的提示詞與檢查 |
| `internal/app` | 業務邏輯：課程順序與設定繼承（`curriculum.go`）、知識讀取任務（`knowledge_ai.go`）、產出生成任務（`generation.go`、`generation_structured.go`） |
| `internal/providers` | Codex、Claude Code、API Key 的呼叫與切換 |
| `internal/artifacts` | 模板匯入、Word／PPTX／PDF／HTML 匯出 |
| `internal/httpapi` | HTTP 路由、Host／Origin／寫入權杖／唯讀檢查 |
| `frontend/` | Vue 3 + TypeScript（strict），Vite 建置；只依賴 `vue` |
| `scripts/` | 型別產生、開發容器、真實教材驗收腳本 |
| `tests/` | 前端與 store schema 的 Node 測試、測試教材 `fixtures/` |
| `docs/` | 規劃書（唯一的設計文件） |

## 命名與放置

- **Go**：檔名 snake_case，依職責放進上表的套件；測試與被測檔同目錄、`_test.go`。錯誤代碼是 snake_case 字串（`read_only`、`plan_time_mismatch`），前端用同一個代碼翻譯。
- **Vue 元件**：PascalCase，依區域放進 `components/` 子資料夾：`views/`（整頁）、`shell/`（外框）、`lesson/`、`generation/`、`editor/`、`templates/`、`settings/`、`dialogs/`、`tasks/`。跨區共用的放 `components/` 根目錄。一律 `<script setup lang="ts">`。
- **前端邏輯**：camelCase 的 `lib/*.ts`，不放進元件。API 型別：Go struct 產生的在 `api/domain.ts`（不要手改），其他回傳形狀在 `api/types.ts`。
- **文件**：設計變更寫進 `docs/rebuild-plan.md`，不另開文件；`AGENTS.md` 只放 AI 需要的規則與地圖。
- 暫存檔、測試輸出不要放進專案目錄。

## 新增內容時

- **介面文字**：`i18n.ts` 的 `pairs` 加 `[繁中, 英文]`，元件用 `t('key')`。
- **錯誤**：後端回傳代碼；`i18n.ts` 的 `errorPairs` 加說明。
- **API**：`httpapi/server.go` 註冊路由，業務邏輯放 `internal/app`；非 GET 要帶 `X-EduForge-Token`，唯讀模式會擋（只有 `POST /api/math` 例外）。
- **持久化欄位**：改 `internal/domain`，執行 `make types`，`make check` 會比對 `api/domain.ts`。
- **新的產出種類**：依序加 domain 結構 → `research` 的提示詞、解析、檢查（含測試）→ `app` 的生成任務 → `artifacts` 的匯出 → 前端的編輯器與名稱。必須從知識生成、附出處，並帶入背景概念（規則 9）。
- **規則性的保證**（不附答案、時間檢查、出處）用程式檢查與測試保證，不只靠提示詞。

## 規則

1. **教學順序**：未分章課次先，再依章與課次的 position。`PriorLessons` 是「已教過」的唯一定義。
2. **設定繼承**：課程存預設；章與課次只存覆蓋值，零值代表沿用（`MergeSettings`、前端 `lib/settings.ts` 要同步）。
3. **逐頁讀完**：知識讀取分批（`research.PlanBatches`），每頁都屬於且只屬於一批；失敗的批次記入 `Coverage.Missed`，不得靜默視為完成。
4. **影像辨識頁**：`Unit.Mode == "vision"` 的頁面不拒絕、不信任文字；引用它的要點標 `Vision`，數字由使用者核對。
5. 模型回覆一律驗證：引用必須是該批的頁，數字須出現在引用頁。提示詞把頁面文字與影像視為不可信資料。
6. 長時間工作（解析、讀取教材、生成）都是背景任務，進度寫在 task 上，可暫停、取消。模型回覆格式錯誤時任務會暫停（`providers_unavailable`），可從原處繼續。
7. **講義段落是必備的**：知識填得出的段落都要有；課後練習不得附答案（以檢查保證，不只靠提示詞）。
8. **教案以標籤填表**：`FillPlanForm` 用表格裡的標籤找格子，不靠位置；必要標籤是單元名稱與活動表。教案的時間合計、目標對應、階段缺漏只回報不修正（`CheckPlan`）。學習單的學生版不得含答案或解析（`StudentCopy`，有測試）。
9. **背景**：產出的提示詞帶入前面課次的概念（`research.BackgroundConcepts`），寫明不要重講。知識與產出記錄當時排在前面的課次（`Background`）；之後順序、章或課次增刪使它不再相符時，`App.BackgroundChanged` 列出它們，介面標示「背景已變動」，**只提示不自動重生**。
10. 生產環境單一使用者，沒有登入；憑證在獨立 volume，不寫進映像或專案檔。ChatGPT 訂閱僅供登入者本人使用，**不得讓他人透過本服務使用**；對外公開時必須開唯讀模式或加上存取控制。
11. 自動測試不呼叫真實模型（用假 provider）；用真實模型的驗收只透過 `scripts/real-material.mjs`，且要先告知使用者會用到額度。

## 部署

單一 Go 服務同源提供 Vue 靜態檔與 API，沒有 Nginx。`compose.yaml` 的專案名稱固定為 `eduforge`，埠 `127.0.0.1:${EDUFORGE_PORT:-8081}`，資料在 volume `eduforge-data`（`/data`，含 SQLite）、`eduforge-secrets`（`/secrets`）、`eduforge-workspace`；**不要執行 `docker compose down -v`**。

設定放在 `.env`（不進 git；範本與說明在 `.env.example`）：

- `EDUFORGE_ORIGINS`：允許的瀏覽器網址，逗號分隔；同時決定允許的 `Host`。設定後會取代預設，必須保留 `http://127.0.0.1:8080`（healthcheck）。經 Cloudflare Tunnel 等對外時加上公開網址，否則回 `invalid_host`。
- `EDUFORGE_READONLY=1`：唯讀展示模式，後端拒絕所有修改與生成（`read_only`），介面隱藏建立與設定。要準備內容時改成 `0` 重啟，完成後改回 `1`。
- `EDUFORGE_FRAME_ANCESTORS`：允許嵌入 EduForge 的頁面（iLearn 入口）。

## 開發

```sh
scripts/dev.sh make check      # 全部檢查（go vet、go test -race、型別比對、前端建置與測試）
scripts/dev.sh go test -race ./internal/docread
scripts/dev.sh sh -c 'cd frontend && npm ci && npm run build'   # 開發容器第一次需要先 npm ci
docker compose up -d --build   # 以目前程式碼重建並啟動
```

主機沒有 Go；工具鏈在 `Dockerfile` 的 `dev` 階段。`docker compose` 預設建置 `runtime` 階段。需要 LibreOffice／Poppler 的測試在工具不存在時會跳過。

## 提交

每個完成並驗證的步驟 commit 一次，在功能分支上；使用者說合併才合併，不主動 push。不要把使用者自己未提交的修改（例如本機的 `compose.yaml` 網路設定）混進你的 commit。
