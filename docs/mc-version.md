# 调研：网易我的世界 Android 版如何获取 MC（基岩引擎）真实版本号

> 调研日期：2026-09-06。设备：PLR110（Android 16，root 可用，adb 已连接）。
> 对象：com.netease.x19（App versionName `3.9.15.297907`，versionCode `840297907`，当天冷启动并进入过游戏）。
> 结论先行：**网易 App 版本号与 MC 基岩引擎版本号是两套编号**。本机 App 为 3.9.15.297907，MC 本体为 **`1.21.120`**，已用 3 个相互独立的文件来源交叉证实。

## 1. 术语澄清：两套"引擎版本"

| 概念 | 本机实测值 | 出处 |
| --- | --- | --- |
| App（启动器）版本 | `3.9.15.297907` / versionCode `840297907` | `dumpsys package com.netease.x19` |
| 网易客户端引擎版本（网易自研整包框架） | `3.9.15.297907`（与 versionName 一致） | `/data/user/0/com.netease.x19/files/games/com.netease/storge/stream/version` |
| **MC（基岩引擎）本体版本** | **`1.21.120`** | 见 §2/§3，多处独立来源 |

注意陷阱：`storge/stream/version` 这个文件名叫 version，内容为
`{"patch_version": "", "engine_version": "3.9.15.297907"}`——这里的 `engine_version` 是**网易客户端引擎**版本（等于 App 版本），**不是** MC 本体版本。

另外，`shared_prefs/ChannelVersionsLog.xml` 里记录的 `base`/`netease`/`channel_*` 等几十个版本号全部是 SDK 组件版本；APK 内 `assets/__version`（`release-v1.15.0.4`）是 unisec 安全 SDK 版本，`version.txt`（`isoparser-1.0.6`）是三方库版本——都不是 MC 版本。

## 2. 方法 A（最推荐）：`telemetry_info.json`

路径（引擎自己的遥测文件，与国际版 MCPE 的 `games/com.mojang/minecraftpe/telemetry_info.json` 同构）：

```
/data/user/0/com.netease.x19/games/com.netease/minecraftpe/telemetry_info.json
```

本机内容：

```json
{
   "file_version" : 1,
   "guard" : "34a09d28-56e7-380c-82f8-921b09fb4e2b",
   "lastsession_Build" : "1.21.120",
   "lastsession_id" : "9736b0f8-5ec9-4c9e-8768-b4147b5e637e"
}
```

`lastsession_Build` 即基岩引擎版本。取用方式：

```bash
adb exec-out "su -c 'cat /data/user/0/com.netease.x19/games/com.netease/minecraftpe/telemetry_info.json'"
```

- 优点：一个 178 字节的小文件、纯 JSON、一次冷启动游戏后必然生成/更新。
- 注意：它记录的是**上一次会话**的引擎版本，App 更新后要重新启动过一次游戏才会刷新（本机安装与启动在同一天，无需担心）。
- 同目录还有 `hs.hex`、`options.txt`（13KB，引擎选项）等，但版本信息只在 telemetry 里。

## 3. 方法 B（无需进入游戏逻辑，适合离线/备份件）：世界 `level.dat`

网易加密存档的 `level.dat` **本身是明文**（加密只作用于 `db/`，见本仓库上一任务的研究），NBT 中直接写着引擎版本。两种读法：

```bash
# 设备上一行搞定（su 可读 /sdcard/Android/data）：
adb shell "su -c 'grep -aoE \"InventoryVersion.{0,16}\" /storage/emulated/0/Android/data/com.netease.x19/files/minecraftWorlds/<世界目录>/level.dat'"
# 实测输出： InventoryVersion 1.21.120
```

本机世界 `ESfjmffkJN0=/level.dat` 的完整解析（小端 NBT，`level.dat` = 4 字节存储版本 + 4 字节长度 + NBT）：

| NBT 字段 | 类型 | 值 |
| --- | --- | --- |
| `lastOpenedWithVersion` | TAG_List\<TAG_Int\> | `[1, 21, 120, 0, 0]` |
| `InventoryVersion` | TAG_String | `"1.21.120"` |

`lastOpenedWithVersion` 五元组 `[1, 21, 120, 0, 0]` 就是基岩版本号 1.21.120 的整数形式。该方法对任意一个网易世界都适用，且**在电脑上解析备份 zip 也能用**（把 zip 里的 `*/level.dat` 拉出来读 NBT 即可，无需 root、无需设备）。

## 4. 方法 C：`libminecraftpe.so` 字符串（APK 内，最"静态"）

