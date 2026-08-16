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

// cleanCJKSpace 删除中文与中文之间的空格（如"你好 世界"→"你好世界"）。
// 只处理两侧都是 CJK 统一表意文字的空格（连续空格视为一个整体）；
// 中文与英文/数字/标点之间的空格保留。
// 用于消除说话停顿被识别成的小空格（该处不加标点也不适合加标点）。
func cleanCJKSpace(text string) string {
	if text == "" {
		return text
	}
	runes := []rune(text)
	out := make([]rune, 0, len(runes))
	for i := 0; i < len(runes); {
		if runes[i] != ' ' {
			out = append(out, runes[i])
			i++
			continue
		}
		// 收集一段连续空格
		j := i
		for j < len(runes) && runes[j] == ' ' {
			j++
		}
		if i > 0 && j < len(runes) && isCJK(runes[i-1]) && isCJK(runes[j]) {
			i = j // 整段空格两侧都是中文：删除
			continue
		}
		for ; i < j; i++ {
			out = append(out, ' ')
		}
	}
	return string(out)
}

// isCJK 判断是否为 CJK 统一表意文字（汉字）。
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han)
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
