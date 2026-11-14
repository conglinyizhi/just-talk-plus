# Changelog

All notable project changes are tracked here.

## v0.2 - 2026-11-14

- **发布范围收窄为 Linux（用户可见行为变化）**：GitHub Release 只发布 Linux x86_64（amd64）归档，不再提供 Linux arm64、macOS 与 Windows 的预编译包。这些平台的代码仍在仓库里（热键、录音、剪贴板、自动上屏），需要时自行编译；做出这个取舍的原因是维护者没有对应平台可以验证，发布没人能担保的二进制比不发布更糟。CI 矩阵与 GoReleaser 配置同步裁剪，测试步骤维持原样（仍只在 amd64 上跑）。
- **录音胶囊改版（用户可见行为变化）**：胶囊改成「中间一个状态球，左右各一栏」。左栏只放时间——当前引擎声明了单次上限（如 `deepseek` 的 120 秒）就倒计数剩余可用秒数，没有硬上限的引擎按 60 秒软上限提示（只提示，不强制停录，实测一分钟以内的语音识别成功率更高）；倒计时从第一次检测到人声起算，按下热键后一直没说话时显示满额静止，开口后才开始递减。右栏只放识别状态：`ASR ON` / `ASR ..` / `ASR ->` / `A.WAIT`。停止采集后左栏换成 `UPLOAD`。出错时两栏换成故障文案并把球改成灰色（红色一直代表正在采集，出错时那件事已经停了）：识别侧 `CRASH: / ASRERR`、连接与传输 `CRASH: / NETERR`、录音设备 `ERROR: / DEVICE`、引擎配置 `ERROR: / CONFIG`。两栏各占满 6 格、字体等宽、按固定宽度排版，文案长短变化不会改变窗口尺寸或文字坐标，数字从三位掉到两位也不会推动后面的字符；球恒定居中（左右栏等宽是球能居中的数学前提，这条被写成硬约束而不是靠偏移凑）。整体尺寸 164×40。布局计算抽成纯函数并加了表驱动测试（球居中、宽度不随文案变、两栏不压球、缩放后仍成立），平台后端只按算出的坐标绘制。另外修掉了按下停止时左栏连续横跳造成的闪烁：现在时间冻结在最后一帧，不再回退成别的文案。胶囊只保留 Linux 实现（X11 / Wayland）：Windows 与 macOS 的实现来自上游 just-talk-go，维护者没有这两个平台可以验证，已删除约 1300 行；这两个平台的其它功能（热键、录音、剪贴板、自动上屏）不受影响，只是没有胶囊指示器，需要时可从 git 历史或上游取回。
- **按引擎上限自动停止录音（用户可见行为变化）**：引擎现在可以声明「单次音频最长多少」，录制侧据此在**到达上限前**自动停止本次录音并提示「已达引擎单次上限 N 秒，自动停止录音」，而不是让用户录满一整段、等到最后转写时才收到「超过上限」的失败。截断另有一层兜底：批处理会话按同一上限丢弃超出部分，保证发出去的请求本身不越界（含停止后仍会流入的尾音）。目前只有 `deepseek` 声明了上限（120 秒）；未声明上限的引擎行为不变。
- 新增 DeepSeek（`deepseek`）语音识别引擎（`POST /v1/tools/asr`）：整段录音识别（同步返回、非流式），请求体只有 `file` 一个表单字段——接口没有 `model` / `language` / `response_format` 参数，语种按音频自动识别，所以引擎配置里没有模型与语言项，TUI 弹层只有 API Key 与 Base URL。单次上限 120 秒 / 8 MiB 两项都在本地拦截、不发送请求：时长按 WAV 头（`fmt` 的字节率，缺失时用采样率 × 声道数 × 位深折算）算真实秒数，chunk 从 RIFF 之后逐块遍历、奇数长度跳过填充字节、`data` 长度未回填（0 / 0xFFFFFFFF）时按剩余字节估算；非 WAV（Ogg Opus）解析不出时长时跳过时长检查、只靠大小上限，避免按 16 kHz 单声道硬除误拦。识别历史记录响应体里的 `request_id`（取不到时回退响应头 `x-ds-trace-id`）；HTTP 错误按实测的嵌套 `error.message` / `error.code` 解析（说明在 `error` 对象里、不在顶层），并把 trace 拼在错误串末尾，可直接提供给 DeepSeek 支持排障。TUI 引擎选择、引擎配置弹层与「相关链接」同步更新，README 双语补充；托盘模型菜单不加该引擎（接口没有模型参数）。
- 新增 Mosi（`mosi`）音频转写引擎：整段录音识别（同步返回、非流式），请求体只提交 `file` 与 `model` 两个表单字段（`response_format` 不显式发送，接口默认就是 json），默认模型 `moss-transcribe-1.0`；接口不接受 Base64 / 内联音频，只走 multipart `file`，`file_id` / `url` / `audio_url` 与异步任务（`async=true` + 任务查询）本轮未实现。响应同时兼容 JSON 与纯文本（`response_format=text` 时接口返回 `text/plain` 裸文本）；HTTP 错误按实测结构解析 `error.message` / `error.code`（错误说明嵌在 `error` 对象里，不在顶层），解析不出时回退原始 body 截断。识别历史会记录响应头 `x-trace-id`（取不到时回退响应体的 `trace_id`），HTTP 错误同样把 trace 拼在错误串末尾，可直接提供给 Mosi 支持排障；TUI 引擎选择、引擎配置弹层与「相关链接」同步更新，README 双语补充。
- **语言与热词改为按引擎存放（用户可见行为变化）**：`language` 与 `hotwords` 不再写在 `[voice]` 下、也不再对所有引擎生效，改为放进各自的 `[voice.engine_configs.<engine>]`（热词是逗号分隔字符串），TUI 的「语言」「热词」字段也随之下移到对应引擎的配置弹层，保存写各自分组——同一台机器上豆包填 `zh-CN`、OpenAI 兼容端点填 `zh`，互不影响。旧配置不用手动改：加载时会把旧版全局 `language` / `hotwords` 自动复制进已经存在的引擎分组（以及旧版平铺 `engine_config`），已有值不覆盖、不给没配过的引擎凭空建组；迁移只在内存里发生、不主动写盘，下次在程序里保存配置时才自然落盘成新写法。新装用户默认不再预置 `zh-CN`，语言交给引擎自动判定，需要固定语言时在引擎配置弹层里按引擎填写。
- **凭据隔离：旧版平铺 `engine_config` 现在只对 `doubao-stream` 生效（用户可见行为变化）**：`[voice.engine_config]` 与顶层的 `app_key` / `access_key` / `resource_id` 都是豆包时代的单引擎写法，此前任何没配过分组的引擎都会拿它当回退配置，等于把豆包的 key 发到别家端点（请求 401，还泄漏凭据；TUI 引擎配置弹层也会被预填成豆包凭据，一保存就写进别的引擎分组）。现在只有 `doubao-stream` 缺失 `[voice.engine_configs.doubao-stream]` 时才回退到平铺配置，其它引擎各读自己的 `[voice.engine_configs.<engine>]`，没有分组配置就是「未配置」。旧配置文件不会被自动修改或删除（不做迁移写盘）：仍在用平铺配置的 `doubao-stream` 用户照旧可用，把别家凭据写在平铺 `engine_config` 里的用户升级后该引擎会显示未配置，在 TUI 的引擎配置弹层里按引擎分别填一次即可。
- **托盘新增「引擎 → 模型」三级菜单（siliconflow）**：托盘「引擎」子菜单下，硅基流动多展开一层模型勾选项，`--gui` 后台模式不用开 TUI 就能换模型；勾选模型会同时把当前引擎切到 `siliconflow`，写入 `[voice.engine_configs.siliconflow].model`，其它引擎的分组配置不受影响（不会改动旧版平铺 `engine_config`；该平铺配置现在只对 `doubao-stream` 生效，见上条）。模型候选来自 `GET {base_url}/models?sub_type=speech-to-text`，缓存在 `{用户缓存目录}/just-talk-plus/models-cache.json`（24 小时有效）；菜单构建只用缓存或内置候选（`FunAudioLLM/SenseVoiceSmall`、`TeleAI/TeleSpeechASR`），构建阶段不发网络请求、菜单永不为空；网络拉取只在后台进行（启动时一次 + `高级 › 刷新模型列表` 手动一次，5 秒超时），失败只记 Warn 不影响菜单与录音。手动刷新成功后提示「已刷新 N 个模型，重启核心后菜单生效」。已知限制：GTK 系托盘（Cinnamon / Xfce / GNOME）首次展开可能丢第三级条目，收起再展开即恢复；KDE Plasma 不受影响。
- 新增硅基流动（SiliconFlow）语音转写引擎（`siliconflow`）：整段录音识别（非流式），请求体只提交 `file` 与 `model` 两个表单字段，默认模型 `FunAudioLLM/SenseVoiceSmall`，可在引擎配置中更换（如 `TeleAI/TeleSpeechASR`）；超过 50MB 在本地拦截并给出可读错误，HTTP 错误优先展示接口返回的 `message` 与 `code`；TUI 引擎选择、引擎配置弹层与「相关链接」同步更新，README 双语补充。识别历史会记录响应头 `x-siliconcloud-trace-id`（批处理引擎的上游追踪号现在也能写入历史，TUI 历史详情的标签由「豆包 LogID」改为通用的「上游 LogID」）；HTTP 错误同样在错误串末尾带上该 trace id，可直接提供给硅基流动支持排障。
- Linux 录音优先使用 `parecord`，并支持直接选择 `virtual_mic_source`
- 增强 Linux 录音进程诊断，记录启动命令、PID、退出状态、stderr 与停止错误
- PipeWire 默认源为 `virtual_mic_source` 时自动读取有实际音频的 `virtual_mic.monitor`
- 修复 `parecord` 输出参数错误导致录音立即 EOF
- 移除中间噪声门控：低于噪声阈值的音频始终原样发送并录制（阈值仅用于开头开始录音与结束停止判定），不再丢失轻声内容

