# EduForge

教育工作者的 AI 教學內容平台。提供繁體中文／英文介面、教學文檔及課堂簡報工作區。需求、樣板版本與編輯紀錄存於 SQLite。生成由後端接入 ChatGPT 訂閱方案或支援結構化輸出的 Chat Completions 相容服務。

## Docker 啟動

需要 Docker Engine 與 Docker Compose v2.32 以上（或相容的新版本）。使用 `make` 是可選的便利方式；主機不需安裝 Go 或 Node.js。首次建置需要連網下載映像與 npm 套件。

```bash
make up
# 或 docker compose up --build --detach --wait
```

開啟 <http://localhost:8081>。前端為 Nginx 靜態網站；`/api/` 原路徑轉送至內網的 Go 後端，後端不對主機公開連接埠。

```bash
curl --fail http://localhost:8081/api/v1/health
# {"status":"ok","service":"eduforge"}

docker compose ps
docker compose logs --tail=100
make down
```

預設綁定本機 `127.0.0.1:8081`，目前維持單一使用者、無登入。可用 `EDUFORGE_PORT=8082 make up` 更換本機連接埠。Docker Compose 啟動會等待後端健康檢查通過；兩個正式容器均以非 root 使用者、唯讀根檔案系統執行。

## 模型連線

執行 `make up` 後，開啟 <http://localhost:8081/settings/models>，或使用頁首的「模型連線」。

- **ChatGPT 訂閱方案**。按「使用 ChatGPT 繼續」，頁面切換到本機 `127.0.0.1` 網址並前往 OpenAI 授權。允許 EduForge 使用 ChatGPT 方案後，返回設定頁選擇帳號可用模型，套用並測試連線。可用性依帳號、方案及 OpenAI 的整合權限而定。
- **API key**。填入完整 HTTPS Chat Completions URL、模型名稱及金鑰，按「儲存 API 連線」，再按「測試連線」。服務須支援 `json_schema` 與 `max_completion_tokens`。

金鑰送出後會清空欄位。金鑰及 OAuth 權杖只暫存在後端記憶體，後端重啟後需重新連線。連線資料僅在 SQLite 保留非機密的主機識別碼、OAuth client ID 與帳號 subject，供後續登入使用。

「中斷連線」會清除目前的連線。ChatGPT 連線亦會嘗試撤銷遠端授權，如果未能確認撤銷，設定頁會提示你至 ChatGPT 設定中中斷應用程式。

也可保留原有的後端環境變數設定方式。以下三個變數必須一起設定，供啟動時載入。

| 變數 | 值 |
|---|---|
| `EDUFORGE_LLM_ENDPOINT` | 完整 HTTPS Chat Completions URL |
| `EDUFORGE_LLM_MODEL` | 端點支援的模型名稱 |
| `EDUFORGE_LLM_API_KEY` | API 金鑰 |

ChatGPT 訂閱採用官方本機應用程式 OAuth 流程，回呼為 `http://127.0.0.1:8081/api/v1/model-connection/chatgpt/callback`。使用 `EDUFORGE_PORT` 更換連接埠時，Compose 會同步設定 `EDUFORGE_PUBLIC_ORIGIN`。對外提供遠端服務前，須另外確認相應的 OpenAI client 註冊與部署條件。

流程依據 [OpenAI 登入文件](https://developers.openai.com/siwc/token-sharing-open-source/sign-in) 與 [模型請求規格](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)。

## 課堂簡報

開啟 <http://localhost:8081/slides>，輸入主題、授課對象、1–12 頁與生成語言。先選版面並產生一頁樣板，確認版本後才能生成完整簡報。生成會在背景執行，可離開後回來、取消或於失敗後重試。

完成後可編輯投影片標題、文字、清單、HTTPS 圖片與講者備註，新增／複製／排序頁面，並播放簡報。點選、多選、從空白處拖曳框選，或反白文字後輸入 AI 修改指示；確認差異後才建立新修訂。所有儲存均保留歷史，支援復原、重做與回復歷史版本。

映像會包含 Noto Sans TC／Noto Serif TC 字型與 OFL 授權，建置時下載固定版本並驗證 SHA-256。圖片生成、匯出 PPTX、跨類型樣板來源與長期風格學習尚未提供；圖片可先使用位置說明，再加入網址。

## 開發與檢查

```bash
make dev      # 前景啟動容器；Compose Watch 同步 Vue、重啟 Go
make lint     # 容器內 gofmt 檢查、go vet、ESLint、vue-tsc
make test     # 容器內 go test ./... 與 Vitest
make build    # 建置正式前後端映像；二進位與靜態檔包含在映像內
make config   # 驗證正式與開發 Compose 設定，不需連線至 daemon
```

`make dev` 同樣使用 <http://localhost:8081>，Vite 會將 `/api/` 轉送到後端。與正式環境共用 Compose 專案，切換時先 `make down`。按 Ctrl+C 結束開發服務後，可用 `make down` 移除容器。

測試工具服務使用 `tools` profile，正常部署不會啟動；檢查時會從最新程式碼建置工具映像，不會對執行中的正式服務寫入資料。測試不呼叫 LLM、TTS 或其他外部 API。

啟動時會自動套用 SQLite migrations，也可執行 `make migrate` 更新正式資料 volume。正式與開發容器各用自己的資料 volume，`make down` 會保留資料。

## 相依套件

`frontend/package-lock.json` 與 `backend/go.sum` 已納入專案。Docker 建置使用 `npm ci` 與 `go mod download`，更新套件時須同步更新對應鎖檔。

## 相關文件

- [開發規則與階段](AGENTS.md)
- [M1 文檔流程、API 與驗收](docs/m1.md)
- [M4 簡報流程、背景任務與選取修改](docs/m4.md)
- [M0 架構與 health API](docs/m0.md)

