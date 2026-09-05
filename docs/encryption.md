# 网易《我的世界》（中国版）基岩存档加密/解密机制详解

> 本文基于对四个开源逆向/解密项目的源码分析、网易官方 `XOREncryptDLL.dll` 的字符串/导出分析，以及一个真实网易加密存档（`ESfjmffkJN0=.zip`）的完整复现验证撰写。文中所有结论均可按 [§6 验证记录](#6-真实存档验证记录可复现) 复现。
>
> **仅供学习研究与自己存档的数据迁移。请勿用于处理他人存档或其他违规用途。**

---

## 目录

1. [TL;DR](#1-tldr)
2. [背景：网易为什么加密存档](#2-背景网易为什么加密存档)
3. [存档结构与存放路径](#3-存档结构与存放路径)
4. [加密格式详解](#4-加密格式详解)
5. [密钥恢复算法（已知明文攻击）](#5-密钥恢复算法已知明文攻击)
6. [真实存档验证记录（可复现）](#6-真实存档验证记录可复现)
7. [参考实现](#7-参考实现)
8. [四个开源项目对比与实现细节](#8-四个开源项目对比与实现细节)
9. [反向加密（国际版 → 网易版）注意事项](#9-反向加密国际版--网易版注意事项)
10. [边界情况与 FAQ](#10-边界情况与-faq)
11. [附录：LevelDB 文件格式速查](#11-附录leveldb-文件格式速查)
12. [参考资料](#12-参考资料)

---

## 1. TL;DR

网易中国版《我的世界》（基岩版引擎）对存档目录 `db/`（LevelDB 数据库）内的文件做了**极简的循环 XOR 加密**：

```
加密文件 = [ 魔数 80 1D 30 01 ] + [ 原文逐字节 XOR 8字节密钥（循环） ]
```

- **魔数**：`80 1D 30 01`（4 字节，大端即 `0x801D3001`）。凡是以它开头的文件就是加密文件，其余文件一律是明文。
- **密钥**：8 字节。网易官方 `XOREncryptDLL.dll` 中硬编码的默认密钥为 ASCII **`88329851`**（hex `38 38 33 32 39 38 35 31`）。同一加密库也可能用其他密钥，因此社区工具普遍支持从存档自动推导。
- **密钥恢复（已知明文攻击）**：LevelDB 的 `db/CURRENT` 文件明文恒为 `MANIFEST-<6位以上序号>\n`（16 字节，恰好两个密钥周期）。取加密 CURRENT 去掉魔数后的密文与该已知明文异或，即得 `密钥||密钥`，取前 8 字节即可。
- **解密**：去掉 4 字节魔数后整体与密钥循环异或。加密是其自身的逆运算。
- **加密范围**：`db/` 内**除 `*.log`（WAL）以外**的所有文件（`CURRENT`、`MANIFEST-*`、`*.ldb` 等）。`level.dat`、`levelname.txt`、`world_icon.jpeg` 等存档根目录下的文件**不加密**。
- **旧版加密**：魔数 `90 1D 30 01`，为 AES-128-CFB8（用于早期版本及资源中心/模组内容的“二次加密”），密钥需走网易 API 获取，目前无法离线解密。

本项目提供了可复现的 Go 命令行工具 [nmcat](../README.md)（`nmcat decrypt` / `nmcat encrypt`），对真实网易加密存档的验证结果：密钥成功推导为 `88329851`，解密后 LevelDB 元数据全部 CRC 校验通过，且「解密 → 再加密」与原始加密存档**字节级完全一致**。

---

## 2. 背景：网易为什么加密存档

网易代理的中国版《我的世界》使用与 Mojang 国际版相同的基岩版（Bedrock Edition）引擎，存档同样是标准的基岩版世界目录（`level.dat` + LevelDB 的 `db/`）。为了把中国版存档与国际版生态隔离（防导入导出、绑定自家资源中心/联机体系），网易对存档的 LevelDB 数据库文件加了一层自研的轻量加密，导致：

- 国际版（Mojang）无法直接读取网易版存档，反之亦然；
- 第三方工具（如 Amulet、MCEdit、 behaviors 分析脚本）打开网易存档会报“损坏”。

社区的应对方式就是本文的主角：四种开源/逆向工具，全部围绕同一套算法。加密本身非常弱（固定 8 字节 XOR + 可从存档内已知明文恢复密钥），其作用更接近“格式隔离”而非密码学保护。

**加密的位置**：加密由网易随启动器/游戏分发的原生库 `XOREncryptDLL.dll` 实现（32 位 Windows DLL，PDB 路径 `I:\xor_encrypt_dll_project\Release\XOREncryptDLL.pdb`），导出三个 C 接口：

| 导出函数 | 作用 |
| --- | --- |
| `check_file_is_encrypt(path)` | 判断文件是否带加密魔数，是则返回 1 |
| `decrypt_file(path, len, out_buf, out_len)` | 解密单个文件，输出到临时文件 |
| `encrypt_file(path, len, out_buf, out_len)` | 加密单个文件 |

该 DLL 的字符串常量中明文写着默认密钥 **`88329851`**（以及 `"not encrypted"`、`"is encrypted"`、`"decrypt faild"` 等日志字符串）。`Carbonateds/MCWorld-Converter` 仓库直接附带了这个 DLL 并通过 P/Invoke 调用；`Redamancy520/MinecraftWorld-Decryption-Tools`（网易自家工具链 `MCStudio` 的反编译代码）中也以 `LevelDbEncryptHelper` 的形式调用同一 DLL。

在 LevelDB 层面，`HTMonkeyG/leveldb-mcne` 项目按照网易的行为改写了 LevelDB 的 `Env` 层（`McneWrapper`），让标准 LevelDB 透明读写加密文件——这也从工程上反向印证了网易的做法：**加密套在文件读写层，数据库逻辑本身未改**。

---

## 3. 存档结构与存放路径

### 3.1 网易基岩存档目录结构（实测样例 `ESfjmffkJN0=.zip`）

```
ESfjmffkJN0=/                        ← 世界目录（网易用 base64 随机串命名，解码后为 8 字节随机 ID）
├── level.dat                        ← 世界元数据（未压缩 NBT：4字节版本 + 4字节长度 + NBT）【明文】
├── level.dat_old                    ← level.dat 的备份【明文】
├── levelname.txt                    ← 世界名，如 “我的世界”（UTF-8）【明文】
├── world_icon.jpeg                  ← 世界图标【明文】
├── world_icon.json                  ← {"captureTime": 1788628246}【明文】
├── netease_world_behavior_packs.json← 网易特有：行为包声明（无包时为字面量 null）【明文】
├── netease_world_resource_packs.json← 网易特有：资源包声明【明文】
├── behavior_packs/                  ← 行为包目录
├── resource_packs/                  ← 资源包目录
└── db/                              ← LevelDB 数据库（唯一被加密的部分）
    ├── 000007.log                   ← WAL 预写日志                【明文！不加密】
    ├── CURRENT                      ← 指向当前 MANIFEST，明文恒为 “MANIFEST-000006\n” 【加密】
    └── MANIFEST-000006              ← 数据库元数据（VersionEdit 记录）             【加密】
    （老存档还会有 00000N.ldb 等 SSTable 文件，同样【加密】）
```

要点：

- **只有 `db/` 内的文件被加密，且 `*.log` 例外**。`leveldb-mcne` 的 README 原文：“数据库除 .log 外的所有文件均会被加密”；其实现中 `MaybeEncrypted()` 对所有文件返回 true、仅排除 `.log` 后缀。实测样例与此完全一致（`CURRENT`、`MANIFEST-000006` 带魔数，`000007.log` 是 31/31 条 CRC 全部有效的标准明文 WAL）。
- 推测原因：WAL 是崩溃恢复用的临时文件，游戏打开世界时会重建，而 `CURRENT`/`MANIFEST`/`*.ldb` 才是持久化数据。网易只在持久化文件上落加密（也可能是在存档关闭/落盘流程中对非日志文件统一处理）。无论如何，解密工具应**以魔数为准**逐文件判断，而不是按文件名猜测——明文的 `.log` 不会带魔数，天然不会被误处理。
- 网易特有文件 `netease_world_*.json`：网易导入存档时靠它们识别附加包；把国际版存档转成网易版时，`MCWorld-Converter` 会把这两个文件覆写为 `null` 来规避校验问题。
- 世界目录名（如 `ESfjmffkJN0=`）是 base64 字符串（base64 解码为 8 字节 `11 27 e3 99 f7 e4 24 dd`），网易用它代替国际版的可读世界名，这也是“网易存档在文件管理器里显示乱码名”的原因。

### 3.2 存放在哪里

| 平台 | 路径 |
| --- | --- |
| Windows 网易端 | `C:\Users\<用户名>\AppData\Roaming\MinecraftPE_Netease\minecraftWorlds\` |
| 安卓（网易官方渠道） | `Android/data/com.netease.x19/files/minecraftWorlds/` |
| 安卓（渠道服，如 4399） | `Android/data/com.netease.mc.m4399/files/games/com.netease/MinecraftWorlds/` |
| 安卓（导入入口） | `Android/data/com.netease.x19/files/importWorlds/` |

> 渠道不同包名不同，找不到时直接搜索 `minecraftWorlds` 目录。世界目录名是 base64 串，可借 `levelname.txt`（网易加密存档中它是明文）或 `world_icon.jpeg` 识别是哪个世界。

---

## 4. 加密格式详解

### 4.1 新版 XOR 加密（现行格式，魔数 `80 1D 30 01`）

对 `db/` 内每个需要加密的文件（除 `.log`）：

```
加密：
  ciphertext = plaintext[i] XOR key[i mod 8]        （i 从 0 计）
  加密文件   = 80 1D 30 01 || ciphertext

解密（加密是其自身的逆运算）：
  plaintext[i] = file[4 + i] XOR key[i mod 8]       （丢弃前 4 字节魔数，i 从 0 计）
```

- **密钥长度**：8 字节，按字节循环。`leveldb-mcne` 实现支持任意非零长度密钥（`offset % key.size()`），但网易现网为 8 字节。
- **相位对齐**：密钥相位从**去掉魔数后**的字节 0 开始（即 `key[0]` 对应文件偏移 4）。`leveldb-mcne` 的 `McneRandomAccessFile::Read` 先从 `kMagicNumSize + offset` 读数据、再以 `offset % keylen` 为相位异或，与此一致。
- **魔数语义**：`McneWrapper` 中注释为 `kMagicNum = 0x01301D80 (big endian)`，即文件前 4 字节依次为 `80 1D 30 01`。魔数后紧跟密文，没有长度/对齐填充，加密后文件大小 = 原大小 + 4。
- **文件级开关**：一个文件是否加密完全取决于它是否以魔数开头。同一个 `db/` 里可以同时存在加密与非加密文件（实测中 `.log` 就是明文）。

### 4.2 默认密钥 `88329851`

| 证据 | 来源 |
| --- | --- |
| 官方 `XOREncryptDLL.dll` 的只读数据段中存在 ASCII 字符串 `88329851`（该 DLL 无其他候选密钥字符串） | `Carbonateds/MCWorld-Converter` 仓库附带的官方 DLL，本文用 `strings`/PE 解析确认 |
| `leveldb-mcne` 的 `McneWrapper` 默认参数：`McneWrapper(Env*, const Slice &key = "88329851")` | [leveldb-mcne/leveldb_mcne.h](https://github.com/HTMonkeyG/leveldb-mcne/blob/main/leveldb_mcne.h) |
| `XOR-MC-Archive-Decrypt` 的 `encryptFile/decryptFile` 默认密钥 `Buffer.from('88329851')` | [XOR-MC-Archive-Decrypt/includes/XOREncryptHelper.js](https://github.com/HTMonkeyG/XOR-MC-Archive-Decrypt/blob/main/includes/XOREncryptHelper.js) |
| 实测样例存档推导出的密钥恰为 `88329851` | 见 §6 |

因此大多数网易存档直接用 `88329851` 就能解开。但社区工具仍保留了“从存档推导密钥”的路径（§5），原因有三：

1. 不排除网易在某些版本/场景（如资源中心下发的地图）使用非默认密钥；
2. 推导逻辑可以顺带校验存档完整性和加密格式（前后 8 字节不重复即报错）；
3. 官方加密 API（`encrypt_file`）理论上允许调用方传入密钥。

### 4.3 旧版加密与其他变体（魔数 `90 1D 30 01` 等）

- **`90 1D 30 01`（旧版加密头）**：`NeteaseMcDencrypter` 的 `Crypt.bHeader` 注释为“旧版加密头”，并说明该格式为 **AES-CFB8**（`leveldb-mcne` 注释称其为 "for AES-128"）。密钥不在存档内，需调用网易 API 才能取得，故社区工具一律拒绝处理（`NeteaseMcDencrypter` 报“存档使用了旧版加密，无法获取秘钥”；`NetEaseMC-Decryptor` 报“存档未加密或使用旧版加密，无法解密”）。`NetEaseMC-Decryptor` 实测覆盖 `3.3.35.270123` 及以下的网易客户端版本。
- **资源中心二次加密（模组加密）**：从资源中心下载的地图/组件可能在此之上再套一层与模组相同的加密。特征之一是 `db/CURRENT` 文件长达约 80 字节（而非正常的 16 字节），使已知明文攻击失效（`XOR-MC-Archive-Decrypt` 对此报“获取密钥失败”，`MCWorld-Converter` 注明“资源工坊存档据传有特殊二次加密，暂无法解密”）。`NeteaseMcDencrypter` 的扫描功能把 `90 1D 30 01` 文件标注为“模组加密”。
- **“MANI” 伪魔数**：`NeteaseMcDencrypter` 中还定义了 `cHeader = "MANI"`。这不是加密格式——明文 `CURRENT` 的内容以 `MANIFEST-…` 开头，前 4 字节自然是 ASCII `MANI`，作者用它识别“未加密存档”。

---

## 5. 密钥恢复算法（已知明文攻击）

### 5.1 原理

LevelDB 规定：`db/CURRENT` 是一个文本文件，内容**恒为**当前 MANIFEST 文件名 + 换行符，即 `MANIFEST-<seq>\n`（如 `MANIFEST-000006\n`，16 字节）。同时 `db/` 下一定存在同名文件 `MANIFEST-<seq>`（序号相同的文件名）。

于是：

```
加密 CURRENT 去魔数后的密文 = "MANIFEST-000006\n" XOR (key || key)     ← 16 字节 = 两个密钥周期
⟹ key = 密文 XOR "MANIFEST-000006\n" 的前 8 字节
```

由于明文（16 字节）恰为两个密钥周期，异或结果的前 8 字节与后 8 字节**应当完全相同**——这既是密钥，也是格式校验：两半不一致说明存档损坏、加密格式不符或 CURRENT 与 MANIFEST 序号不匹配。

### 5.2 逐步演示（真实样例）

样例存档 `ESfjmffkJN0=.zip` 中：

```
db/CURRENT          = 80 1D 30 01 | 75 79 7D 7B 7F 7D 66 65 15 08 03 02 09 08 03 3B   （20 字节）
db/MANIFEST-000006  →  已知明文 "MANIFEST-000006\n"
                    = 4D 41 4E 49 46 45 53 54 2D 30 30 30 30 30 36 0A                  （16 字节）
```

异或（密文 XOR 已知明文）：

```
75^4D=38  79^41=38  7D^4E=33  7B^49=32  7F^46=39  7D^45=38  66^53=35  65^54=31   → 38 38 33 32 39 38 35 31
15^2D=38  08^30=38  03^30=33  02^30=32  09^30=39  08^30=38  03^36=35  3B^0A=31   → 38 38 33 32 39 38 35 31
```

前 8 字节 == 后 8 字节，密钥为 `38 38 33 32 39 38 35 31`，即 ASCII **`88329851`** —— 与官方 DLL 中的硬编码密钥完全一致。

### 5.3 实现要点（四个项目做法一致）

| 步骤 | Java (`Crypt.getKey`) | JS (`main.js` 主动解密) | Web (`getKey`) |
| --- | --- | --- | --- |
| 取 CURRENT 去魔数后的密文 | `readCheckHeader(CURRENT)` | `preprocess.after.subarray(4)` | `currentFileData.slice(4)` |
| 构造已知明文 | `manifestName + 0x0A` | `MANIFEST-* 文件名 + '\n'` | `manifestName` 后 append `0x0A` |
| 异或取 keystream | `xor(bin, source)` | 逐字节 `buf1[i] ^= buf2[i]` | `xor(encryptedData, source)` |
| 校验 + 截取 8 字节 | `optimizeKey()`：前 8 字节与后 8 字节逐一比对，不一致打警告 | `buf1[i] != buf1[i & 0x07]` 则失败 | `optimizeKey()`：长度 16 且两半相同才取 8 字节 |

细节差异：三方对“两半不一致”的兜底不同——Java 版打印警告后**返回完整 16 字节 keystream 当作密钥继续**；网页版 `optimizeKey()` 同样在两半不同时静默退回 16 字节 keystream（数学上与 8 字节密钥等价——keystream 本身就是 `key||key`——但等于放弃了损坏检测）；XOR-MC 版则直接报“获取密钥失败”。建议采用第三种（快速失败）。

### 5.4 无 CURRENT 可用时

若 `CURRENT` 缺失、损坏，或存档走了资源中心二次加密（CURRENT 约 80 字节），可以尝试：

1. 用默认密钥 `88329851` 直接解密，解开后看 `CURRENT` 明文是否为 `MANIFEST-…\n`、`.ldb` 文件尾 8 字节是否为 LevelDB SSTable 魔数 `DB 47 75 24 8B 80 FB 57`（大端读为 `0x57FB808B247547DB`）来判定成功与否；
2. `MANIFEST-*` 文件第一条 VersionEdit 记录内含明文比较器名 `leveldb.BytewiseComparator`，也可作已知明文（但其在记录中的偏移受 varint 头影响，不如 CURRENT 方便）；
3. 两者都失败则大概率是旧版 AES 加密或二次加密，社区目前无离线解法。

---

## 6. 真实存档验证记录（可复现）

**验证对象**：`ESfjmffkJN0=.zip`（214,871 字节，2026-09-06 打包的网易加密存档，内含世界目录 `ESfjmffkJN0=/`）。

### 6.1 加密状态盘点

| 文件 | 大小 | 首 4 字节 | 状态 |
| --- | --- | --- | --- |
| `db/CURRENT` | 20 | `80 1D 30 01` | 加密（XOR） |
| `db/MANIFEST-000006` | 54 | `80 1D 30 01` | 加密（XOR） |
| `db/000007.log` | 894,777 | `A6 D7 41 27`（LevelDB 记录 CRC） | **明文 WAL，未加密** |
| `level.dat` | 3,185 | `0A 00 00 00`（NBT） | 明文 |
| `levelname.txt` | 12 | `E6 88 91 …`（“我的世界”） | 明文 |
| `world_icon.jpeg` | 17,060 | `FF D8 FF E0`（JPEG） | 明文 |
| 其余 json / level.dat_old | — | — | 明文 |

### 6.2 验证脚本与结果

参考实现为本项目工具 `nmcat`（等价的 Python 验证脚本曾在研究阶段使用并得出相同结果）：

```console
$ nmcat decrypt testdata/ESfjmffkJN0=.zip
[i] 输入:      testdata/ESfjmffkJN0=.zip (zip)
[i] db 目录:   ESfjmffkJN0=/db
[i] 密钥:      88329851（hex 3838333239383531，来源: 自动推导）
[i] 已解密: 2 个文件: CURRENT, MANIFEST-000006
[✓] 校验:     CURRENT 明文校验通过（MANIFEST-000006）
[✓] 已写出:   testdata/ESfjmffkJN0=_decrypted.zip（10 个条目）
```

工具与测试内置了多重独立校验，不依赖“打开游戏看效果”：

1. **密钥自检**：keystream 前 8 字节与后 8 字节必须相同（§5）；
2. **CURRENT 已知明文**：解密结果必须逐字节等于 `MANIFEST-000006\n`（推导密钥时天然满足，指定密钥时用于拦截错误密钥）；
3. **MANIFEST 软校验**：解密结果应含 `leveldb.BytewiseComparator`；
4. **往返一致性**（`go test ./...` 集成测试内置）：把解密结果再加密，与原始加密存档做逐条目 SHA-256 比对：

```console
$ nmcat encrypt testdata/ESfjmffkJN0=_decrypted.zip -o /tmp/re.zip
[i] 输入:      testdata/ESfjmffkJN0=_decrypted.zip (zip)
[i] db 目录:   ESfjmffkJN0=/db
[i] 密钥:      88329851（hex 3838333239383531，来源: 官方默认）
[i] 已加密: 2 个文件: CURRENT, MANIFEST-000006
[✓] 已写出:   /tmp/re.zip（10 个条目）
```

研究阶段另用独立 Python 脚本做过更深的验证（此处记录结论）：解密后的 MANIFEST 以 CRC32C（Castagnoli，含 LevelDB mask 变换）逐条校验 log 记录帧 2/2 全过；明文 WAL `000007.log` 31/31 条记录 CRC 全过——证明解密结果与未加密文件整体自洽。

以上结果均可重复：构建 nmcat（`go build ./cmd/nmcat`）后对 [`testdata/ESfjmffkJN0=.zip`](../testdata/) 依次执行上面两条命令，并比对两个 zip 的逐条目字节即可。

### 6.3 本样例没有 `*.ldb` 文件说明

这是一个刚创建的世界的导出：所有键值尚在 WAL（`000007.log`）中，还未触发 LevelDB compaction，所以没有 SSTable。世界游玩一段时间后再备份，`db/` 中就会出现加密的 `00000N.ldb`；判断其解密是否正确，可用文件尾 8 字节 SSTable 魔数 `0x57FB808B247547DB`（大端）校验——这正是 `XOR-MC-Archive-Decrypt` 的 `avalTest()` 做的事。

---

## 7. 参考实现

`nmcat`（本项目工具，Go 实现，见 [README](../README.md)）：

```console
# 解密：输入 zip 或目录，自动定位 db/、推导密钥、解密并校验
nmcat decrypt 网易存档.zip              # → 网易存档_decrypted.zip
nmcat decrypt 网易存档目录/              # → 网易存档目录_decrypted/

# 指定密钥（默认按 ASCII；hex: 前缀表示十六进制）
nmcat decrypt 网易存档.zip --key 88329851

# 反向加密（国际版 → 网易版），仅加密 CURRENT / MANIFEST-* / *.ldb，与游戏行为一致
nmcat encrypt 国际版存档目录/
```

核心逻辑（完整算法只需十几行）：

```go
var Magic = []byte{0x80, 0x1D, 0x30, 0x01}

func XOR(data, key []byte) []byte {
    out := make([]byte, len(data))
    for i, b := range data {
        out[i] = b ^ key[i%len(key)]
    }
    return out
}

// 已知明文攻击：CURRENT 明文恒为 "MANIFEST-xxxxxx\n"（两个密钥周期）
func DeriveKey(currentEnc []byte, manifestName string) ([]byte, error) {
    plain := append([]byte(manifestName), '\n')
    ks := XOR(currentEnc[4:], plain)
    if !bytes.Equal(ks[:8], ks[8:16]) {
        return nil, errors.New("keystream 前后两半不一致")
    }
    return ks[:8], nil
}

func DecryptFile(data, key []byte) ([]byte, error) {
    if !bytes.HasPrefix(data, Magic) {
        return nil, errors.New("缺少加密魔数")
    }
    return XOR(data[4:], key), nil
}
```

---

## 8. 四个开源项目对比与实现细节

| | [NetEaseMC-Decryptor](https://github.com/ihaiming/NetEaseMC-Decryptor)（网页版） | [MCWorld-Converter](https://github.com/Carbonateds/MCWorld-Converter)（C#/WinForms） | [NeteaseMcDencrypter](https://github.com/Jerbvsjhs/NeteaseMcDencrypter)（Java） | [XOR-MC-Archive-Decrypt](https://github.com/HTMonkeyG/XOR-MC-Archive-Decrypt)（Node.js） |
| --- | --- | --- | --- | --- |
| 运行环境 | 浏览器（JSZip），纯前端 | Windows + 自带官方 `XOREncryptDLL.dll` | JVM | Node.js（npm 包 `@htmonkeyg/cryptmc`） |
| 核心算法来源 | AS5L 的网页版核心算法 | **直接调用网易官方 DLL** | 社区逆向 | 作者称最早从 `XOREncrypt.dll` 逆向提取并开源 |
| 密钥来源 | 自动推导（CURRENT+MANIFEST） | 官方 DLL 内置（`88329851`） | 自动推导；也可保存/加载密钥文件 | 默认 `88329851`；或自定义 hex 密钥；或自动推导（“自适应”） |
| 处理范围 | zip/文件夹内**所有带魔数的文件**（自动定位 db 目录，重打包整个 zip） | 世界根目录与一级子目录内**所有带魔数的文件** | `db/` 目录内**所有带魔数的文件** | `db/` 内**仅** `CURRENT`、`MANIFEST-*`、`*.ldb`（按文件名正则筛选） |
| 旧版 `90 1D 30 01` | 拒绝并提示 | 由官方 DLL 判断 | 扫描时标注“模组加密”，解密时拒绝 | `checkFileIsEncrypt` 视为加密，但解密同样无能为力 |
| 输出方式 | 生成新 zip，原文件不动 | 解密结果覆盖原文件（有日志） | 原地覆盖 db 文件 | 复制到 `*_Dec` / `*_Enc` 新目录，原存档不动 |
| 特色 | 零安装、隐私（本地处理）、中英双语、自动定位 db | 单文件 exe、无需理解算法 | 功能最全：解密/加密/扫描/密钥导出，可批处理集成 | 完整性预检 + 解密后 `.ldb` footer 魔数校验，交互式 CLI |

各项目实现要点与值得注意的差异：

- **NetEaseMC-Decryptor**（[index.html](https://github.com/ihaiming/NetEaseMC-Decryptor/blob/main/index.html)）：逻辑与 §5 完全一致；zip 模式先在压缩包里定位 `CURRENT`（其所在目录即 `db/`），再对该目录下每个带魔数的条目解密，最后重打包——因此能顺带处理 `db/` 之外万一被加密的文件（以魔数为准，最稳健）。文件夹模式基于 File System Access API，**会直接覆盖原文件**，务必先备份。
- **MCWorld-Converter**（[Program.cs](https://github.com/Carbonateds/MCWorld-Converter/blob/main/MCWorld-Converter/Program.cs)）：自己不实现算法，P/Invoke 官方 DLL 的 `check_file_is_encrypt` / `decrypt_file` / `encrypt_file`（均以**文件路径**为参数，DLL 内部读写临时文件）。加密方向会额外把 `netease_world_behavior_packs.json` / `netease_world_resource_packs.json` 覆写为 `null`。缺点：依赖 Windows；遍历仅两层（根目录 + 一级子目录）。
- **NeteaseMcDencrypter**（[Crypt.java](https://github.com/Jerbvsjhs/NeteaseMcDencrypter/blob/main/src/netease/mc/decrypter/Crypt.java) / [World.java](https://github.com/Jerbvsjhs/NeteaseMcDencrypter/blob/main/src/netease/mc/decrypter/World.java)）：密钥推导时先校验 CURRENT 带魔数；`optimizeKey()` 两半不一致时仅告警（与其他工具的“直接失败”不同）；支持 `-gkey` 把推导出的密钥写成 `key.bin`、再用它加密国际版 db，实现“同密钥批量导入”。注意其加密操作会用网易密钥**原地覆盖**国际版 db。
- **XOR-MC-Archive-Decrypt**（[main.js](https://github.com/HTMonkeyG/XOR-MC-Archive-Decrypt/blob/main/main.js) / [XOREncryptHelper.js](https://github.com/HTMonkeyG/XOR-MC-Archive-Decrypt/blob/main/includes/XOREncryptHelper.js)）：唯一提供“自定义密钥加密”的工具；`preprocessDir()` 用正则 `^MANIFEST-[0-9]{6,}$`、`^CURRENT$`、`^[0-9]{6,}\.ldb$` 限定处理对象——这基本就是网易实际会加密的文件集合，但意味着 `db/` 里**其他**带魔数的文件（若网易未来扩展加密范围）会被它遗漏；用 `XOREncryptHelper` 的四个工具型函数即可嵌入任何 Node 项目。衍生项目：Rust 版 [Lavaver/Crypt-Dew-World](https://github.com/Lavaver/Crypt-Dew-World)。
- **相关但未列入对比**：[AS5L/AS5L.github.io](https://github.com/AS5L/AS5L.github.io)（网页版算法源头）、[HTMonkeyG/leveldb-mcne](https://github.com/HTMonkeyG/leveldb-mcne)（改写 LevelDB Env 层支持该加密的 C++ 库，最适合需要“把网易存档当普通 LevelDB 用”的开发者）、[Redamancy520/MinecraftWorld-Decryption-Tools](https://github.com/Redamancy520/MinecraftWorld-Decryption-Tools)（网易 `MCStudio` 工具链调用官方 DLL 的参考代码）。

---

## 9. 反向加密（国际版 → 网易版）注意事项

把国际版存档导入网易版需要把 `db/` 中的文件按同一算法加密（XOR 是自逆的，加解密同一函数）：

1. **密钥**：用默认 `88329851`，或从另一个网易存档推导的密钥（`NeteaseMcDencrypter` 的 `-dec:网易db -enc:国际db` 流程）。加密前后 `CURRENT` 的明文/密文关系必须自洽——用推导密钥加密时，新加密的 CURRENT 解回去要等于它的 MANIFEST 文件名（本工具 `--encrypt` 用同一密钥处理全目录，天然自洽）。
2. **只加密 `db/` 内文件**（且按网易惯例 `*.log` 不动）。根目录的 `level.dat` 等不要加密。
3. **附加包声明**：参考 `MCWorld-Converter` 的做法，将 `netease_world_behavior_packs.json` 与 `netease_world_resource_packs.json` 写为 `null`，否则网易端导入可能因附加包校验失败而异常。
4. **`netease_config` 目录**：`XOR-MC-Archive-Decrypt` README 提示，解密后的资源中心存档若残留 `netease_config` 目录，重新导入网易版可能出错，需删除或保持加密状态。
5. 建议加密前先备份，加密后用网易客户端实际导入验证（XOR 层面正确 ≠ 版本兼容）。

---

## 10. 边界情况与 FAQ

**Q1：怎么快速判断一个文件/存档是否是这种加密？**
看前 4 字节是否为 `80 1D 30 01`。是 → 新版 XOR 加密；`90 1D 30 01` → 旧版 AES（无法离线解密）；`MANI`（即 `4D 41 4E 49`）→ 明文 CURRENT（未加密）；其他 → 明文。批量判断可直接对目录树做魔数扫描（`NeteaseMcDencrypter -sc:路径`）。

**Q2：解密后游戏仍读不出？**
依次排查：① 是否存在 `90 1D 30 01`（旧版/二次加密，无解）；② CURRENT 是否 80 字节左右（资源中心二次加密，无解）；③ 用 `.ldb` footer 魔数、MANIFEST CRC 校验确认解密正确性；④ 加密/解密时是否只动了 `db/`（动错文件会把明文当密文破坏）；⑤ 是否忘了删 `netease_config`。

**Q3：密钥推导报“前后两半不一致”？**
CURRENT 与 `MANIFEST-*` 序号不匹配（取**序号最大**的 MANIFEST，与 CURRENT 指向一致）、存档损坏，或加密方案不是本文所述格式。可改用默认密钥直接试解（见 §5.4 的成功判据）。

**Q4：空文件怎么处理？**
0 字节文件无魔数，属“未加密”，跳过即可。若要加密一个空文件，会得到“只有魔数”的 4 字节文件，还原后仍为空——`NeteaseMcDencrypter` 选择直接跳过空文件。

**Q5：这算加密还是混淆？**
从密码学角度只是“带魔数的循环异或”，且密钥可从密文自恢复，属于防误改/格式隔离级别，不提供真实机密性。

---

## 11. 附录：LevelDB 文件格式速查

理解 §5 的已知明文攻击只需要这几条 LevelDB 常识（以标准 LevelDB 为准，网易仅在 Env 层加了加解密包装）：

| 文件 | 内容 |
| --- | --- |
| `CURRENT` | 一行文本：当前 MANIFEST 文件名 + `\n`（如 `MANIFEST-000006\n`） |
| `MANIFEST-*` | 以 log 记录格式存储的 `VersionEdit` 序列（数据库名空间/文件表/日志号等元数据）；首条记录含比较器名 `leveldb.BytewiseComparator` |
| `00000N.log` | WAL 预写日志（`db/` 内唯一**不加密**的文件） |
| `00000N.ldb` | SSTable 数据文件；文件末尾固定 8 字节魔数 `0xDB4775248B80FB57`（小端存储，大端读为 `0x57FB808B247547DB`） |

log/MANIFEST 的记录帧格式（`MANIFEST` 与 `.log` 同构）：

```
每 32KB 为一个块（block）；块内记录头 7 字节：
  [0..3)  CRC   uint32 小端 —— CRC32C(类型字节 || 载荷) 经 mask 变换：
                             ((crc >> 15) | (crc << 17)) + 0xA282EAD8  (mod 2^32)
  [4..5)  长度  uint16 小端
  [6]     类型  0=ZERO 1=FULL 2=FIRST 3=MIDDLE 4=LAST
块尾不足 7 字节时补零。
```

本文验证脚本即按此格式实现了独立的 CRC32C 校验，作为解密正确性的判据（§6.2）。

---

## 12. 参考资料

**本文分析的四个项目**

- <https://github.com/Carbonateds/MCWorld-Converter> — C# 转换器，P/Invoke 网易官方 `XOREncryptDLL.dll`（仓库内附带该 DLL）
- <https://github.com/ihaiming/NetEaseMC-Decryptor> — 浏览器端一键解密（在线版 <https://mc.hm0.top/>），基于 AS5L 的核心算法
- <https://github.com/Jerbvsjhs/NeteaseMcDencrypter> — Java 跨平台工具（解密/加密/扫描/密钥导出）
- <https://github.com/HTMonkeyG/XOR-MC-Archive-Decrypt> — Node.js 工具（npm: `@htmonkeyg/cryptmc`），算法提取自 `XOREncrypt.dll` 的最早开源实现

**其他相关**

- <https://github.com/HTMonkeyG/leveldb-mcne> — 支持网易 XOR 加密的 LevelDB（C++，`McneWrapper` Env 包装；默认密钥 `"88329851"`，文档明确“除 .log 外所有文件均加密”）
- <https://github.com/HTMonkeyG/XOREncryptHelper> — 同作者的算法封装库
- <https://github.com/AS5L/AS5L.github.io> — 网页解密算法的源头项目
- <https://github.com/Redamancy520/MinecraftWorld-Decryption-Tools> — 网易 MCStudio 工具链调用官方 DLL 的参考
- <https://github.com/Lavaver/Crypt-Dew-World> — Rust 实现（原 XOR-MC-Archive-Decrypt-Rs）

**背景资料**

- 存档位置：4399 教程 <https://m.4399.cn/news-id-799596.html>、网易官网社区 <https://cg.163.com/static/content/696883b75c6e46c709c5e4a9>
- LevelDB 实现细节（CURRENT/MANIFEST/log 记录帧/CRC32C mask）：LevelDB 文档 `doc/log_format.md` 与源码（Google LevelDB）