- **修复 systemd 开机重启风暴**：`--gui` daemon 在图形会话尚未就绪（启动时序 race，`WAYLAND_DISPLAY`/`XDG_SESSION_TYPE` 为空）时，不再立即退出并触发 systemd `Restart=on-failure` 每 2 秒反复拉起；改为先带超时等待会话就绪、再重跑环境检查后启动。systemd 用户服务 unit 同时增加 `StartLimitIntervalSec`/`StartLimitBurst` 限制重启爆发，避免真正失败时无限刷日志。

- **托盘「Android 虚拟麦克风」子菜单补强**：新增「重启服务器」入口（停止并重新启动内嵌服务端，已连接的 Android 设备需重新握手）；「设为当前输入设备」点击后弹出对话框，说明该操作的具体影响（录音输入切到 Android 设备采集、自动转 16 kHz / 单声道 / i16、需确认服务端运行中、麦克风菜单勾选同步）；原来禁用的「Android端：16kHz / 单声道 / i16」提示改为可点击的「配置指南」，弹出当前版本连接方式（仅支持 TCP / WIFI，未适配 adb）与使用步骤。

- **托盘新增「关于和许可」子菜单**：含「just-talk-go-plus（当前）」「just-talk-go」「androidmic」三个项目，每项下设「开源许可」（弹窗显示许可、版权与修改声明）与「查看源码」（调用浏览器打开对应仓库），方便只运行 `--gui`（无终端）的用户也能查到开源与许可归属、源码与修改声明，无需翻源码或打开终端；just-talk-go 为直接上游（Go 版，作者 whoamihappyhacking），README/NOTICE 同步对齐。

