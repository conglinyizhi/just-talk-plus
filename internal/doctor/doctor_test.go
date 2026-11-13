// Copyright (C) 2026 conglinyizhi
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Just Talk Plus.

package doctor

import (
	"strings"
	"testing"

	"github.com/conglinyizhi/just-talk-plus/config"
)

// TestLegacyFlatHintOnlyForOtherEngines 旧版平铺 engine_config 只对 doubao-stream
// 生效；别的引擎未配置时要给出说明，否则用户只会以为配置丢了。
func TestLegacyFlatHintOnlyForOtherEngines(t *testing.T) {
	flat := map[string]string{"api_key": "doubao-flat-key"}

	cases := []struct {
		name      string
		engine    string
		flat      map[string]string
		wantHint  bool
		wantInMsg string
	}{
		{name: "siliconflow 有平铺配置", engine: "siliconflow", flat: flat, wantHint: true, wantInMsg: "只对 doubao-stream 生效"},
		{name: "openai 有平铺配置", engine: "openai", flat: flat, wantHint: true, wantInMsg: "只对 doubao-stream 生效"},
		{name: "doubao 本身不需要提示", engine: "doubao-stream", flat: flat, wantHint: false},
		{name: "没有平铺配置不提示", engine: "siliconflow", flat: nil, wantHint: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vc := config.VoiceConfig{Engine: tc.engine, EngineConfig: tc.flat}
			hint := legacyFlatHint(vc)
			if tc.wantHint {
				if hint == "" {
					t.Fatalf("期望有提示，实际为空")
				}
				if !strings.Contains(hint, tc.wantInMsg) {
					t.Fatalf("提示 %q 不含 %q", hint, tc.wantInMsg)
				}
				if !strings.Contains(hint, tc.engine) {
					t.Fatalf("提示 %q 未点名当前引擎 %q", hint, tc.engine)
				}
				return
			}
			if hint != "" {
				t.Fatalf("不应有提示，实际为 %q", hint)
			}
		})
	}
}

// TestASREngineCheckHintishFix 引擎未配置时，Fix 里要带上平铺配置的说明。
func TestASREngineCheckHintAppearsInFix(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "siliconflow"
	cfg.Voice.EngineConfig = map[string]string{"api_key": "doubao-flat-key"}

	check := asrEngineCheck(cfg, "/tmp/config.toml")
	if check.OK {
		t.Fatal("siliconflow 无分组凭据时应判定未配置")
	}
	if !strings.Contains(check.Fix, "只对 doubao-stream 生效") {
		t.Fatalf("Fix 缺少平铺配置说明: %q", check.Fix)
	}
	if !strings.Contains(check.Fix, "[voice.engine_configs.<引擎>]") {
		t.Fatalf("Fix 缺少填写位置提示: %q", check.Fix)
	}
}
