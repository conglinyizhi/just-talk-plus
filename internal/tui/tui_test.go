package tui

import (
	"strings"
	"testing"

	"github.com/c/just-talk-go/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func fieldKeys(fields []field) map[string]bool {
	keys := make(map[string]bool, len(fields))
	for _, f := range fields {
		keys[f.key] = true
	}
	return keys
}

func fieldByKey(fields []field, key string) *field {
	for i := range fields {
		if fields[i].key == key {
			return &fields[i]
		}
	}
	return nil
}

func TestBuildFieldsDoubao(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "doubao-stream"
	cfg.Voice.AppKey = "legacy-app"
	fields := buildFields(cfg)

	keys := fieldKeys(fields)
	for _, want := range []string{"enabled", "engine", "language", "ec.model_version", "ec.stream_mode", "ec.api_key", "ec.app_id", "ec.access_key", "hotwords"} {
		if !keys[want] {
			t.Fatalf("missing field %q in %v", want, keys)
		}
	}
	// 旧版平铺 app_key 迁移显示到 ec.app_id
	if f := fieldByKey(fields, "ec.app_id"); f.input.Value() != "legacy-app" {
		t.Fatalf("app_id = %q, want legacy-app", f.input.Value())
	}
	// 模型版本默认 1.0（bigasr）
	if f := fieldByKey(fields, "ec.model_version"); f.selectValue() != "1.0" {
		t.Fatalf("model_version = %q, want 1.0", f.selectValue())
	}
}

func TestBuildFieldsOpenai(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "openai"
	cfg.Voice.EngineConfig = map[string]string{"api_key": "sk-1", "model": "whisper-small"}
	fields := buildFields(cfg)

	keys := fieldKeys(fields)
	if keys["ec.model_version"] || keys["ec.app_id"] || keys["hotwords"] {
		t.Fatalf("openai should not show doubao fields: %v", keys)
	}
	if f := fieldByKey(fields, "ec.model"); f.input.Value() != "whisper-small" {
		t.Fatalf("model = %q", f.input.Value())
	}
}

func TestSwitchEnginePreservesValues(t *testing.T) {
	cfg := config.Default() // engine = doubao-stream
	m := New(cfg)
	for i := range m.fields {
		switch m.fields[i].key {
		case "push_to_talk":
			m.fields[i].input.SetValue("F9")
		case "ec.api_key":
			m.fields[i].input.SetValue("sk-test")
		case "ec.model_version":
			m.fields[i].optIdx = 1 // 2.0 (seedasr)
		}
	}
	m.switchEngine("openai")

	if f := fieldByKey(m.fields, "engine"); f.selectValue() != "openai" {
		t.Fatalf("engine = %q, want openai", f.selectValue())
	}
	if f := fieldByKey(m.fields, "push_to_talk"); f.input.Value() != "F9" {
		t.Fatalf("hotkey lost: %q", f.input.Value())
	}
	// api_key 在两个引擎中都存在，应保留
	if f := fieldByKey(m.fields, "ec.api_key"); f.input.Value() != "sk-test" {
		t.Fatalf("api_key lost: %q", f.input.Value())
	}
	// doubao 专属字段不应残留
	if fieldByKey(m.fields, "ec.model_version") != nil {
		t.Fatal("ec.model_version should not exist for openai")
	}
}

func TestSwitchEngineBackToDoubaoKeepsModelVersion(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.EngineConfig = map[string]string{"model_version": "2.0"}
	m := New(cfg)
	if f := fieldByKey(m.fields, "ec.model_version"); f.selectValue() != "2.0" {
		t.Fatalf("model_version = %q, want 2.0", f.selectValue())
	}
}

func testTI(v string) textinput.Model {
	ti := textinput.New()
	ti.SetValue(v)
	return ti
}