- **托盘菜单换用 `fyne.io/systray`（替换 `energye/systray`），修正历史/统计子菜单不实时刷新**：新库实现 `com.canonical.dbusmenu.ItemsPropertiesUpdated`，识别结果与统计数据变化能即时推送到二级子菜单项，不再依赖旧的 `LayoutUpdated(parent=0)` 顶层刷新（Plasma 只重拉顶层、子菜单内容停在首次展开时的快照）；移除为绕过该问题而加的 `scheduleHistoryRefresh` / `scheduleStatsRefresh` 分批重试与 `WithForce` 强制刷新路径，改为纯 `SubscribeHistory` / `SubscribeStats` 事件驱动，每次识别只触发一次菜单更新，消除之前的 D-Bus 信号风暴与菜单闪烁。

- 托盘新增「统计信息」子菜单，与 TUI 共用统计数据，显示总使用次数、输入字数、平均速度和最近速度；识别完成后随统计状态及时刷新。

- 托盘 Android 虚拟麦克风菜单增加手机端配置提示：`16000 Hz / 单声道 / i16（PCM16）`，用于规避 AndroidMic F32 短读造成的缓冲区尾部残留。

- 修复 GUI 运行反馈与录音收尾：识别结果确认后立即写入历史，托盘历史菜单不再等待上屏/剪贴板操作完成；停止录音时等待采集协程把最后一批 PCM 入队，减少录音尾部丢失；配置文件热加载增加防抖，托盘显示「配置状态」并弹出应用成功或失败提示。

- 修复 GUI/systemd 运行异常：流式 ASR 在临时无音频块时发送低频静音保活，收到致命 ASR 错误后立即终止会话并停止重复报错；托盘历史菜单改为运行中动态刷新；systemd 用户服务安装时固定当前 Wayland/X11 后端，避免启动环境短暂缺失时进入 `unknown backend` 重启风暴。

- 改进远程 ASR 建连慢时的录音收尾：检测到有效人声后，如果用户先结束录音，程序会冻结本地末段音频并限时等待 ASR 建连，再补发缓存音频；胶囊依次显示 `MIC ON  ASR ->`、`WAIT ASR`、`ASR WAIT`，区分拨号、等待拨号和等待最终识别。

- Android 虚拟麦克风子菜单新增「复制诊断信息」：按需复制最近 10 次连接的收包、转换与丢帧统计，所有诊断仅常驻固定大小内存，不保存音频、不包含识别文本或密钥，也不会持续写入诊断文件。

- 托盘新增「重启」入口：重新加载当前程序版本，不调用 `systemctl --user restart`，方便开发构建后直接从托盘生效。

- 修复 GUI 录音状态显示延迟：胶囊和系统托盘现在在录音状态变更时立即刷新，不再依赖最多 1 秒的托盘轮询；停止倒计时和 AndroidMic 状态仍按周期维护。

- 新增系统托盘后台控制面：`--gui` 后台模式默认挂系统托盘（`fyne.io/systray`），可从托盘切换识别引擎 / 麦克风设备、开关 push-to-talk（hold/toggle）、控制 Android 虚拟麦克风服务端、查看或打开日志、退出；录音状态用托盘图标颜色提示（待机/录音中/停止中/错误）。`[tray]` 下可配 `enabled` 与可选全局热键 `cycle_engine_hotkey`、`toggle_push_to_talk_hotkey`。Windows 走纯 Go `Shell_NotifyIconW`（保持 `CGO_ENABLED=0`），Linux/Wayland 走 D-Bus StatusNotifierItem；菜单项仅在状态变化时刷新，避免 KDE 打开菜单时被每秒重绘闪烁。历史与频谱等重度交互仍保留 `just-talk-plus --tui` 完整终端面板兜底。

