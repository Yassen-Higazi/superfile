---
title: 疑難排解
description: 遇到問題了嗎？來這裡看看。
head:
  - tag: title
    content: 疑難排解 | superfile
---

## 我的 superfile 圖示沒有正確顯示

請試試以下方法：

- 確認您已安裝 [nerdfont](https://www.nerdfonts.com/font-downloads)（可以選擇任何您喜歡的字體！）
- 將這個字體套用到您的終端機。不同終端機可能需要不同設定，請查閱對應的設定方式。

## 救命！我的 superfile 畫面渲染整個亂掉了！

請試試以下方法：

- 將 locale 設為 utf-8
- chcp 65001（如果您的 shell 支援）
- 將環境變數 RUNEWIDTH_EASTASIAN 設為 0（`RUNEWIDTH_EASTASIAN=0`）
