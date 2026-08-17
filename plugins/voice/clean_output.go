// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"unicode"

	"github.com/conglinyizhi/just-talk-plus/config"
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

// isLatinOrDigit ASCII 字母或数字（英文与数字用半角）。
func isLatinOrDigit(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
}

// isNumberLike 数字或数字组的一部分：半角数字、度数、百分比、千分号。
// 用于「90° 的角」「15% 的」这类数字组与中文之间补空格，而数字组内部不加。
func isNumberLike(r rune) bool {
	return r >= '0' && r <= '9' || r == '°' || r == '%' || r == '‰'
}

// isFullWidthPunct 全角（非 ASCII）标点或符号。
// 规范：全角标点与其他字符之间不加空格。度数/百分比符号（° ‰ %）不算标点，属于数字组。
func isFullWidthPunct(r rune) bool {
	if r <= 127 {
		return false
	}
	if r == '°' || r == '‰' {
		return false
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// needSpaceBetween 按中文排版规范判断两字符之间是否需要空格：
//   - 中文与英文之间：需要
//   - 中文与数字（含度数/百分比）之间：需要
//   - 全角标点两侧：不需要
//   - 其余（中文-中文、英文-数字、数字-数字等）：不需要
func needSpaceBetween(a, b rune) bool {
	if isFullWidthPunct(a) || isFullWidthPunct(b) {
		return false
	}
	aNum := isNumberLike(a)
	bNum := isNumberLike(b)
	return (isCJK(a) && (isLatinOrDigit(b) || bNum)) || ((isLatinOrDigit(a) || aNum) && isCJK(b))
}

// autoSpaceCJK 按《中文文案排版指北》自动整理空格：
//   - 中文与英文之间补空格（如"在LeanCloud上"→"在 LeanCloud 上"）
//   - 中文与数字之间补空格（如"花了5000元"→"花了 5000 元"、"K3刚发布"→"K3 刚发布"）
//   - 全角标点与两侧字符之间不留空格（如"iPhone ，好开心！"→"iPhone，好开心！"）
//   - 全角数字转半角（如"１０００"→"1000"）
//
// 数字与单位之间（"20TB"）不强制补空格：产品名（K3、3K）等歧义过大，
// 与 pangu 等主流工具保持一致，只处理中文边界。
func autoSpaceCJK(text string) string {
	if text == "" {
		return text
	}
	runes := []rune(text)
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		// 全角数字 → 半角
		if r >= '０' && r <= '９' {
			r = r - '０' + '0'
		}
		// 全角标点：与两侧字符之间不加空格（去掉前面的空格）
		if isFullWidthPunct(r) {
			for len(out) > 0 && out[len(out)-1] == ' ' {
				out = out[:len(out)-1]
			}
			out = append(out, r)
			continue
		}
		// 全角标点后的空格也去掉（全角标点右侧不留空格）
		if r == ' ' && len(out) > 0 && isFullWidthPunct(out[len(out)-1]) {
			continue
		}
		// 中文与英文/数字之间补空格
		if i > 0 && needSpaceBetween(out[len(out)-1], r) {
			out = append(out, ' ')
		}
		out = append(out, r)
	}
	return string(out)
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