- 日志梳理与桌面层补强：daemon（`--gui`）模式不再把实时识别文本逐字打到 stdout（避免刷屏）；调试插件的热键事件从裸 `fmt.Printf` 改为走 slog（带时间戳与结构化字段）；托盘「完整设置」点击改为弹桌面提示框（kdialog / notify-send）；Android 虚拟麦克风状态会探测端口实际监听，可区分本进程运行 / 端口被其它进程占用 / 未运行。

- **`--tui-config` 新增配置模式（config-only，breaking）**：只编辑配置，不录音、不注册热键、不启动服务（含 Android 虚拟麦克风监听），跳过 doctor 健康检查；保存写配置文件，正在运行的 daemon 通过 WatchConfig 热重载，可与后台 daemon 并行。运行时面板（历史/频谱/录音调试等）在配置模式下显示占位提示；`--tui` 与默认一致，仍为完整 TUI（默认启动）。

- **命令行**：后台 daemon 模式的 flag 由 `--no-tui` 更名为 `--gui`；新增 `--tui-config`（纯配置模式，不录音/不开服务，跳过 doctor 检查，保存后 daemon 自动热重载）；`--tui` 与默认一致，仍为完整 TUI（默认启动）。

- **Linux systemd --user 集成**：`--install-systemd` 生成用户级 unit（运行 `just-talk-plus --gui`，开机自启 + journal 日志接管）；daemon 在 systemd 服务（`INVOCATION_ID`）下日志只走 stderr 交给 journald，不再写 /tmp 文件。

- 托盘补强：设备菜单新增「虚拟麦克风（AndroidMic）」可选项；「按住说话」菜单项去掉英文前缀；新增「历史记录」菜单，每条记录挂子菜单（播放录音 / 复制识别文本 / 复制报错 ID / 在文件管理器中显示）；桌面提示框跨平台（Linux kdialog / notify-send、Windows PowerShell MessageBox、macOS osascript）。

- 剪贴板恢复改为安全快照：自动上屏仅恢复受支持的纯文本、HTML、PNG 数据；检测到外部剪贴板变化、文件列表、私有格式、超大内容或 Linux 多 MIME 内容时跳过恢复，不再把图片等二进制数据作为文本写回。Wayland 的真实剪贴板测试改为显式 opt-in，默认不污染用户剪贴板。

- 自动上屏统一粘贴后保护窗口：X11/Wayland/Windows/macOS 在模拟粘贴输入成功发送后都保留 800ms settle 等待，给目标应用留出读取临时剪贴板的时间，再由输出层恢复/回收剪贴板，降低丢字或粘到旧剪贴板内容的概率（保护窗口为尽力而为，不承诺跨应用确认，实际恢复时机仍取决于目标应用读取速度）。
- 新增小米 MiMo 语音识别引擎（`mimo-asr`）：整段录音转写，走 OpenAI Chat Completions + input_audio 形态（复用新的 `chataudio` 通用客户端，后续接入同形态云厂商只需配置即可）；TUI 引擎选择、引擎配置弹层与「相关链接」同步更新，README 双语补充。

- 新增 Android 设备录音输入：内嵌 AndroidMic TCP 服务端（默认端口 54345，与 AndroidMic 协议兼容），手机端 AndroidMic 应用连接本机后即可把手机当麦克风，音频自动转换为 16 kHz 16-bit 单声道交给识别管线；「输入 → Android 虚拟麦克风」可单独启停服务端、查看监听端口与连接；麦克风调试面板的设备切换新增 Android 设备档，配置 `[voice] androidmic_port` 可改端口（重启生效）。服务端所有内部错误独立隔离，不会带崩主应用。

- 修正「语音延迟」指标口径：会话日志里的语音延迟由「检测到第一声 → 音频开始发送」改为「开始录音（按下按键）→ 音频开始发送」。起算点提前到按下录音那一刻，把等待检测到第一声的时间一并计入，与体感一致；终点为第一帧音频成功发送给 ASR 的时刻。

- 新增「生成诊断报告」（输入 tab）：用最近一条有录音的历史统计底噪 / 说话电平 / 停顿分布，生成 markdown 报告（含给大模型的分析 prompt），供参考调整参数；只生成不改配置。

- 停止判定防抖：停止判定的「人声 vs 底噪」判定新增容差（`noise_deadband`，默认 0.1）——高于底噪阈值超过容差才算人声，环境噪声在阈值附近的波动不再反复重置停止倒计时；新增可选的自动底噪（`noise_floor_auto`，默认关）：开启后停止判定阈值随环境自动漂移，无需手动校准。两者均可在 TUI「输入」tab 调整；容差调太大会把正常说话判成噪音导致提前截断丢字。

- TUI 设置补齐：新增「显示」tab（录制状态胶囊的开关/位置/空闲显隐/缩放，重启生效）；自适应增益的 4 个参数（目标峰值/峰值上限/每块最大升高/增益上限）放入「输入」tab 可直接调整，留空即回内置默认（保存后重启生效）；补上调试插件开关与调试热键编辑。

