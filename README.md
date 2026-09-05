# nmcat · netease-mc-archive-tool

网易我的世界（中国版）基岩存档命令行工具，使用 Go 实现。

> 解密 / 加密网易加密存档，读取 MC（基岩引擎）真实版本号与世界基本信息。
> 算法与密钥推导原理的完整研究见 [docs/encryption.md](docs/encryption.md)。

## 功能

| 命令 | 功能 |
| --- | --- |
| `nmcat decrypt` | 解密网易 XOR 加密存档（zip 或目录，自动推导密钥） |
| `nmcat encrypt` | 加密存档（国际版 → 网易版），默认密钥与官方一致 |
| `nmcat version` | 读取存档对应的 MC（基岩引擎）真实版本号（≠ App 版本号） |
| `nmcat info` | 查看存档基本信息：世界名、版本、最后游玩、模式、种子、加密状态等 |

支持 zip 压缩包与目录两种输入，解密/加密均为流式处理，大存档不会整体载入内存；默认输出到新文件，**不修改输入**。

## 安装

```sh
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

`version` 与 `info` 对加密存档同样有效（网易加密只作用于 `db/`，`level.dat` 为明文）；两者均支持 `--json` 输出机器可读结果。

## 常用参数

```sh
nmcat decrypt 存档.zip -o 输出.zip          # 指定输出路径
nmcat decrypt 存档.zip -k 88329851          # 指定密钥（默认按 ASCII；hex: / 0x 前缀表示十六进制）
nmcat encrypt 存档.zip --overwrite          # 输出已存在时覆盖
nmcat version 存档目录/ --json              # 机器可读输出
```

## 加密机制速查

| 项 | 值 |
| --- | --- |
| 加密范围 | 仅 `db/` 内文件（`CURRENT`、`MANIFEST-*`、`*.ldb` 等）；`*.log` 与 `level.dat` 等不加密 |
| 文件格式 | 4 字节魔数 `80 1D 30 01` + 原文与 8 字节密钥循环逐字节异或 |
| 默认密钥 | ASCII `88329851`（网易官方 `XOREncryptDLL.dll` 内置） |
| 密钥推导 | LevelDB `CURRENT` 明文恒为 `MANIFEST-<序号>\n`，与密文异或即得重复两遍的密钥 |
| 旧版加密 | 魔数 `90 1D 30 01`（AES-CFB8，资源中心二次加密/模组），无法离线解密 |

详见 [docs/encryption.md](docs/encryption.md)（含四个开源工具的对比研究与真实存档验证）、[docs/mc-version.md](docs/mc-version.md)（App 版本号 ≠ MC 版本号的调研）。

## 项目结构

```
cmd/nmcat/        入口
internal/crypt/   XOR 加解密、密钥推导与解析
internal/archive/ zip/目录统一抽象、db 定位、流式转换
internal/level/   基岩小端 NBT 读取器、level.dat 解析
internal/cli/     cobra 子命令
testdata/         测试存档（不入库）与单测夹具
docs/             研究文档
```

## 测试

```sh
go test ./...
```

单元测试覆盖加解密、密钥推导（含真实样例向量）、NBT 解析与存档转换的往返一致性；若 `testdata/` 下放置了真实网易加密存档 zip，集成测试会额外执行端到端验证（解密 → 再加密 → 与原始逐条目字节比对）。

## 免责声明

本项目仅供学习研究与自己存档的数据迁移使用，请勿用于处理他人存档或任何违反法律法规及《我的世界》相关服务条款的用途。使用本工具造成的任何损失由使用者自行承担。

## 许可证

[MIT](LICENSE)