func TestEngineFieldsAuralwise(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Engine = "auralwise"
	cfg.Voice.EngineConfig = map[string]string{"api_key": "asr_x", "optimize": "true"}
	fields := buildFields(cfg)
	keys := fieldKeys(fields)
	for _, want := range []string{"ec.api_key", "ec.base_url", "ec.optimize"} {
		if !keys[want] {
			t.Fatalf("missing field %q in %v", want, keys)
		}
	}
	if f := fieldByKey(fields, "ec.optimize"); f.selectValue() != "true" {
		t.Fatalf("optimize = %q, want true", f.selectValue())
	}
}

func TestEngineChannelsDescriptions(t *testing.T) {
	byName := map[string]pickerItem{}
	for _, it := range engineChannels {
		byName[it.name] = it
	}
	for _, name := range []string{"doubao-stream", "auralwise", "openai"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("missing channel %q", name)
		}
	}
	// doubao-stream：提示选定后再选择 1.0 / 2.0 模型版本
	if !strings.Contains(byName["doubao-stream"].subtitle, "1.0") || !strings.Contains(byName["doubao-stream"].subtitle, "2.0") {
		t.Fatalf("doubao-stream subtitle should mention 1.0/2.0 model version: %q", byName["doubao-stream"].subtitle)
	}
	// openai：给出来源方向链接
	if !strings.Contains(byName["openai"].subtitle, "https://www.qianwenai.com/models?search=asr") {
		t.Fatalf("openai subtitle should include qianwenai link: %q", byName["openai"].subtitle)
	}
}

func TestEnginePickerNavigationAndConfirm(t *testing.T) {
	cfg := config.Default() // engine = doubao-stream
	m := New(cfg)
	m.w, m.h = 100, 40

	// 打开弹层，光标定位到当前引擎 doubao-stream
	m.openEnginePicker()
	if m.picker == nil || m.picker.idx != 0 {
		t.Fatalf("picker = %+v", m.picker)
	}

	// 导航到 openai 并确认
	m.handlePickerKey("down")
	m.handlePickerKey("down")
	if got := m.picker.items[m.picker.idx].name; got != "openai" {
		t.Fatalf("picker idx -> %q", got)
	}
	m.handlePickerKey("enter")
	if m.picker != nil {
		t.Fatal("picker should close after confirm")
	}
	for _, f := range m.fields {
		if f.key == "engine" && f.selectValue() != "openai" {
			t.Fatalf("engine = %q, want openai", f.selectValue())
		}
	}
	// openai 动态字段已重建
	if fieldByKey(m.fields, "ec.model") == nil {
		t.Fatal("openai fields not rebuilt")
	}
}

func TestEnginePickerEscCancels(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)
	m.openEnginePicker()
	m.handlePickerKey("esc")
	if m.picker != nil {
		t.Fatal("picker should close on esc")
	}
	for _, f := range m.fields {
		if f.key == "engine" && f.selectValue() != "doubao-stream" {
			t.Fatalf("engine changed after cancel: %q", f.selectValue())
		}
	}
}

func TestEnterOnEngineFieldOpensPicker(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)
	for i, f := range m.fields {
		if f.key == "engine" {
			m.cursor = i
			break
		}
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.picker == nil {
		t.Fatal("enter on engine field should open picker")
	}
	if m.editing {
		t.Fatal("should not enter field editing for engine")
	}
	// 弹层打开时其他按键全部被拦截
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.picker == nil {
		t.Fatal("picker should stay open while active")
	}
}

func TestRenderPicker(t *testing.T) {
	cfg := config.Default()
	m := New(cfg)
	m.w, m.h = 100, 40
	m.openEnginePicker()
	out := m.renderPicker()
	if out == "" {
		t.Fatal("empty picker render")
	}
	for _, want := range []string{"doubao-stream", "auralwise", "openai", "qianwenai.com"} {
		if !strings.Contains(out, want) {
			t.Fatalf("picker render missing %q:\n%s", want, out)
		}
	}
	// doubao-stream 的 subtitle 提到 1.0 / 2.0 模型版本
	if !strings.Contains(out, "1.0") || !strings.Contains(out, "2.0") {
		t.Fatalf("picker render should mention doubao model versions:\n%s", out)
	}
}