- 历史记录补录无结果录音：用户停止录音后 ASR 未返回文本、但已连接远程 ASR 且保存了录音的会话，也会写入一条历史（识别结果显示为 `[无结果]`），可从菜单直接回放该段录音；连续多条此类记录各自保留，不去重。

- 修复历史记录过长时无法正常浏览的问题：历史 tab 改为以光标为中心的滚动窗口（超出部分以 ▲/▼ 指示剩余条数，光标条目始终可见），二级操作菜单补上条目序号与时间戳，长列表下也能确认操作对象。

- 自适应增益防过爆加固：响亮音频块在放大前按安全峰值整体预缩放（不再逐样本软限幅削波）；增益上限从 100 倍收紧到 25 倍，回撤改为渐进式（避免骤降振荡），并可在 config.toml 覆盖 `auto_gain_target_peak`、`auto_gain_max_peak`、`auto_gain_max_rise`、`auto_gain_max_gain` 四个参数（缺省即保守默认值，重启生效）。

- 豆包引擎配置新增清晰的「识别传输模式」选择：双向流式（低延迟、实时结果，对应 `async`）或单向流式（等待完整音频后给最终结果，对应 `nostream`）；原有 TOML 配置保持兼容。

- 保留录音文件现在是出站审计 WAV：只写入成功交给 ASR 的音频，噪声门控丢弃或发送失败的片段不会混入；DashScope 8k 模型会以实际发送的 8k 采样率保存。

- 修复延迟麦克风的音频截断风险：采集现在独立于 ASR 拨号和网络发送持续进行，连接成功后严格按原顺序补发已缓存音频；Linux 停止录音先请求 `arecord`/`parecord` 优雅收尾，2 秒无响应才强制结束。ASR 传输或采集异常会明确显示错误并且只终止发生故障的会话。

- 新增「有声后停止」开关：停止请求后若网络麦克风仍送入有效人声，会重置「停止延迟」倒计时；只有连续静音满该时长才截断录音，胶囊会继续显示至识别完成。自适应增益改为更保守的目标电平、触发门限和抬升速度，减少过度放大。

- 新增「保留录音文件」开关：成功识别的 WAV 与最近 50 条运行期历史绑定保存；历史页回车可复制文本、使用 `ffplay` 播放，或在文件管理器中显示录音。

- 修复 TUI 折叠日志将两套缓冲分别截取后拼接而导致的乱序与跳动；现在统一按写入顺序显示最近三条。录音胶囊改为将状态点、间距与文本整体居中，改善录音状态的左偏拥挤感。

- 历史记录改为最多 50 条的运行期滚动快照，程序重启后可恢复；新记录会淘汰最旧记录，系统重启后自动清理。

- 豆包流式 ASR 的每条识别历史现在会记录并显示 WebSocket 握手返回的 `X-Tt-Logid`，便于追溯上游日志与排障。

- `make dev` 的 Air 热重载现在隐藏 watcher/build 日志并关闭自动清屏；Linux/macOS 会在运行时将前台终端交给 Bubble Tea TUI，并在退出或热重载时交还，避免 Air 与 TUI 争抢终端。

- 在 `make dev` 中按 `q` 正常退出 TUI 时，Air 会继续监听文件变更而不会自动重启程序。

- 新增噪声门控：可在输入设置中启用 `noise_gate`，低于 `noise_threshold` 的增益后 RMS 音频不会发送到远程 ASR；新增“检测当前底噪”动作，采样约 3 秒环境噪声后自动计算并持久化阈值，也支持手动调整。

- 底噪检测完成后会在 TUI/日志中明确报告成功、保存的阈值，或失败/无有效样本等结果。

- 胶囊文字增大并改为状态灯与文字整体居中布局，Linux 使用系统等宽字体测量实际文字宽度，Windows/macOS 同步放大系统字体，避免文字偏左且没有占满胶囊。

- 胶囊状态现在使用系统字体和等宽数字倒计时（如 `STOP 06.0s`），并额外显示 `MIC ON` 与 `ASR ON/..`，区分麦克风是否真的在采集、ASR 会话是否已经连接；Linux ARGB 胶囊使用 Cairo 系统字体渲染。

- 录音现在先在本地采集并缓存音频，检测到有效声音后才连接 ASR，再补发缓存的开头音频；适合有明显传输延迟的网络麦克风，也避免用户长时间静音时提前建立识别连接。胶囊进度条补齐点阵字形并扩大显示宽度。

- 录音停止延迟现在会在胶囊弹窗中显示实时进度条（每 50ms 刷新），并修复未配置或负值延时被当成立即停止的问题；`6000ms` 等有效配置会完整等待，延时上限为 60 秒。音频发送排空等待从 4 秒放宽到 15 秒，适配网络麦克风的长尾延迟。

- Added opt-in in-memory diagnostics: the `[高级] 诊断收集` TUI setting enables collection for the current run, the next three starts, or persistently; `--diagnostics` enables one invocation. `K` exports an enabled run's local snapshot to Kate, while normal launches retain no diagnostic log.

