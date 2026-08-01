---
title: 圖片預覽
description: 了解 superfile 的圖片預覽如何運作，以及如何判斷終端機相容性。
head:
  - tag: title
    content: 圖片預覽 | superfile
---

本教學會一步步帶你了解如何使用 superfile 的圖片預覽功能。

## 什麼是圖片預覽？

superfile 支援透過多種顯示協定，直接在終端機中預覽圖片。只要終端機支援，圖片就能以行內方式顯示，不需要外部檢視器。

---

## 終端機相容性

superfile 會使用 `$TERM` 與 `$TERM_PROGRAM` 環境變數自動偵測你的終端機。我們支援在下列終端機中渲染：

| 終端機        | 協定           | 圖片預覽支援 |
| ------------- | -------------- | ------------ |
| **kitty**     | Kitty protocol | ✅           |
| **WezTerm**   | Kitty protocol | ✅           |
| **Ghostty**   | Kitty protocol | ✅           |
| **iTerm2**    | Inline images  | ❌           |
| **Konsole**   | Inline images  | ❌           |
| **VSCode**    | Inline images  | ❌           |
| **Tabby**     | Inline images  | ❌           |
| **Hyper**     | Inline images  | ❌           |
| **Mintty**    | Inline images  | ❌           |
| **foot**      | Sixel graphics | ❌           |
| **Black Box** | Sixel graphics | ❌           |

> ✅ 代表完整支援使用 Kitty protocol 進行行內圖片預覽  
> ❌ 代表目前尚不支援圖片預覽

---

## 支援的協定

superfile 支援下列渲染協定，並會根據你的終端機自動選擇最適合的方式：

| 協定名稱           | 說明                                                  | 狀態         |
| ------------------ | ----------------------------------------------------- | ------------ |
| **Kitty protocol** | 功能最完整，支援像素精準渲染、透明度與縮放。          | ✅ Preferred |
| **Sixel**          | DEC 終端機和 foot 等部分現代終端機使用的舊標準。      | ❌           |
| **iTerm2 inline**  | iTerm2 的專有圖片格式，也用於 Tabby、Hyper 等終端機。 | ❌           |
| **ANSI**           | 後備文字渲染方式，只使用 ANSI 區塊或中繼資料。        | ✅ Always    |

---

## 終端機偵測與像素大小

superfile 會檢查下列項目來偵測終端機能力：

- `$TERM`
- `$TERM_PROGRAM`

這些變數能協助判斷是否可能使用進階渲染。不過，實際支援狀態會在執行時透過終端機查詢確認。

為了正確縮放圖片，superfile 會送出下列 escape code：

```
\x1b[16t
```

這段序列會向終端機查詢每個 **cell 的像素大小**。superfile 會使用結果來：

- 維持正確的圖片長寬比
- 避免預覽畫面變形
- 適應終端機大小變更

如果你的終端機不支援 `\x1b[16t`，我們會退回使用預設假設，例如每個 cell 為 `10×20 px`。

## 優雅退回 ANSI

當不支援進階圖片預覽時（例如終端機不支援 Kitty protocol），superfile 會優雅地退回使用以 ANSI 為基礎的預覽，以色塊方式顯示。

這能確保在所有終端機環境中都有一致且可靠的使用體驗。
