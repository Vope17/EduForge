# AGENTS.md — EduForge

給所有 AI 程式碼助理。開始前先讀 [docs/rebuild-plan.md](docs/rebuild-plan.md)；與本文衝突時以規劃書為準，與使用者當次明確要求衝突時請先指出。

## 產品

教學內容平台：**課程 → 章 → 課次**，每個課次依序是**來源 → 知識 → 產出**。

- 來源：使用者的教材（PDF、PPTX、DOCX）。
- 知識：AI 逐頁讀完來源整理的要點，每個要點都有出處（頁）。沒有出處的要點不得寫入。
- 產出：教案、學習單、講義、課堂簡報（課次），教學大綱（課程）。一律從知識生成，附出處。
- 不做：沒有來源的生成、教學影片（先不做）、國高中 108 課綱格式。

目前進度：第 1 期（結構、`docread`、知識）與第 2 期（講義、課堂簡報）完成。教案、學習單、教學大綱是第 3 期。

產出代號（沿用 Paper2Lab 的資料格式）：`A` 講義、`B` 課堂簡報；第 3 期加 `C` 教案、`D` 學習單。介面只顯示名稱。講義的段落見 `internal/research/handout.go`（`HandoutParts`），簡報的規劃與寫作見 `deck.go`。

## 結構

| 目錄 | 責任 |
|---|---|
| `cmd/eduforge` | 服務入口、healthcheck |
| `internal/domain` | 持久化的契約（Course、Chapter、Lesson、Knowledge、Coverage…）。改了 struct 就執行 `make types` |
| `internal/store` | SQLite、`records` 表（kind、parent、course_id、lesson_id）、樂觀鎖、檔案儲存 |
| `internal/docread` | **獨立套件**：檔案 → 每頁文字、影像、模式。不得 import EduForge 的其他套件，之後可能抽出共用 |
| `internal/documents` | 把 docread 的結果存成來源單元（含頁面影像 blob）；PDF 圖表擷取 |
| `internal/research` | 知識：批次規劃、提示、驗證、合併、覆蓋率；講義（`handout.go`）與簡報（`deck.go`）的提示詞與檢查 |
| `internal/app` | 業務邏輯：課程順序與設定繼承（`curriculum.go`）、知識讀取任務（`knowledge_ai.go`）、產出生成任務（`generation.go`） |
| `internal/providers` | Codex、Claude Code、API Key 的呼叫與切換 |
| `internal/artifacts` | 模板匯入、Word／PPTX／PDF／HTML 匯出 |
| `internal/httpapi` | HTTP 路由 |
| `frontend/` | Vue 3 + TypeScript（strict）；API 型別由 Go 產生 |

## 規則

1. **教學順序**：未分章課次先，再依章與課次的 position。`PriorLessons` 是「已教過」的唯一定義。
2. **設定繼承**：課程存預設；章與課次只存覆蓋值，零值代表沿用（`MergeSettings`、前端 `lib/settings.ts` 要同步）。
3. **逐頁讀完**：知識讀取分批（`research.PlanBatches`），每頁都屬於且只屬於一批；失敗的批次記入 `Coverage.Missed`，不得靜默視為完成。
4. **影像辨識頁**：`Unit.Mode == "vision"` 的頁面不拒絕、不信任文字；引用它的要點標 `Vision`，數字由使用者核對。
5. 模型回覆一律驗證：引用必須是該批的頁，數字須出現在引用頁。提示詞把頁面文字與影像視為不可信資料。
6. 長時間工作（解析、讀取教材、生成）都是背景任務，進度寫在 task 上，可暫停、取消。
7. **講義段落是必備的**：知識填得出的段落都要有；課後練習不得附答案（以檢查保證，不只靠提示詞）。
8. **背景**：產出的提示詞帶入前面課次的概念（`research.BackgroundConcepts`），寫明不要重講。
9. 生產環境單一使用者；憑證在獨立 volume，不寫進映像或專案檔。ChatGPT 訂閱僅供登入者本人使用。

## 開發

```sh
scripts/dev.sh make check      # 全部檢查
scripts/dev.sh go test -race ./internal/docread
```

主機沒有 Go；工具鏈在 `Dockerfile` 的 `dev` 階段。`docker compose` 預設建置 `runtime` 階段。需要 LibreOffice／Poppler 的測試在工具不存在時會跳過。

## 提交

每個完成並驗證的步驟 commit 一次，在功能分支上；使用者說合併才合併，不主動 push。
