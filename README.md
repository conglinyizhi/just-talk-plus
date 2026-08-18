# Just Talk Plus

[English](README.en.md)

减少用键盘的次数，改用口喷吧。

Just Talk Plus 是一个面向桌面环境的语音输入工具。它通过全局快捷键录音，把语音识别结果复制到剪贴板，或直接上屏到当前输入框，适合写代码、聊天、记笔记和处理长文本输入。

## 截图

![Just Talk Plus TUI](docs/screenshot-tui.png)

## 功能

- 全局快捷键录音，支持 `toggle` 和 `hold` 两种模式。
- 语音热键限定为适合作为全局快捷键的按键：支持纯修饰键、功能键、Tab、CapsLock、方向键和导航键等；不支持字母、数字、标点、空格等普通字符键。
- 多语音引擎可选：豆包（火山引擎）流式 ASR（默认，边录边识别）、千问 DashScope Fun-ASR 实时流式（8k 模型自动降采样）、AuralWise 批处理转写、OpenAI 兼容 Whisper 接口（可指向本地 whisper / Groq 等端点）。
- 麦克风调试面板：同一录音热键触发测试录音（不进识别），实时显示峰值/有效电平与软限幅，细粒度调整增益（`+`/`-`），并可切换只在有效人声期间调节的自适应增益。
- 识别历史面板：最近 50 条上屏/复制结果滚动保存在系统运行时目录，可选中重新复制到剪贴板；系统重启后自动清理。有录音但未识别出文本的会话会以 `[无结果]` 记入历史，便于回放录音。
- 可选保留录音：开启「输入 → 保留录音文件」后，成功识别会保留实际发往 ASR 的对应 WAV（噪声门控丢弃的片段不会写入）；历史页回车可复制文本、用 `ffplay` 播放录音，或在文件管理器中显示文件。
- 句尾标点省略：输出前统一处理，主开关省略句尾句号「。」，可扩展为切断句尾所有符号或额外省略英文 `.` `!` `?`。
- 中文间空格清理：可选删除中文与中文之间的空格（说话停顿被识别成的小空隙），中文与英文/数字之间的空格按排版惯例保留。
- 中文排版自动空格（默认开，遵循《[中文文案排版指北](https://github.com/sparanoid/chinese-copywriting-guidelines)》）：自动在中文与英文/数字之间补空格（如 `K3刚发布` → `K3 刚发布`）、全角标点两侧去空格、全角数字转半角。
  - 两者分工：**删除中文间空格**（默认关）只删中文↔中文之间的空格；**中文排版自动空格**（默认开）只补中文↔英文/数字之间的空格。二者互不影响，可独立开关。
- 上屏与复制相关选项归在「自动上屏」下（输出 tab）：**自动上屏**（模拟粘贴到输入框），其子项**复制到剪贴板**（识别结果写入剪贴板）、**上屏后还原剪贴板**（成功后还原原内容，尽量不污染剪贴板历史）、**上屏失败不写入**（失败时也还原，识别文本不留在剪贴板）——识别完成后按组合执行，减少污染剪贴板历史（如剪贴板管理器记录）。
- Wayland / X11 / macOS / Windows 顶层录音状态胶囊提示。
- TUI 配置界面基于组件库（list/help 等）并采用 tab 分组（通用/引擎/输入/输出/历史/显示），设置项再多也不撑高界面。
- 热词增强识别（豆包），适合项目名、人名、英文术语和专有名词。
- 录音历史统计，包括历史次数、总字数、平均速度和最近速度。

## 平台状态

当前支持 Linux、macOS 和 Windows 桌面：

| 平台 | 状态 | 说明 |
| --- | --- | --- |
| Linux Wayland | 已支持 | 已支持 Sway / wlroots 场景；快捷键基于 evdev，需要 input 权限 |
| Linux X11 | 已支持 | 使用 X11 原生全局热键 |
| macOS | 已支持 | 全局快捷键基于 CGEventTap，录音使用 CoreAudio，剪贴板使用 NSPasteboard，胶囊显示使用 AppKit NSPanel |
| Windows 10/11 | 已支持 | 全局按键轮询及低级键盘钩子边沿回退、WinMM 录音、Unicode 剪贴板、SendInput 自动上屏和 Win32 状态胶囊 |

## 构建

Just Talk Plus 依赖平台原生能力。Linux 和 macOS 构建需要启用 cgo；Windows 使用纯 Go 的 Win32 调用，不需要 cgo。

Linux 构建依赖：

```bash
# Arch Linux
sudo pacman -S --needed go gcc libx11 libxtst libxext wayland

# Debian / Ubuntu
sudo apt install golang-go build-essential libx11-dev libxtst-dev libxext-dev libxinerama-dev libwayland-dev
```

macOS 构建依赖：

```bash
# 需要 Apple Command Line Tools 提供 clang 和 macOS SDK；不需要安装完整 Xcode。
xcode-select --install
```

Windows 构建依赖：

```powershell
# 安装 Go 1.25 或更高版本；不需要额外安装 ffmpeg、SoX 或 C 编译器。
winget install --id GoLang.Go --exact
```

构建当前平台二进制：

```bash
cd just-talk-plus
CGO_ENABLED=1 go build -o build/just-talk-plus ./cmd/just-talk-plus
```

Windows PowerShell：

```powershell
cd just-talk-plus
go build -o build\just-talk-plus.exe .\cmd\just-talk-plus
```

安装到 `~/.local/bin/just-talk-plus`：

```bash
# 确保 ~/.local/bin 在 PATH 中（如未配置，将下面这行加入 ~/.bashrc 或 ~/.zshrc）
# export PATH="$HOME/.local/bin:$PATH"
build/just-talk-plus --install
# 或
make install
```

macOS 需要在本机 macOS 上构建；项目不提供非 cgo 版本。

Windows 安装到 `%LOCALAPPDATA%\Programs\Just Talk Plus\just-talk-plus.exe`：

```powershell
.\build\just-talk-plus.exe --install
# 如果安装目录尚未在 PATH 中，按命令输出提示添加即可。
```

## Release 下载

GitHub Release 提供以下预编译归档：

- Linux amd64 / arm64
- macOS Intel / Apple Silicon
- Windows amd64 / arm64
- `SHA256SUMS.txt` 文件校验

发布流程使用 GoReleaser v2 和官方 `goreleaser/goreleaser-action`。Linux、macOS 和 Windows 二进制分别在对应的 GitHub 托管 runner 上原生构建；维护者推送 `v*` 标签时会自动构建并发布，例如：

```bash
git tag v0.7.0
git push origin v0.7.0
```

## 使用

默认启动 TUI：

```bash
just-talk-plus
```

后台模式：

```bash
just-talk-plus --no-tui
```

指定后端：

```bash
just-talk-plus --backend wayland
just-talk-plus --backend x11
```

Windows 不需要指定后端。首次使用前可检查麦克风和配置：

```powershell
.\build\just-talk-plus.exe --doctor
```

开发调试可使用 Air 热重载（需先安装 `air`）：

```bash
go install github.com/air-verse/air@latest
make dev
make dev ARGS="--no-tui --verbose"
```

代码或配置变更后会自动重新构建并重启调试 CLI，临时二进制位于 `tmp/`。
在 Linux/macOS 上，Air 会通过终端入口程序将前台终端临时交给 TUI，并在退出或热重载时交还给 Air；这样 Bubble Tea 可正常接收按键。如果需要观察 Air 自身日志，可临时执行 `air -c .air.toml -log.main_only false`。

使用 `doubao-stream` 时，历史页会显示本次识别 WebSocket 握手响应中的 `X-Tt-Logid`，可用于向豆包支持团队追溯上游日志。

## 配置

默认配置路径：

```text
# Linux / macOS
~/.config/just-talk-plus/config.toml

# Windows
%APPDATA%\just-talk-plus\config.toml
```

推荐热键配置：

```toml
[voice]
mode = "toggle"
push_to_talk = "Alt+Super"
gain = 1
auto_gain = true   # 自适应增益：仅在检测到人声时缓慢抬升弱输入；响亮块整体预缩放防削波，限幅时渐进回撤
# auto_gain_target_peak = 0.42  # 目标输出峰值（默认 0.42）
# auto_gain_max_peak = 0.65     # 输出峰值超过此值开始回撤增益（默认 0.65）
# auto_gain_max_rise = 1.15     # 每块（约 200ms）最大升高倍数（默认 1.15）
# auto_gain_max_gain = 25       # 增益上限（倍，默认 25；上限 100）
# 以上 4 个参数也可在 TUI 的「输入 → 自适应增益」下直接调整：留空即回内置默认，保存后重启生效。
stop_delay_ms = 3000  # 停止后的静音等待时长
stop_after_silence = true  # 停止请求后，持续收到人声就重置等待；静音满上述时长才截断
# 延迟麦克风建议：停止延迟应不小于设备的最大传输延迟；采集会在 ASR 拨号期间持续缓存，连接后补发。
omit_trailing_punct = false  # 输出前省略句尾句号「。」（主设置）
# omit_trailing_all = false  # 副设置：切断句尾所有符号（含英文）
# omit_trailing_ascii = false  # 副设置：额外省略英文 . ! ?
# remove_cjk_space = false  # 删除中文与中文之间的空格（思考停顿产生的空隙）；中文与英文/数字间的空格保留
# auto_space = true  # 中文排版自动空格：中英/中数间补空格，全角标点两侧去空格，全角数字转半角
# 上屏与复制（归在 auto_submit 下）：
# auto_submit = true          # 自动上屏：模拟粘贴到当前输入框
# write_clipboard = true      # 子项：复制到剪贴板（识别结果写入剪贴板）
# restore_clipboard = true    # 子项：上屏后还原剪贴板（成功后还原原内容）
# no_clipboard_on_fail = true # 子项：上屏失败不写入（失败时也还原原内容）

# 诊断收集默认关闭；在 TUI 选择“本次运行”即时启用；3 = 后续 3 次启动，-1 = 持续开启
# [diagnostics]
# startup_runs = 3
```

`Alt+Super` 配合 `toggle` 模式是推荐用法。按一次开始录音，再按一次停止录音，避免按住模式下和桌面环境或输入框发生按键冲突。在 Windows 上，低级键盘钩子只观察按键边沿，不会消费或回放修饰键，因此单独使用 `Alt`、`Super` 或 `Alt+Tab` 时会保持系统原有行为。组合键必须精确匹配配置的修饰键集合；钩子回退状态会通过未拦截的物理按键状态校验，避免把两次独立的单键按下拼成组合键。

语音热键只支持适合作为全局快捷键的按键：

- 支持：纯修饰键组合，如 `Alt+Super`、`Ctrl+Alt+Shift`。
- 支持：功能键 `F1` 到 `F24`，如 `F9`、`Alt+F8`。
- 支持：非文本控制键和导航键，如 `Tab`、`Enter`、`Escape`、`Backspace`、`CapsLock`、`Up`、`Down`、`Left`、`Right`、`Home`、`End`、`PageUp`、`PageDown`、`Insert`、`Delete`。
- 不支持：字母、数字、标点、空格、数字小键盘数字和符号等会输入文本的按键，如 `Alt+G`、`G`、`Alt+1`、`Alt+Space`。

热词示例：

```toml
[voice]
hotwords = ["Wayland", "Sway", "wl-copy", "wtype", "just-talk-plus"]
```

macOS 热键写法：

```toml
[voice]
# Option 等价于 Alt，Command/Cmd 等价于 Super
push_to_talk = "Option+Command"
```

Windows 使用 `Win` 或 `Super` 表示 Windows 徽标键。如果麦克风不可用，请在“Windows 设置 → 隐私和安全性 → 麦克风”中允许桌面应用访问麦克风。

### 语音引擎

`[voice]` 下的 `engine` 选择识别引擎，每个引擎的私有配置单独存放于 `[voice.engine_configs.<engine>]`（如 `[voice.engine_configs.openai]`），可同时保存多个渠道的凭据、互不覆盖。默认 `engine = "doubao-stream"`。TUI 中「引擎」字段按回车会打开渠道选择弹层，每个渠道附特点说明；「引擎配置」二级弹层（按回车进入、`s` 保存、`esc` 取消）编辑当前引擎的凭据与专属设置，保存后写入 `engine_configs.<engine>`；「相关链接」弹层汇总全部引擎的文档链接，回车用浏览器打开。旧版平铺 `engine_config` 与 `app_key` / `access_key` / `resource_id` 字段仍作为迁移读取的兼容写法保留。

#### 豆包（火山引擎）流式 ASR

边录边识别，支持热词。豆包有两种接入方式：

| 接入方式 | 模型版本 | 凭据 | 资源 ID（默认） |
| --- | --- | --- | --- |
| 豆包 1.0（旧版控制台） | `1.0` | `app_id` + `access_key`（或旧版平铺 `app_key` / `access_key`） | `volc.bigasr.sauc.*` |
| 豆包 2.0（新版控制台） | `2.0` | `api_key` | `volc.seedasr.sauc.*` |

新版凭据二选一：新版控制台创建应用后拿到 `X-Api-Key`，用 `api_key`；旧版控制台的 App ID + Access Token 用 `app_id` + `access_key`。示例：

```toml
[voice]
engine = "doubao-stream"
language = "zh-CN"

[voice.engine_configs.doubao-stream]
# 新版控制台凭据
api_key = "…"
# 或用旧版凭据：
# app_id = "…"
# access_key = "…"
# 接入方式：
model_version = "1.0"      # 1.0=bigasr（默认） | 2.0=seedasr
stream_mode = "async"      # async=双向流式（低延迟、实时结果） | nostream=单向流式（完整音频后给最终结果）
# —— 识别质量选项（均在 TUI「引擎配置」弹层中，仅豆包引擎显示）——
# enable_lid = true         # 中英文及方言识别（中英混说建议开；语言需为自动或 zh-CN）
# enable_nonstream = true   # 二遍识别，最终更准（仅 async 双向流式生效）
# enable_itn = true         # 数字规范化（一九七零年 → 1970年）
# enable_punc = true        # 标点
# enable_ddc = false        # 语义顺滑（删语气词/重复词）
# boosting_table_name = ""  # 热词词表名（控制台自学习平台）
# boosting_table_id = ""
# correct_table_name = ""   # 替换词词表名
# correct_table_id = ""
# regex_correct_table_name = ""  # 正则替换词表名
# regex_correct_table_id = ""
# resource_id = "volc.bigasr.sauc.duration"  # 显式覆盖资源 ID
# base_host = "openspeech.bytedance.com"
```

AuralWise 异步任务转写（整段录音识别，无需公网 URL）：

```toml
[voice]
engine = "auralwise"

[voice.engine_configs.auralwise]
api_key = "asr_…"
# base_url = "https://api.auralwise.cn/v1"
# optimize = "auto"          # auto | true | false
# poll_timeout = "10m"
```

OpenAI 兼容 Whisper 接口（官方 API、Groq、本地 whisper 服务等）：

```toml
[voice]
engine = "openai"

[voice.engine_configs.openai]
api_key = "sk-…"
# base_url = "https://api.openai.com/v1"   # 可指向兼容端点
# model = "whisper-1"
```

千问（DashScope）Fun-ASR 实时流式识别（WebSocket，边录边出字）。文档见 [实时语音识别](https://platform.qianwenai.com/docs/developer-guides/speech/asr-realtime)，可用的 ASR 模型可参考 [千问实时 ASR 模型库（搜索 asr）](https://www.qianwenai.com/models?search=asr)：

```toml
[voice]
engine = "dashscope"

[voice.engine_configs.dashscope]
api_key = "sk-…"        # DASHSCOPE_API_KEY
# model = "fun-asr-realtime"            # 8k 模型（如 fun-asr-flash-8k-realtime）自动降采样到 8000 Hz
# sample_rate = "16000"                 # 8000 | 16000
# base_url = "wss://dashscope.aliyuncs.com/api-ws/v1/inference/"
# language = "zh-CN"
```

批处理引擎（auralwise / openai）在录音结束后一次性转写，需要更长的等待时间；流式引擎（doubao-stream / dashscope）在识别过程中即可看到部分结果。`engine = "null"` 用于禁用识别。

### 麦克风调试与识别历史

TUI 主界面选择「麦克风调试」回车进入调试面板：

- 按**同一录音热键**触发/停止测试录音（只测麦克风，不触发识别与上屏）
- `[` / `]`（或 `h`/`l`、`k`/`j`）循环切换录音设备（含「自动 = 系统默认」），切换即停止测试并持久化到 `[voice].device`（设备列表自动探测）
- 实时显示原始输入的峰值/真实电平条、应用增益后的真实电平条与“已限幅”警告；各电平条会暂留峰值标记
- `+` / `-` 细粒度调整增益（1–100，实时生效并持久化）
- 空格切换自适应增益：检测到有效人声后缓慢抬升弱输入（增益上限 25 倍）；静音不抬增益，响亮块整体预缩放防削波，限幅时渐进回撤
- 需要排查卡顿、上屏或识别异常时，在通用设置选择「[高级] 诊断收集」的本次、后续 3 次或持续启动；诊断启用后按 `K` 导出本机内存日志到 Kate。也可用 `--diagnostics` 仅为本次启动开启

「历史记录」面板展示最近 50 条成功上屏/复制的识别结果，`j`/`k` 选择、回车重新复制到剪贴板，`esc` 退出。历史滚动快照保存在系统运行时目录，程序重启后可恢复，系统重启后自动清理。

### 与剪贴板管理器（Klipper / CopyQ）配合

自动上屏在机制上需要「写剪贴板 → 模拟粘贴」，会在剪贴板历史（Klipper、CopyQ 等）留下一条识别文本记录。为了不污染剪贴板历史，Just Talk Plus 在 Linux/Wayland 上以**敏感内容**方式写入临时剪贴板（`wl-copy --sensitive`，即 `x-kde-passwordManagerHint: secret`）：

- **Klipper** 与 **CopyQ 8.0+** 识别该标记后**静默忽略**：不存储、不显示、不进入历史；
- 上屏仍可正常粘贴（文本本身不受影响），只是剪贴板管理器不记录。

启用方式：

1. 安装 CopyQ（Arch：`sudo pacman -S copyq`，其他发行版同理），并勾选「启动时自动运行」（或复制 `copyq.desktop` 到 `~/.config/autostart/`）；
2. CopyQ 启动后接管剪贴板历史；Klipper 无需卸载（它很小且可能被桌面元包依赖），若想避免双份历史可在「系统设置 → 开机与关机 → 自动启动」里取消 Klipper；
3. 剪贴板历史快捷键由 Klipper 的 `Ctrl+Alt+V` 换成 CopyQ 的（默认 `Ctrl+Alt+C`，可在 CopyQ 设置中自定义）。

> 说明：X11 会话下 `xclip` 不支持该敏感标记，X11 上屏仍通过「保存→粘贴→恢复」尽量还原剪贴板内容，但剪贴板管理器可能仍短暂记录一条；`wl-copy --sensitive` 需要 wl-clipboard ≥ 2.2（当前主流发行版均已包含）。


## 更新日志

见 [CHANGELOG.md](CHANGELOG.md)。

## 维护与贡献

Just Talk Plus 由 `conglinyizhi` 维护（基于 `whoamihappyhacking` 的原作继续开发，版权声明见 [NOTICE](NOTICE)）。

本项目不接受 Pull Request。欢迎通过 Issue 反馈 bug、使用体验和功能建议。

## 许可证

Just Talk Plus 使用 GNU General Public License v3.0 开源。