- Adaptive gain now gates on the unamplified microphone level, raises consistently weak speech toward a useful range within a few audio blocks, and uses a soft limiter plus immediate gain back-off to protect louder speech.

- `make run` now builds and starts a fresh normal binary; pass `ARGS=--diagnostics` when reproducing a problem.

- Wayland 自动上屏改为前台托管临时 `wl-copy`，粘贴完成后主动回收，避免后台剪贴板 owner 残留导致第二次上屏卡住。

- Linux 上屏的剪贴板与辅助命令现在有 3 秒硬超时，避免 `wl-copy`、`xclip` 或剪贴板恢复异常时卡住输出锁，导致后续识别结果排队数分钟。

- Mic-debug panel now renders tri-color (green→yellow→red) level bars: the bright fill marks the current level in its zone color, and peak markers hold the highest peak for about 1.5 seconds of silence. The panel distinguishes the unamplified input's peak and RMS ("真实") from the RMS after the current gain has been applied ("增益后真实").

- Renamed the project identity from `just-talk` to `just-talk-plus` (no compatibility kept — zero users besides the maintainer): the Go module path is now `github.com/conglinyizhi/just-talk-plus`, the `cmd/` package and binary are `just-talk-plus`, the per-user config directory moved from `~/.config/just-talk/` (Windows `%APPDATA%\just-talk\`) to `~/.config/just-talk-plus/` (Windows `%APPDATA%\just-talk-plus\`), and the backend-override env var is now `JUST_TALK_PLUS_BACKEND`. The GitHub remote stays `conglinyizhi/just-talk-go-plus`.

- Added Doubao recognition-quality options to the engine-config overlay (shown only for `doubao-stream`): 中英文识别 (`enable_lid`) for mixed Chinese/English and dialect speech, 二遍识别 (`enable_nonstream`, second-pass recognition for more accurate finals — async mode only), 数字规范化 (`enable_itn`), 标点 (`enable_punc`), 语义顺滑 (`enable_ddc`), and console hotword/replacement/regex-replacement tables (`boosting_table_name/id`, `correct_table_name/id`, `regex_correct_table_name/id`). The engine-config overlay now supports toggle fields (Enter flips them) alongside text and select fields.

- Fixed an autotype bug where the submitted text was the **previous** utterance's content: Wayland's `pasteWayland` returned immediately after simulating paste, so the output layer restored the clipboard before the target app had read it — the app then pasted the restored (old) content. Both X11 and Wayland now wait `pasteSettleDelay` (300ms) after the paste, and the output dispatch is serialized with a mutex so two consecutive recordings can no longer interleave clipboard writes.

- Grouped the submit/copy options under 自动上屏 on the Output tab: 复制到剪贴板 (`write_clipboard`), 上屏后还原剪贴板 (`restore_clipboard`) and 上屏失败不写入 (`no_clipboard_on_fail`) are now indented sub-options of `auto_submit` instead of flat siblings.

- Linux/Wayland autotype now writes the temporary clipboard with the sensitive marker (`wl-copy --sensitive`, i.e. `x-kde-passwordManagerHint: secret`), so Klipper and CopyQ 8.0+ silently ignore it — the recognition text no longer pollutes clipboard-manager history while pasting still works normally. Detects wl-copy ≥ 2.2 and falls back gracefully on older versions; X11 keeps the existing "save → paste → restore" behavior (xclip cannot attach the marker). README now documents setting up CopyQ as the clipboard manager.

- Renamed the "麦克风" tab to "输入" (tab order unchanged — Engine stays second, as ASR engines matter more than input settings).

- Removed the standalone "设备" field from the Microphone tab too — device selection now lives exclusively in the mic debug panel (`[`/`]` to cycle). Previously the tab field was a snapshot that could drift out of sync with the panel's persisted choice; with a single place to switch devices that linkage problem is gone.

- Removed the standalone "增益" field from the Microphone tab — gain is now adjusted exclusively in the mic debug panel (`+`/`-`), which is the single place it is configured.

- TUI wording polish: the Engine field help is now just "回车选择渠道"; the engine-config help states each engine's settings are stored independently and never conflict. The "中文排版自动空格" help no longer mentions its default or the sibling option (users see the toggle state directly). The mic debug panel is wider (84 cols) to fit long Linux device names, and the mic-test entry's gain now shows the configured baseline (same source as the "增益" field) instead of the auto-gain live value, so the two no longer disagree.

- Field help text is now rendered under the cursor field in the main form and the engine-config overlay (it was defined but never displayed). This makes every setting's explanation visible, including the two space options, whose wording now explicitly clarifies their division: **删除中文间空格** (off by default) only drops spaces between two CJK characters, **中文排版自动空格** (on by default) only adds spaces between CJK and Latin/digits — independent and togglable separately.

- The Microphone tab now has a "设备" field that lets you pick the recording device (auto-detected list, "自动" = system default), and the mic debug panel shows the current device and switches it with `[`/`]` (or `h`/`l`, `k`/`j`) — switching stops the running test and persists to `[voice].device`. Useful when you are not sure which microphone works.

- Added CJK typographic auto-spacing (`auto_space`, on by default, following the [Chinese Copywriting Guidelines](https://github.com/sparanoid/chinese-copywriting-guidelines)): automatically add a space between CJK and Latin letters or digits (e.g. `K3刚发布` → `K3 刚发布`), strip spaces adjacent to full-width punctuation, and convert full-width digits to half-width. Applied in the same output post-processing pass, before submit/copy and history.

- Toggle fields no longer show a "(空格)" hint or accept space to switch — they flip directly with Enter in the main form (space had become a dead key since toggles don't enter edit mode). Applies to every toggle across all tabs.

- Added an output post-processing option `remove_cjk_space` (TUI: Output tab "删除中文间空格"): drops spaces between two CJK characters — gaps that speech pauses get recognized as, where no punctuation fits — while keeping spaces between CJK and Latin letters or digits per typographic convention. Applied before submit/copy, same as trailing-punctuation omission.

- Navigation now follows vim conventions and arrow keys work everywhere: `j`/`k` or `↑`/`↓` move the cursor in the main form, history list and every overlay; `h`/`l` or `←`/`→` switch tab groups (also cycling select options while editing). The bottom help bar and each overlay's hint line spell out both key sets.

- The "相关链接" overlay now also lists Doubao resources (official docs and the new speech console). The Qianwen ASR model catalog link (qwen3-asr-flash-realtime 等) moved under the DashScope channel where it belongs — the OpenAI channel keeps no links since that catalog is not OpenAI-compatible.

- Engine credentials are now stored per engine (`[voice.engine_configs.<engine>]`) instead of one flat `engine_config`, so you can configure and keep keys for several ASR channels at once and switch between them without re-entering credentials. The TUI "engine config" overlay (renamed from "channel config") edits and persists the current engine's own settings; the legacy flat `engine_config` and `app_key`/`access_key`/`resource_id` fields remain as migration-read compatibility.
- Moved documentation links out of the channel picker (which previously rendered them as long underlined lines that broke layout) into a "相关链接" entry on the Engine tab: one overlay lists links for all engines (not just the currently selected one), j/k to select and Enter opens the chosen link in the system browser (`xdg-open` / `open` / `rundll32`). Links live in a separate `engineLinks` table so adding one never touches picker layout.

- Added a DashScope (Qianwen AI platform) Fun-ASR realtime streaming engine (`dashscope`) over the official WebSocket API (`wss://dashscope.aliyuncs.com/api-ws/v1/inference/`): live partial results while recording, Bearer-token auth via `api_key`, configurable `model` / `sample_rate` / `base_url` / `language` in `[voice.engine_config]`. 8k models (e.g. `fun-asr-flash-8k-realtime`) only accept 8000 Hz audio, so the engine auto-downsamples the recorder's 16 kHz PCM 2:1 and defaults `sample_rate` to 8000 for them. Docs: https://platform.qianwenai.com/docs/developer-guides/speech/asr-realtime
- Reworked the TUI main view onto the charmbracelet component library: settings are grouped into tabs (General / Engine / Microphone / Output / History) rendered by `list` with title/description rows, string fields edit via `textinput`, and the key help bar is generated from bindings (`help`). The history view is now a tab whose entries can be copied with Enter. Vertical height only depends on the largest tab, so adding settings no longer stretches the screen.

- Added trailing-punctuation omission processed uniformly before output: the master toggle `omit_trailing_punct` strips a trailing Chinese period "。", with two sub-options (`omit_trailing_all` to strip all trailing punctuation, `omit_trailing_ascii` to additionally strip ASCII `.` `!` `?`) shown indented under it in the TUI.

- Added a microphone debug panel in the TUI: the same recording hotkey triggers a test recording (no ASR), with live peak/RMS level bars, clipping warning, fine-grained gain adjustment (`+`/`-`, persisted), and an adaptive-gain mode that auto-lowers gain on clipping and recovers slowly when quiet (applies to normal recordings too, toggleable via `[voice].auto_gain`).
- Added an in-memory recognition history (last 10 submitted/copied results) with a TUI panel to re-copy any entry to the clipboard.

- Added copyright headers to source files (new files © conglinyizhi, modified files carry a modified notice), plus `NOTICE` and `THIRD_PARTY_NOTICES` documenting project ownership, maintenance history, and third-party dependency licenses. Maintenance of Just Talk moved from `whoamihappyhacking` to `conglinyizhi`; the project remains GPL-3.0.
- Renamed the application to Just Talk Plus (in-app TUI title, README, doctor output, and install paths); the binary and config paths keep the `just-talk` name.

- Added a pluggable ASR engine layer (`internal/speech`) with a registry, ported from the go-human-handler speech module: `doubao` (Volcano Engine, streaming + batch), `auralwise` (async task transcription), and a new OpenAI-compatible Whisper engine (`openai`). `[voice].engine` selects the engine and `[voice.engine_config]` holds engine-specific settings; `app_key`/`access_key`/`resource_id` remain as Doubao legacy shortcuts.
- Batch-only engines are wrapped into streaming sessions via a buffering adapter (`speech.BatchSession`) so the recording pipeline stays unchanged; batch engines get a longer final-result wait.
- The TUI gained an engine selector, and `--doctor` now reports the configured engine's availability instead of assuming Doubao credentials.
- Reworked the TUI into an engine-aware dynamic form: switching the Engine field shows only that engine's settings (Doubao model version/stream mode/two credential sets/hotwords; AuralWise API key/base URL/optimize; OpenAI API key/base URL/model) and saves them into `engine_config`; legacy flat Doubao fields are migrated into the form.
- The Engine field now opens a channel picker overlay (j/k navigation, per-channel description subtitles) instead of a cycling select; selecting a channel rebuilds the form underneath.
- Renamed the Doubao engine type from `doubao` to `doubao-stream` to make the streaming nature explicit; the picker description notes that the 1.0 / 2.0 model version is chosen after selecting the channel. The OpenAI channel description points to the QianwenAI model catalog for available ASR models.
- Removed engine type aliases (`volc-asr`, `doubao-asr`, `aural-wise`); the canonical types are `doubao-stream` / `auralwise` / `openai`, and `null` is now an internal fallback only. Doubao's 1.0 (bigasr) vs 2.0 (seedasr) access modes are documented in the README with a comparison table.
- `--doctor` ASR guidance now asks users to configure any one of the supported engines instead of showing a raw Doubao credential error.

## v0.0.3 - 2026-07-29

- Reworked Windows modifier-only hotkeys so the low-level hook only observes key edges and never consumes or replays modifiers. `Alt+Super` works through exact physical-state matching, stale hook state is cleared without combining separate single-key presses, and system shortcuts such as `Alt+Tab` remain untouched.

## v0.0.2 - 2026-07-21

- Fixed intermittent Windows `Alt+Super` false activations where stale low-level hook state could make a later Alt-only press look like the full shortcut; Windows modifier combinations now require an exact modifier set and reset stale suppressed state after physical release.

## v0.0.1 - 2026-07-17

- Added a tag-triggered GoReleaser v2 pipeline using the official GitHub Action to build Linux, macOS, and Windows amd64/arm64 archives on native runners and publish SHA256 checksums.
- Added Windows 10/11 support with global key-state monitoring, native WinMM microphone capture, Unicode clipboard access, SendInput auto-submit, a click-through Win32 status overlay, and Windows environment checks.
- Added Windows-standard config, state, log, and per-user installation paths. Windows builds no longer require external recording or clipboard commands.
- Added an opt-in Windows microphone integration test using `JUST_TALK_TEST_WINDOWS_AUDIO=1`.
- Added a low-level Windows keyboard-hook fallback so modifier-only shortcuts such as `Alt+Super` still work when `GetAsyncKeyState` misses the physical Windows key.
- Consume Windows modifier-only voice shortcuts such as `Alt+Super` so they do not activate menus or toolbars in the focused application, while replaying unrelated modifier shortcuts normally.
- Fixed recording shutdown getting stuck while ASR audio writes were still in flight, and accept empty final ASR responses without waiting for a false timeout.
- Redesigned the Windows status overlay with per-pixel transparency, antialiased animated waveform bars, smoother capsule edges, and a soft shadow.
- Clarified README build and install setup steps for the repository directory and `~/.local/bin` PATH.
- Restricted voice hotkeys to non-text global shortcut keys, rejecting letters, digits, punctuation, Space, and similar text-producing keys.
- Avoid duplicate auto-submit on KDE Plasma by using uinput directly and not writing the Wayland primary selection there.
- TUI is now the default startup mode.
- Added persistent usage statistics for total sessions, recognized characters, average speed, and recent speed.
- Added configurable ASR hotwords.
- Added TUI help toggle with `h`.
- Improved Wayland clipboard and auto-submit behavior with `wl-copy` and `wtype`.
- Added Linux recording status overlay for X11 and Wayland.
- Added macOS support for global hotkeys, native recording, clipboard, auto-submit, recording status overlay, and environment checks.
- Removed non-cgo macOS fallback builds; Just Talk now requires cgo for native platform integration.
- Replaced the old Claude-specific agent guide with `AGENTS.md` and clarified build documentation.
- Improved toggle and hold hotkey behavior for fast repeated key presses.
- Show ASR connection and final-result timeout errors in the status UI/overlay instead of immediately falling back to idle.
- Added transient `Esc` cancel and `R` retry hotkeys while recording or showing retryable errors.
- Improved X11 overlay placement on multi-monitor setups and switched X11 rendering to an ARGB window for smoother rounded corners.
- Fixed a Wayland overlay shutdown race that could crash while closing the app, and surfaced Linux `arecord` microphone/device failures in the UI.
- Made `Esc` cancel active overlay states, including the final ASR wait state, and suppress output from canceled pending sessions.
- Improved Wayland overlay rounded-corner antialiasing, especially on KDE Plasma.
- Added `just-talk --install` and `make install` to install the binary into `~/.local/bin`.

## 2026-05-30

- Initial Linux-focused development snapshot.
- Supported Linux Wayland hotkeys via evdev.
- Supported Linux X11 hotkeys via native X11 grabs.
- Added Doubao streaming ASR integration.
- Added TUI configuration interface.
- Added automatic clipboard copy and auto-submit.