引擎 so 位于 APK 内并已解压：

```
/data/app/~~wS27C-Ex-RiflTDJxafgsw==/com.netease.x19-9lfYurhs0MfFv73xwjOvZg==/lib/arm64/libminecraftpe.so   (334 MB)
```

```bash
adb shell "su -c 'grep -aoE \"1\.21\.1[0-9]{1,2}\" <so 路径> | sort | uniq -c'"
# 3 次 1.21.120，3 次 1.21.110（后者为历史兼容串）
```

三处 `1.21.120` 上下文均为 `format_version 1.21.120 or higher` 类特性描述——引擎以自身支持的特性级别写这句，即其版本为 1.21.120。so 中没有 4 段式完整版本号（网易在运行时拼接）。

注意 APK 本体 2.3 GB，不要整包 pull；设备上没有 unzip，可用已装的 Termux：

```bash
adb shell "su -c 'LD_LIBRARY_PATH=/data/data/com.termux/files/usr/lib \
  /data/data/com.termux/files/usr/bin/unzip -l <apk 路径> | grep -iE version'"
adb shell "su -c 'LD_LIBRARY_PATH=/data/data/com.termux/files/usr/lib \
  /data/data/com.termux/files/usr/bin/unzip -p <apk 路径> assets/__version'"   # 但这是 SDK 版本，不是 MC 版本
```

## 5. 实测无效的途径（避免再踩）

| 途径 | 结果 |
| --- | --- |
| `logcat`（含按 PID 过滤，冷启动至引擎加载阶段） | **引擎不向 logcat 输出任何版本行**，网易重定向了引擎日志；只有启动器侧标签（`DynamicFramerate`、`LoadedApk` 等）。主 Activity 名为 `com.mojang.minecraftpe.MainActivity`（网易沿用国际版 Activity 名） |
| `/data/user/0/com.netease.x19/files/mcp.log` | 启动器 JS 桥日志，**每行 base64 编码**；解码后只有家园组件 `engine_version: 0.0 :: 3.9` 和 unifix 的 versionCode 事件，无 MC 版本 |
| `games/com.netease/storge/stream/version` | `engine_version` 是网易客户端版本（=App 版本），易误读 |
| APK `assets/`（`__version`、`version.txt` 等） | 均为 SDK/三方库版本 |
| `shared_prefs/`（`ChannelVersionsLog.xml` 等） | 全是 SDK 组件版本 |

## 6. 各路径一览（本机实测）

```
/data/user/0/com.netease.x19/
├── games/com.netease/minecraftpe/telemetry_info.json   ← 方法 A：lastsession_Build = "1.21.120"
├── shared_prefs/ChannelVersionsLog.xml                 ← SDK 组件版本（非 MC）
├── files/games/com.netease/
│   ├── manifest                                        ← 资源 hash 清单（无版本串）
│   ├── vanilla.mcp / vanilla_patch.mcp                 ← "MCPK" 容器，23.8MB ×2
│   ├── storge/stream/version                           ← 网易客户端引擎版本（=App 版本，非 MC）
│   └── minecraftpe/{options.txt,clientId.txt,...}
/storage/emulated/0/Android/data/com.netease.x19/
├── files/mcp.log                                       ← base64 行的启动器日志
├── files/minecraftWorlds/<世界>/level.dat              ← 方法 B：InventoryVersion / lastOpenedWithVersion
/data/app/~~…==/com.netease.x19-…==/
├── base.apk (2.3 GB)
└── lib/arm64/libminecraftpe.so (334 MB)                ← 方法 C：format_version 特性串
```

## 7. 建议的获取脚本

```bash
# 一行版（root；读引擎遥测，最直接）：
adb exec-out "su -c 'cat /data/user/0/com.netease.x19/games/com.netease/minecraftpe/telemetry_info.json'" \
  | grep -oE '"lastsession_Build"[^,]+'

# 无 root / 有任意一个世界备份时（PC 端解析 level.dat，明文 NBT）：
adb exec-out "su -c 'grep -aoE \"InventoryVersion.{0,16}\" <世界目录>/level.dat'"
```

## 附：取证方式说明

调研过程中拉取过的原始证据（`telemetry_info.json`、`ChannelVersionsLog.xml`、base64 解码后的 `mcp.log`、logcat 抓取等）已在归档后清理，本文所有命令与输出即为可复现的记录。设备侧操作全部为只读（ls/cat/find/grep/unzip -p），实验性启动游戏后已 `am force-stop` 恢复原状。
