package level

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/YuleBest/netease-mc-archive-tool/internal/crypt"
)

// LevelDat 为解析后的 level.dat。
type LevelDat struct {
	// StorageVersion 为文件头前 4 字节的存储格式版本（gzip 兜底路径下为 0）。
	StorageVersion uint32
	// RootName 为 NBT 根 Compound 名称（Bedrock 通常为空串）。
	RootName string
	// Root 为根 Compound 的原始字段。
	Root map[string]any
}

// ParseLevelDat 解析 level.dat 原始字节。
//
// 标准格式为：4 字节存储版本（小端）+ 4 字节 NBT 长度（小端）+ NBT；
// 同时兼容 gzip 压缩与无长度头的变体。若数据带加密魔数则返回相应错误。
func ParseLevelDat(data []byte) (*LevelDat, error) {
	if crypt.HasOldMagic(data) {
		return nil, crypt.ErrOldEncryption
	}
	if crypt.IsEncrypted(data) {
		return nil, fmt.Errorf("level.dat 带加密魔数：请先用 nmcat decrypt 解密存档: %w", crypt.ErrNotEncrypted)
	}
	if len(data) >= 2 && data[0] == 0x1F && data[1] == 0x8B {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("gzip 解压失败: %w", err)
		}
		raw, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("gzip 读取失败: %w", err)
		}
		name, root, err := Parse(raw)
		if err != nil {
			return nil, err
		}
		return &LevelDat{RootName: name, Root: root}, nil
	}
	// 优先尝试标准 8 字节头（长度字段校验通过才采用），否则回退 4/0 字节偏移
	for _, off := range []int{8, 4, 0} {
		if len(data) < off+1 {
			continue
		}
		if off == 8 {
			n := int32(binary.LittleEndian.Uint32(data[4:8]))
			if int(n) != len(data)-8 {
				continue
			}
		}
		if data[off] != byte(TagCompound) {
			continue
		}
		name, root, err := Parse(data[off:])
		if err != nil {
			continue
		}
		ld := &LevelDat{RootName: name, Root: root}
		if off == 8 {
			ld.StorageVersion = binary.LittleEndian.Uint32(data[:4])
		}
		return ld, nil
	}
	return nil, fmt.Errorf("无法解析 level.dat：既非标准 8 字节头 NBT，也非 gzip 压缩")
}

// Info 为世界基本信息（来自 level.dat）。
type Info struct {
	LevelName                      string  `json:"level_name"`
	EngineVersion                  string  `json:"engine_version"`
	InventoryVersion               string  `json:"inventory_version,omitempty"`
	LastOpenedWithVersion          []int32 `json:"last_opened_with_version,omitempty"`
	MinimumCompatibleClientVersion string  `json:"minimum_compatible_client_version,omitempty"`
	// LastPlayed 为最后游玩时间的 Unix 毫秒时间戳。
	// 部分网易版本以秒存储该字段，解析时按 <1e12 启发式归一化为毫秒。
	LastPlayed      int64  `json:"last_played_ms"` // Unix 毫秒
	GameType        int32  `json:"game_type"`      // 0 生存 1 创造 2 冒险
	Difficulty      int32  `json:"difficulty"`     // 0 和平 1 简单 2 普通 3 困难
	Generator       int32  `json:"generator"`      // 0 旧世界 1 无限 2 扁平
	RandomSeed      int64  `json:"random_seed"`
	SpawnX          int32  `json:"spawn_x"`
	SpawnY          int32  `json:"spawn_y"`
	SpawnZ          int32  `json:"spawn_z"`
	Time            int64  `json:"time"` // 世界游戏内时间（tick）
	StorageVersion  uint32 `json:"storage_version"`
	CommandsEnabled bool   `json:"commands_enabled"`
}

// Info 从已解析的 level.dat 提取世界基本信息。
func (l *LevelDat) Info() Info {
	lastPlayed := l.getInt64("LastPlayed")
	if lastPlayed > 0 && lastPlayed < 1e12 {
		// 该值为 Unix 秒（部分网易版本如此），归一化为毫秒
		lastPlayed *= 1000
	}
	return Info{
		LevelName:                      l.getString("LevelName"),
		EngineVersion:                  l.EngineVersion(),
		InventoryVersion:               l.getString("InventoryVersion"),
		LastOpenedWithVersion:          l.IntList("lastOpenedWithVersion"),
		MinimumCompatibleClientVersion: FormatVersionArray(l.IntList("minimumCompatibleClientVersion")),
		LastPlayed:                     lastPlayed,
		GameType:                       l.getInt32("GameType"),
		Difficulty:                     l.getInt32("Difficulty"),
		Generator:                      l.getInt32("Generator"),
		RandomSeed:                     l.getInt64("RandomSeed"),
		SpawnX:                         l.getInt32("SpawnX"),
		SpawnY:                         l.getInt32("SpawnY"),
		SpawnZ:                         l.getInt32("SpawnZ"),
		Time:                           l.getInt64("Time"),
		StorageVersion:                 l.StorageVersion,
		CommandsEnabled:                l.getByte("commandsEnabled") != 0,
	}
}

// EngineVersion 返回 MC（基岩引擎）版本字符串：优先 InventoryVersion，
// 其次由 lastOpenedWithVersion 数组格式化。两者皆缺时返回空串。
func (l *LevelDat) EngineVersion() string {
	if v := l.getString("InventoryVersion"); v != "" {
		return v
	}
	return FormatVersionArray(l.IntList("lastOpenedWithVersion"))
}

// IntList 以 []int32 形式返回整型版本数组字段，兼容 TAG_List<Int> 与 TAG_Int_Array 两种编码。
func (l *LevelDat) IntList(name string) []int32 {
	v, ok := l.Root[name]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []any:
		out := make([]int32, 0, len(t))
		for _, e := range t {
			switch n := e.(type) {
			case int32:
				out = append(out, n)
			case int64:
				out = append(out, int32(n))
			}
		}
		return out
	case []int32:
		return t
	}
	return nil
}

// FormatVersionArray 把基岩版本号数组格式化为点分字符串。
// 规则：去掉末尾连续的 0（最少保留 3 段），如 [1,21,120,0,0] → "1.21.120"，
// [1,21,120,5] → "1.21.120.5"。
func FormatVersionArray(a []int32) string {
	if len(a) == 0 {
		return ""
	}
	end := len(a)
	for end > 3 && a[end-1] == 0 {
		end--
	}
	parts := make([]string, end)
	for i := 0; i < end; i++ {
		parts[i] = strconv.FormatInt(int64(a[i]), 10)
	}
	return strings.Join(parts, ".")
}

func (l *LevelDat) getString(name string) string {
	if v, ok := l.Root[name].(string); ok {
		return v
	}
	return ""
}

func (l *LevelDat) getInt32(name string) int32 {
	return int32(l.getInt64(name))
}

func (l *LevelDat) getInt64(name string) int64 {
	switch v := l.Root[name].(type) {
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

func (l *LevelDat) getByte(name string) int8 {
	if v, ok := l.Root[name].(int8); ok {
		return v
	}
	return 0
}
