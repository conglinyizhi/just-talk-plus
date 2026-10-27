// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file was modified from the original Just Talk by whoamihappyhacking
// (GPL-3.0). See NOTICE for maintenance history.

package doctor

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/conglinyizhi/just-talk-plus/config"
	"github.com/conglinyizhi/just-talk-plus/internal/asrcfg"
)

type Severity int

const (
	Required Severity = iota
	Warning
)

type Check struct {
	Name     string
	OK       bool
	Severity Severity
	Detail   string
	Notes    []string
	Fix      string
}

type Report struct {
	Platform string
	Backend  string
	Info     []string
	Checks   []Check
}

func Run(cfg *config.Config, backend string) Report {
	return runPlatform(cfg, backend)
}

func (r Report) Healthy() bool {
	for _, check := range r.Checks {
		if check.Severity == Required && !check.OK {
			return false
		}
	}
	return true
}

func (r Report) Print(w io.Writer) {
	fmt.Fprintln(w, "Just Talk Plus 环境检查")
	fmt.Fprintf(w, "平台：%s", platformName(fallback(r.Platform, "unknown")))
	if r.Backend != "" {
		fmt.Fprintf(w, " / %s", r.Backend)
	}
	fmt.Fprintln(w)
	for _, line := range r.Info {
		if strings.TrimSpace(line) != "" {
			fmt.Fprintln(w, line)
		}
	}
	fmt.Fprintln(w)

	for _, check := range r.Checks {
		mark := "✓"
		if !check.OK {
			if check.Severity == Warning {
				mark = "!"
			} else {
				mark = "✗"
			}
		}
		fmt.Fprintf(w, "%s %s", mark, check.Name)
		if check.Detail != "" {
			fmt.Fprintf(w, "：%s", check.Detail)
		}
		fmt.Fprintln(w)
		for _, note := range check.Notes {
			if strings.TrimSpace(note) != "" {
				fmt.Fprintf(w, "  %s\n", note)
			}
		}
		if !check.OK && check.Fix != "" {
			fmt.Fprintf(w, "  处理：%s\n", check.Fix)
		}
	}

	if r.Healthy() {
		fmt.Fprintln(w, "\n结果：环境正常")
	} else {
		fmt.Fprintln(w, "\n结果：需要处理上面的项目后再启动 Just Talk Plus。")
	}
}

func fallback(s, v string) string {
	if strings.TrimSpace(s) == "" {
		return v
	}
	return s
}

func platformName(s string) string {
	switch strings.ToLower(s) {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	default:
		return s
	}
}

// asrEngineCheck 按配置构建语音引擎并探测可用性（引擎感知的 ASR 配置检查）。
// cfgPath 用于生成可操作的 Fix 提示。
func asrEngineCheck(cfg *config.Config, cfgPath string) Check {
	eng, err := asrcfg.Build(cfg.Voice)
	if err != nil {
		return Check{
			Name: "ASR 配置", OK: false, Severity: Warning,
			Detail: fmt.Sprintf("语音引擎 %q 未配置凭据，请任选 %s 之一配置",
				asrcfg.EngineType(cfg.Voice), engineChoices()),
			Fix: "在 " + cfgPath + " 的 [voice].engine 选择引擎，并在 [voice.engine_configs.<引擎>] 填写对应 api_key 等凭据。",
		}
	}
	ok, detail := eng.Available(context.Background())
	name := eng.Name()
	if !ok {
		return Check{
			Name: "ASR 配置", OK: false, Severity: Warning,
			Detail: name + "：" + detail,
			Fix:    "在 " + cfgPath + " 的 [voice.engine_configs.<引擎>] 中填写所需凭据（如 api_key）。",
		}
	}
	return Check{Name: "ASR 配置", OK: true, Severity: Warning, Detail: name + "：" + detail}
}

// engineChoices 面向用户的引擎候选文案（与 README 保持一致）。
func engineChoices() string {
	return "doubao-stream / auralwise / openai / dashscope / mimo-asr / siliconflow"
}
