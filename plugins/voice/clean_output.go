// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"unicode"

	"github.com/c/just-talk-go/config"
)

// cleanTrailingPunct 输出层统一处理：按配置省略句尾标点。
// 主开关省略中文句号「。」；副设置可扩展为「切断所有符号」或「额外省略英文 . ! ?」。
// 应用于上屏/复制之前的最终文本（含历史记录）。
func cleanTrailingPunct(text string, vc config.VoiceConfig) string {
	if !vc.OmitTrailingPunct || text == "" {
		return text
	}
	runes := []rune(text)
	i := len(runes)
	for i > 0 && isTrailingPunct(runes[i-1], vc) {
		i--
	}
	return string(runes[:i])
}

// isTrailingPunct 判断字符是否属于当前配置下应省略的句尾标点。
func isTrailingPunct(r rune, vc config.VoiceConfig) bool {
	if vc.OmitTrailingAll {
		// 切断句尾所有符号（中英文标点与符号，如 。！？…～ .!?,; 等）
		return unicode.IsPunct(r) || unicode.IsSymbol(r)
	}
	if r == '。' {
		return true
	}
	if vc.OmitTrailingASCII {
		return r == '.' || r == '!' || r == '?'
	}
	return false
}
