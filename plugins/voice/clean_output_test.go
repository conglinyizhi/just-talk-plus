// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package voice

import (
	"testing"

	"github.com/c/just-talk-go/config"
)

func TestCleanTrailingPunct(t *testing.T) {
	cases := []struct {
		name string
		vc   config.VoiceConfig
		in   string
		want string
	}{
		{"disabled keeps text", config.VoiceConfig{}, "你好。", "你好。"},
		{"empty text", config.VoiceConfig{OmitTrailingPunct: true}, "", ""},

		// 主开关：仅省略中文句号
		{"zh period", config.VoiceConfig{OmitTrailingPunct: true}, "你好。", "你好"},
		{"zh multiple periods", config.VoiceConfig{OmitTrailingPunct: true}, "你好。。", "你好"},
		{"zh mid-text period kept", config.VoiceConfig{OmitTrailingPunct: true}, "你好。世界", "你好。世界"},
		{"zh exclamation kept by default", config.VoiceConfig{OmitTrailingPunct: true}, "你好！", "你好！"},
		{"ascii period kept by default", config.VoiceConfig{OmitTrailingPunct: true}, "Hello.", "Hello."},

		// 副设置：英文符号 . ! ?
		{"ascii dot", config.VoiceConfig{OmitTrailingPunct: true, OmitTrailingASCII: true}, "Hello.", "Hello"},
		{"ascii bang", config.VoiceConfig{OmitTrailingPunct: true, OmitTrailingASCII: true}, "Hello!", "Hello"},
		{"ascii mixed", config.VoiceConfig{OmitTrailingPunct: true, OmitTrailingASCII: true}, "你好。Hello!!", "你好。Hello"},
		{"ascii keeps zh bang", config.VoiceConfig{OmitTrailingPunct: true, OmitTrailingASCII: true}, "你好！", "你好！"},

		// 副设置：切断所有符号
		{"all zh bangs", config.VoiceConfig{OmitTrailingPunct: true, OmitTrailingAll: true}, "你好！！", "你好"},
		{"all ellipsis", config.VoiceConfig{OmitTrailingPunct: true, OmitTrailingAll: true}, "你好…～", "你好"},
		{"all ascii", config.VoiceConfig{OmitTrailingPunct: true, OmitTrailingAll: true}, "Hello!!!", "Hello"},
		{"all mixed", config.VoiceConfig{OmitTrailingPunct: true, OmitTrailingAll: true}, "你好。！Hello.", "你好。！Hello"},
		{"all keeps letters", config.VoiceConfig{OmitTrailingPunct: true, OmitTrailingAll: true}, "你好世界", "你好世界"},

		// 主 + 副1 + 副2：all 优先
		{"all plus ascii", config.VoiceConfig{OmitTrailingPunct: true, OmitTrailingAll: true, OmitTrailingASCII: true}, "你好。！", "你好"},
	}
	for _, c := range cases {
		if got := cleanTrailingPunct(c.in, c.vc); got != c.want {
			t.Fatalf("%s: cleanTrailingPunct(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestCleanCJKSpace(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no space", "你好世界", "你好世界"},
		{"single space between zh", "你好 世界", "你好世界"},
		{"multiple spaces between zh", "你好  世界", "你好世界"},
		{"zh-en space kept", "你好 world", "你好 world"},
		{"en-zh space kept", "hello 世界", "hello 世界"},
		{"zh-number space kept", "你好 2024", "你好 2024"},
		{"number-zh space kept", "2024 你好", "2024 你好"},
		{"zh-punct-zh space kept", "你好， 世界", "你好， 世界"},
		{"leading space untouched", " 你好世界", " 你好世界"},
		{"trailing space untouched", "你好世界 ", "你好世界 "},
		{"en only untouched", "hello world", "hello world"},
		{"mixed: only zh-zh removed", "你好 世界 hello 你好 世界", "你好世界 hello 你好世界"},
		{"han-han removed regardless of script", "汉字 中文", "汉字中文"},
		{"kana-han space kept (not both Han)", "こんにちは 你好", "こんにちは 你好"},
	}
	for _, c := range cases {
		if got := cleanCJKSpace(c.in); got != c.want {
			t.Fatalf("%s: cleanCJKSpace(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestAutoSpaceCJK(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"pure zh untouched", "你好世界", "你好世界"},
		{"pure en untouched", "hello world", "hello world"},

		// 中文与英文之间补空格
		{"zh-en", "在LeanCloud上", "在 LeanCloud 上"},
		{"en-zh", "使用GitHub登录", "使用 GitHub 登录"},
		{"zh-en-zh", "围绕AVObject进行", "围绕 AVObject 进行"},
		{"already spaced kept", "在 LeanCloud 上", "在 LeanCloud 上"},

		// 中文与数字之间补空格
		{"zh-digit", "花了5000元", "花了 5000 元"},
		{"digit-zh", "有15个", "有 15 个"},
		{"user scenario K3", "而且 K3刚发布，他们不可能直接跨个大版本直接发 K4吧", "而且 K3 刚发布，他们不可能直接跨个大版本直接发 K4 吧"},
		{"k model spaced", "K3 刚发布", "K3 刚发布"},
		{"degree kept tight", "角度为90°的角", "角度为 90° 的角"},
		{"percent kept tight", "有15%的CPU提升", "有 15% 的 CPU 提升"},

		// 全角标点两侧去空格
		{"punct left space", "iPhone ，好开心！", "iPhone，好开心！"},
		{"punct right space", "iPhone， 好开心！", "iPhone，好开心！"},
		{"punct both", "他说「喵」 了哎！", "他说「喵」了哎！"},

		// 全角数字转半角
		{"fullwidth digits", "这个蛋糕只卖１０００元", "这个蛋糕只卖 1000 元"},

		// 混合
		{"mixed", "新MacBook Pro有15%的CPU性能提升。", "新 MacBook Pro 有 15% 的 CPU 性能提升。"},
	}
	for _, c := range cases {
		if got := autoSpaceCJK(c.in); got != c.want {
			t.Fatalf("%s: autoSpaceCJK(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
