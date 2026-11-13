# 產物下載

教學文檔提供 Word（DOCX）與 PDF；課堂簡報提供 PowerPoint（PPTX）與 PDF。下載按鈕位於編輯器上方，有未儲存修改時需先儲存。檔名含標題與修訂號，下載讀取明確的不可變 revision，不呼叫 LLM，也不建立或覆寫 revision。

## 匯出方式

`backend/internal/render` 直接由 Artifact 模型產生 Office Open XML ZIP；文字與表格仍可編輯，圖片嵌入檔案，PPTX 保留講者備註。簡報依三種 layout 排列內容，使用樣板配色與字型。Office 自身的字型度量與換行可能與瀏覽器預覽不同，密集內容需實際檢查；字級縮至下限仍無法容納時，以 `export_layout_failed` 拒絕匯出，提示縮短文字或分頁，避免靜默裁切。

依使用者指示參考相鄰 Paper2Lab 的 `internal/artifacts/export.go` 與 `internal/documents/parse.go`：PDF 使用 LibreOffice 無頭模式轉換同一份 DOCX／PPTX。每次使用獨立暫存目錄及 UserInstallation profile；參數陣列不經 shell，轉檔限 45 秒，整個下載請求限 60 秒。成功、錯誤與取消都清除暫存檔；PDF 需有 `%PDF-` 標頭且不超過 128 MiB。

Office 檔案內含 Noto Sans TC／Noto Serif TC 字型。容器也安裝同一批字型供 LibreOffice 產生 PDF。這是現有文檔與簡報的下載功能，尚未實作 M5 獨立 PDF 產物工作區、M6 影片下載或圖片 AI 生成。

## API

- `GET /api/v1/documents/{id}/download?revision={number}&format=docx|pdf`
- `GET /api/v1/slides/{id}/download?revision={number}&format=pptx|pdf`

revision 必須為正整數；未生成或不存在的版本回傳 404；格式與產物不符回傳 400。沿用固定預設使用者與產物類型檢查。成功回應為檔案 MIME、`Content-Disposition: attachment`、`Cache-Control: no-store`。

錯誤沿用 JSON envelope；圖片或字型讀取失敗為 `export_assets_failed`，產生檔案失敗為 `export_failed`。前端檢查 HTTP 狀態後才建立 Blob 下載，不把錯誤 JSON 存成 Office 檔案。

## 圖片與資源限制

上傳圖片驗證同一使用者及產物後讀取；外部圖片只接受 HTTPS 443，所有 DNS 結果及重新導向均驗證，連線固定至驗證過的公網 IP，不使用環境 proxy。每個遠端圖片限 8 MiB／15 秒，解碼限 4096×4096 且不超過 800 萬像素；解碼後重新編碼 PNG。匯出圖片合計最多 64 MiB，伺服器同時只處理一份匯出。無法讀取的圖片會回報錯誤，不靜默遺漏。

## 部署與驗證

後端 Docker build context 改為專案根目錄，以共用 `frontend/download-fonts.mjs` 的固定版本及 SHA-256 字型下載。後端 runtime 改為 Debian，安裝 LibreOffice Writer／Impress 與 fontconfig；前端維持原有配置。需要重新建置映像，原有資料 volume 不變。

`make test` 的 backend-tools 設定 `EDUFORGE_EXPORT_INTEGRATION=1`，以實際 LibreOffice 匯出文檔／簡報 PDF，再用 pdftotext 與 pdffonts 驗證繁中文字串及字型內嵌。Office 結構測試涵蓋 XML、文字跳脫、圖片、講者備註、字型與來源不變；前端測試涵蓋指定版本、未儲存狀態、PDF 入口與下載錯誤。

必跑 `make config`、`make lint`、`make test`、`make build`。另以繁中及英文內容實際開啟 DOCX／PPTX 與 PDF，檢查三種簡報版面、圖片、換行、缺字與講者備註。程式碼測試無法取代 Office／PDF 的視覺驗收。

## 既有資料 volume 的帳號相容性

正式後端固定以非 root 的 UID 100／GID 101 執行，與原 Alpine 映像的資料擁有者一致。更換基底映像時必須保留這組數字；僅保留 `app` 名稱不足以相容既有 volume。若 UID 改變，SQLite 的 WAL 初始化會以 readonly database 失敗。不要以刪除資料 volume 處理這個問題。

重新建置後可執行 `sh scripts/check-backend-permissions.sh` 檢查執行帳號與 SQLite 寫入；檢查只使用容器內 `/tmp` 的暫存資料庫，不讀寫正式資料庫。再以 `make up` 與 `docker compose ps` 確認服務 healthy。
