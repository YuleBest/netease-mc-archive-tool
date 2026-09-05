# nmcat · netease-mc-archive-tool

[![Release](https://github.com/YuleBest/netease-mc-archive-tool/actions/workflows/release.yml/badge.svg)](https://github.com/YuleBest/netease-mc-archive-tool/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

网易我的世界（中国版）基岩存档命令行工具，使用 Go 实现。

> 解密 / 加密网易加密存档，一键导出 `.mcworld` 供国际版导入，读取 MC（基岩引擎）真实版本号与世界基本信息。
> 加密算法与密钥推导原理的完整研究见 [docs/encryption.md](docs/encryption.md)。

## 功能

| 命令 | 别名 | 功能 |
| --- | --- | --- |
| `nmcat decrypt` | — | 解密网易 XOR 加密存档（密钥自动推导） |
| `nmcat encrypt` | — | 加密存档（国际版 → 网易版），默认密钥与官方一致 |
| `nmcat export` | `mcworld` | 解密并导出为 `.mcworld`，国际版我的世界打开即自动导入 |
| `nmcat version` | — | 读取存档对应的 MC（基岩引擎）真实版本号（≠ App 版本号） |
| `nmcat info` | — | 查看存档基本信息：世界名、版本、最后游玩、模式、种子、加密状态等 |
| `nmcat tui` | — | **交互式界面**：浏览存档信息，选操作、看进度、出结果，全程按键驱动 |

**通用行为**：

- 输入支持 **zip 压缩包**与**目录**两种形态，按内容自动识别（目录 → 按目录读取；文件头为 `PK` 魔数 → 按 zip 读取），不依赖扩展名；
- 解密/加密均为流式处理，大存档不会整体载入内存；
- 默认输出到新文件（`<输入名>_decrypted` / `_encrypted` / `.mcworld`），**不修改输入**；输出与输入同路径会被直接拒绝；
- 退出码：`0` 成功，`1` 运行时错误（存档无效、旧版加密、密钥错误等），`2` 用法错误；
- `version` / `info` 支持 `--json` 机器可读输出；
- `tui` 需要交互式终端（TTY）：Windows Terminal / 各类 *nix 终端 / Android Termux 均可运行。

## 交互式界面（`nmcat tui`）

```sh
nmcat tui                      # 进入后输入存档路径
nmcat tui 网易存档.zip          # 直接打开存档
```

```text
┌ nmcat 交互式存档工具 ──────────────┐
│ 世界        我的世界               │
│ MC 引擎版本  1.21.120              │
│ db 加密     已加密（2/3 个文件…）  │
│ 密钥        可自动推导（88329851） │
└───────────────────────────────────┘
  ▸ 解密存档（网易版 → 国际版）
    加密存档（国际版 → 网易版）
    导出 .mcworld（国际版一键导入）
    重新选择存档 / 退出
```

方向键选操作 → 回车确认（自动算好输出路径，已存在会提示覆盖）→ 进度条实时显示逐文件处理 → 结果页给出校验结论。按键：`↑/↓` 选择、`Enter` 确认、`Esc` 返回、`q`/`Ctrl+C` 退出。

## 安装

下载预编译版本（推荐）：从 [Releases](https://github.com/YuleBest/netease-mc-archive-tool/releases) 获取对应平台的压缩包，覆盖 Windows（zip）/ Linux / macOS（tar.gz）/ Android（arm64，Termux 与 adb 环境可直接运行）。

```sh
# 或用 go install：
go install github.com/YuleBest/netease-mc-archive-tool/cmd/nmcat@latest
# 或从源码构建：
git clone https://github.com/YuleBest/netease-mc-archive-tool
cd netease-mc-archive-tool
go build -ldflags "-X main.version=$(git describe --tags --always)" -o nmcat ./cmd/nmcat
```

## 快速上手

```console
$ nmcat decrypt 网易存档.zip
[i] 输入:      网易存档.zip (zip)
[i] db 目录:   ESfjmffkJN0=/db
[i] 密钥:      88329851（hex 3838333239383531，来源: 自动推导）
[i] 已解密: 2 个文件: CURRENT, MANIFEST-000006
[✓] 校验:     CURRENT 明文校验通过（MANIFEST-000006）
[✓] 已写出:   网易存档_decrypted.zip（10 个条目）

$ nmcat export 网易存档.zip
[i] 输入:      网易存档.zip (zip)
[i] 世界根:    ESfjmffkJN0=（内容已提升到压缩包根）
[i] 密钥:      88329851（hex 3838333239383531，来源: 自动推导）
[i] 已解密: 2 个文件
[✓] 校验:     CURRENT 明文校验通过（MANIFEST-000006）
[✓] 已导出:   网易存档.mcworld（10 个条目）
[i] 提示:     将该文件发送到手机后，用国际版我的世界打开即可自动导入

$ nmcat version 网易存档.zip
MC 引擎版本:            1.21.120
InventoryVersion:       1.21.120
lastOpenedWithVersion:  [1, 21, 120, 0, 0]
level.dat:              ESfjmffkJN0=/level.dat

$ nmcat info 网易存档.zip
世界:            ESfjmffkJN0=
输入:            网易存档.zip (zip)
levelname.txt:   我的世界
世界名:          我的世界
MC 引擎版本:     1.21.120
最后游玩:        2026-09-06 01:10:46 CST（1788628246000）
游戏模式:        生存（GameType=0）
难度:            简单（Difficulty=1）
生成器:          无限（Generator=1）
世界种子:        4436057132318921
出生点:          X=0, Y=32767, Z=0
游戏内时间:      389 tick
存档格式版本:    10
文件:            10 个 / 896.9 KB
db 加密:         已加密（2/3 个文件带魔数）
密钥:            可自动推导（88329851）
level.dat:       ESfjmffkJN0=/level.dat
```

以上均为真实运行输出（样例为 `docs/encryption.md` §6 的验证存档）。`version` / `info` 对加密存档同样有效（网易加密只作用于 `db/`，`level.dat` 为明文），也能直接读取 `.mcworld` 文件。

## 命令参考

### `decrypt` / `encrypt` / `export`（三者参数一致）

| 参数 | 说明 |
| --- | --- |
| `<input>` | 存档 zip 或目录（自动识别、自动定位 `db/`） |
| `-o, --output <path>` | 输出路径。默认：decrypt/encrypt 为 `<输入名>_decrypted` / `_encrypted`（zip 输入 → zip，目录输入 → 目录）；export 为 `<输入名>.mcworld` |
| `-k, --key <key>` | 密钥。**默认按 ASCII 解析**，`hex:` / `0x` 前缀表示十六进制（避免 `88329851` 这类纯数字被误解为 4 字节 hex）。不指定时：decrypt 自动推导，encrypt 用官方默认 `88329851`，export 自动推导 |
| `--overwrite` | 输出已存在时覆盖（覆盖前会拒绝输出与输入相同的路径） |

行为差异：

- `decrypt`：解密 `db/` 内所有带魔数 `80 1D 30 01` 的文件，并对 CURRENT 做已知明文校验；遇到旧版魔数 `90 1D 30 01` 立即报错；
- `encrypt`：与游戏行为一致，仅加密 `CURRENT` / `MANIFEST-*` / `*.ldb`（`.log` 保持明文），加密后在内存中回读自检；
- `export`：解密 + 把世界内容提升到压缩包根打包为 `.mcworld`，并对产物做 `level.dat` / `db/CURRENT` 校验。

### `version`

`nmcat version <input> [--json]` —— 优先输出 `InventoryVersion` 字段，其次由 `lastOpenedWithVersion` 数组格式化（如 `[1,21,120,0,0]` → `1.21.120`）。适用于加密存档（`level.dat` 不加密），可用于确认网易存档与哪个国际版引擎版本兼容。

### `tui`

`nmcat tui [存档路径]` —— 上述全部能力的交互式封装：加载存档后展示信息面板，列出解密/加密/导出/重选/退出操作，确认时自动计算输出路径并提示覆盖，执行时显示逐文件进度，结束时给出校验结论。基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea)。

### `info`

`nmcat info <input> [--json]` —— 汇总 `level.dat`（明文 NBT）、`levelname.txt` 与 `db/` 扫描：世界名、引擎版本、最后游玩时间（自动兼容秒/毫秒存储）、游戏模式、难度、生成器、种子、出生点、游戏内时间、文件统计、db 加密状态与密钥可推导性。

## 加密机制速查

| 项 | 值 |
| --- | --- |
| 加密范围 | 仅 `db/` 内文件（`CURRENT`、`MANIFEST-*`、`*.ldb` 等）；`*.log` 与 `level.dat` 等不加密 |
| 文件格式 | 4 字节魔数 `80 1D 30 01` + 原文与 8 字节密钥循环逐字节异或 |
| 默认密钥 | ASCII `88329851`（网易官方 `XOREncryptDLL.dll` 内置） |
| 密钥推导 | LevelDB `CURRENT` 明文恒为 `MANIFEST-<序号>\n`，与密文异或即得重复两遍的密钥 |
| 旧版加密 | 魔数 `90 1D 30 01`（AES-CFB8，资源中心二次加密/模组），无法离线解密 |

详见 [docs/encryption.md](docs/encryption.md)（含四个开源工具的对比研究与真实存档验证）、[docs/mc-version.md](docs/mc-version.md)（App 版本号 ≠ MC 版本号的调研）。

## 导入国际版指南

nmcat 提供"网易存档 → 国际版可玩"的两种路径，推荐方式一：

### 方式一（推荐）：`nmcat export` 导出 `.mcworld`

```sh
nmcat export 网易存档.zip      # → 网易存档.mcworld
```

把 `.mcworld` 传到手机后，用文件管理器选择"用我的世界打开"（或国际版已在后台时点击文件），游戏会自动完成导入。两个要点：

- **世界内容必须在压缩包根**——嵌套一层世界子目录（如网易导出 zip 的 `ESfjmffkJN0=/level.dat`）会被新版游戏拒绝导入（Mojira MCPE-19966）。`nmcat export` 已自动处理，并对产物做自检；
- **游戏要能读到该文件**：文件放在游戏自有外部目录（`/storage/emulated/0/Android/data/com.mojang.minecraftpe/files/`）或应用可读的任意位置均可；放公共 `Download/` 需先给国际版授予"所有文件访问"权限，否则导入会静默失败。

### 方式二：手动复制解密目录

`nmcat decrypt` 后把解密目录复制进国际版的世界目录。注意**存储位置**：新版国际基岩默认从**应用私有目录**读世界（`/data/user/0/com.mojang.minecraftpe/games/com.mojang/minecraftWorlds`），放进传统外部路径 `/storage/emulated/0/Android/data/.../minecraftWorlds` 的世界会被隐藏（列表提示"在'设置'中更改存储位置"）；且复制后需 `chown -R` 为国际版应用 uid，否则应用无权读取。需要 root（或文件管理器的高级权限）。

### 兼容性

实测网易版 v3.9.15（基岩引擎 1.21.120）的加密世界，解密后在国际版 v1.21.120.4 中世界列表正常识别、可进入游玩（游泳、氧气、死亡等机制均正常）。`nmcat version` 可在迁移前确认两边引擎版本是否一致。

## 实战演练记录

2026-09-06 在真机（PLR110，Android 16，root）完成全链路验证：拉取网易加密世界 → nmcat 解密 → 两种方式导入国际版 → 实机进入世界运行（含 drowned 死亡机制等），logcat 无致命错误。完整记录与截图见 [`e2e-drill/REPORT.md`](e2e-drill/REPORT.md)（个人数据，不入库）。

## 常见问题（FAQ）

**解密报"旧版加密格式"（`90 1D 30 01`）？**
资源中心二次加密或早期版本使用 AES-CFB8，密钥需调用网易 API，社区目前无法离线解密。

**`info` 显示"密钥: 推导失败"？**
多为 CURRENT 与 `MANIFEST-*` 序号不匹配、存档损坏，或为资源中心二次加密（CURRENT 长约 80 字节）。可尝试 `decrypt --key 88329851` 直接解。

**导入后世界列表看不到世界？**
见[导入指南](#导入国际版指南)的存储位置说明——国际版默认只读应用私有目录。

**`.mcworld` 打开后只出现"导入中"提示但没有"导入完成"？**
导入中途失败了。检查文件是否可被游戏读取（权限），以及是否由旧版工具导出——`nmcat export` 已修复空名 zip 条目导致导入失败的问题（实测 MCPE 1.21.120），请使用最新版本重新导出。

**`最后游玩` 时间看起来不对？**
不同网易版本对 `LastPlayed` 分别以秒或毫秒存储，nmcat 已自动归一化（< 10¹² 视为秒）。若仍异常请附带 `--json` 输出提 issue。

## 项目结构

```
cmd/nmcat/                 入口（go install .../cmd/nmcat 得到 nmcat）
internal/crypt/            XOR 加解密、密钥推导与解析
internal/archive/          zip/目录统一抽象、db 定位、流式转换
internal/level/            基岩小端 NBT 读取器、level.dat 解析
internal/cli/              cobra 子命令（decrypt/encrypt/export/version/info/tui）
internal/tui/              交互式界面（Bubble Tea）
internal/                  内部测试（含真实存档集成测试）
testdata/                  测试存档（不入库）与夹具
docs/                      研究文档（加密机制、版本号调研）
.github/workflows/         Release 工作流（GoReleaser）
.goreleaser.yaml           多平台构建配置
e2e-drill/                 实机演练证据（个人数据，不入库）
```

## 测试与发版

```sh
go test ./...    # 单测 + 集成测试；testdata/ 存有真实加密存档时自动追加端到端验证
```

发版：推送 `v*` 标签（`git tag -a vX.Y.Z && git push origin vX.Y.Z`），GitHub Actions 会通过 GoReleaser 自动构建上述全部平台并发布 Release——无需手动上传产物。

## 免责声明

本项目仅供学习研究与自己存档的数据迁移使用，请勿用于处理他人存档或任何违反法律法规及《我的世界》相关服务条款的用途。使用本工具造成的任何损失由使用者自行承担。

## 许可证

[MIT](LICENSE)
