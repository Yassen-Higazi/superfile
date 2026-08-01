---
title: superfile 專案結構指南
description: 了解 superfile 程式碼庫組織方式的詳細指南
head:
  - tag: title
    content: superfile 專案結構指南 | superfile
---

# superfile 專案結構指南

此專案採用標準 Go 專案配置，並清楚分離各項關注點。以下是主要目錄及其用途的詳細說明：

## 核心目錄

### `src/` - 主要原始碼

主要原始碼分成幾個關鍵目錄：

#### `cmd/` - 進入點

- `main.go` - 應用程式的主要進入點，負責：
  - CLI 參數解析
  - 設定初始化
  - 應用程式啟動

#### `config/` - 設定管理

- `fixed_variable.go` - 包含常數值與設定路徑
- `icon/` - 圖示相關設定
  - `function.go` - 圖示初始化與管理函式
  - `icon.go` - 圖示定義與對應

#### `internal/` - 核心應用程式邏輯

包含應用程式的主要商業邏輯，依功能組織：

**設定與型別：**

- `config_function.go` - 設定載入與管理
- `config_type.go` - 設定相關型別定義
- `default_config.go` - 預設設定值
- `type.go` - 核心型別定義

**檔案操作：**

- `file_operations.go` - 基本檔案操作函式
- `file_operations_compress.go` - 檔案壓縮功能
- `file_operations_extract.go` - 檔案解壓縮功能
- `handle_file_operations.go` - 檔案操作處理器

**UI 與互動：**

- `handle_modal.go` - Modal dialog 管理
- `handle_panel_movement.go` - 面板導覽邏輯
- `handle_panel_navigation.go` - 面板聚焦管理
- `handle_pinned_operations.go` - 釘選項目功能
- `key_function.go` - 鍵盤輸入處理
- `model.go` - 核心應用程式 model
- `model_render.go` - UI 渲染邏輯

**工具：**

- `function.go` - 一般工具函式
- `get_data.go` - 資料取得函式
- `string_function.go` - 字串處理工具
- `string_function_test.go` - 字串工具測試
- `style.go` - UI 樣式定義
- `style_function.go` - UI 樣式函式
- `string_function_test.go` - 字串工具測試
- `style.go` - UI 樣式定義
- `style_function.go` - UI 樣式函式

### `testsuite/` - 使用 Python 撰寫的 superfile testsuite

- 自動測試 superfile 的功能。
- 詳細資訊請參閱 `testsuite/ReadMe.md`

## 程式碼組織原則

1. **關注點分離：**
   - 設定管理隔離在 `config/` 目錄
   - 核心商業邏輯位於 `internal/`
   - UI 相關程式碼與商業邏輯分開

2. **模組化設計：**
   - 每個檔案都有特定職責
   - 相關功能會分組在一起
   - 元件之間的相依關係清楚

3. **測試：**
   - 測試檔會放在其測試的程式碼旁邊
   - 範例：`string_function_test.go` 測試 `string_function.go`

## 貢獻指南

為 superfile 貢獻時：

1. **新增功能：**
   - 將新的商業邏輯放在適當的 `internal/` 子目錄中
   - 保持 UI 相關程式碼與商業邏輯分離
   - 遵循既有命名慣例

2. **進行變更：**
   - 維持既有檔案結構
   - 為新功能新增測試
   - 如有需要，更新設定檔

3. **程式碼風格：**
   - 遵循 Go 最佳實務
   - 維持一致格式
   - 新增適當文件

這個結構有助於維持程式碼組織，也讓新的貢獻者更容易了解應該在哪裡進行變更。
