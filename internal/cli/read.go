package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/YuleBest/netease-mc-archive-tool/internal/archive"
	"github.com/YuleBest/netease-mc-archive-tool/internal/crypt"
	"github.com/YuleBest/netease-mc-archive-tool/internal/level"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version <存档zip或目录>",
		Short: "读取存档对应的 MC（基岩引擎）版本号",
		Long: `读取存档的 MC 真实版本号。

网易 App 版本号（如 3.9.15.297907）与 MC 基岩引擎版本号是两套编号。
本命令解析存档内 level.dat（网易加密只作用于 db/，level.dat 为明文），
优先输出 InventoryVersion 字段，其次由 lastOpenedWithVersion 数组
格式化（如 [1,21,120,0,0] → 1.21.120）。原理见 docs/mc-version.md。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := archive.OpenStore(args[0])
			if err != nil {
				return err
			}
			if closer, ok := store.(interface{ Close() error }); ok {
				defer closer.Close()
			}
			ldPath, raw, err := archive.FindLevelDat(store)
			if err != nil {
				return err
			}
			ld, err := level.ParseLevelDat(raw)
			if err != nil {
				return err
			}
			info := ld.Info()
			out := struct {
				Input                          string  `json:"input"`
				LevelDat                       string  `json:"level_dat"`
				EngineVersion                  string  `json:"engine_version"`
				InventoryVersion               string  `json:"inventory_version,omitempty"`
				LastOpenedWithVersion          []int32 `json:"last_opened_with_version,omitempty"`
				MinimumCompatibleClientVersion string  `json:"minimum_compatible_client_version,omitempty"`
			}{
				Input:                          args[0],
				LevelDat:                       ldPath,
				EngineVersion:                  info.EngineVersion,
				InventoryVersion:               info.InventoryVersion,
				LastOpenedWithVersion:          info.LastOpenedWithVersion,
				MinimumCompatibleClientVersion: info.MinimumCompatibleClientVersion,
			}
			if asJSON {
				return printJSON(cmd, out)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "MC 引擎版本:            %s\n", orUnknown(out.EngineVersion))
			fmt.Fprintf(w, "InventoryVersion:       %s\n", orUnknown(out.InventoryVersion))
			fmt.Fprintf(w, "lastOpenedWithVersion:  %s\n", formatIntSlice(out.LastOpenedWithVersion))
			if out.MinimumCompatibleClientVersion != "" {
				fmt.Fprintf(w, "最小兼容客户端版本:     %s\n", out.MinimumCompatibleClientVersion)
			}
			fmt.Fprintf(w, "level.dat:              %s\n", ldPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
	return cmd
}

func newInfoCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "info <存档zip或目录>",
		Short: "查看存档基本信息",
		Long: `查看存档的基本信息（世界名、MC 版本、最后游玩时间、游戏模式、
难度、种子、出生点等）与归档概况（文件数、db 加密状态、密钥可否推导）。

信息来源：level.dat（明文 NBT）、levelname.txt 与 db 目录扫描。
默认人类可读输出，--json 输出机器可读 JSON。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := archive.OpenStore(args[0])
			if err != nil {
				return err
			}
			if closer, ok := store.(interface{ Close() error }); ok {
				defer closer.Close()
			}
			st, err := archive.Inspect(store)
			if err != nil {
				return err
			}
			out := struct {
				Input        string      `json:"input"`
				World        string      `json:"world"`
				LevelInfo    *level.Info `json:"level_info,omitempty"`
				LevelDatErr  string      `json:"level_dat_error,omitempty"`
				DBEncrypted  bool        `json:"db_encrypted"`
				KeyDerivable bool        `json:"key_derivable"`
				DerivedKey   string      `json:"derived_key,omitempty"`
				archive.WorldStats
			}{
				Input:       args[0],
				World:       worldName(store, st, args[0]),
				DBEncrypted: len(st.EncryptedDB) > 0,
				WorldStats:  *st,
			}

			// level.dat 信息（可能不存在或异常）
			if st.LevelDatPath == "" {
				out.LevelDatErr = "存档中找不到 level.dat"
			} else if raw, err := store.ReadAll(st.LevelDatPath); err != nil {
				out.LevelDatErr = err.Error()
			} else if ld, err := level.ParseLevelDat(raw); err != nil {
				out.LevelDatErr = err.Error()
			} else {
				info := ld.Info()
				out.LevelInfo = &info
			}

			// db 已加密时尝试推导密钥（只读）
			if out.DBEncrypted && len(st.EncryptedOld) == 0 {
				if key, err := archive.DeriveKey(store); err == nil {
					out.KeyDerivable = true
					out.DerivedKey = crypt.FormatKeyASCII(key)
				}
			}
			if asJSON {
				return printJSON(cmd, out)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "世界:            %s\n", out.World)
			fmt.Fprintf(w, "输入:            %s (%s)\n", args[0], st.Kind)
			if st.LevelNameTxt != "" {
				fmt.Fprintf(w, "levelname.txt:   %s\n", st.LevelNameTxt)
			}
			if out.LevelInfo == nil {
				fmt.Fprintf(w, "level.dat:       解析失败（%s）\n", orUnknown(out.LevelDatErr))
			} else {
				info := *out.LevelInfo
				if info.LevelName != st.LevelNameTxt && info.LevelName != "" && st.LevelNameTxt != "" {
					// 两者都有且不一致时都展示
				}
				fmt.Fprintf(w, "世界名:          %s\n", orUnknown(info.LevelName))
				fmt.Fprintf(w, "MC 引擎版本:     %s\n", orUnknown(info.EngineVersion))
				fmt.Fprintf(w, "最后游玩:        %s\n", formatMillis(info.LastPlayed))
				fmt.Fprintf(w, "游戏模式:        %s（GameType=%d）\n", gameTypeName(info.GameType), info.GameType)
				fmt.Fprintf(w, "难度:            %s（Difficulty=%d）\n", difficultyName(info.Difficulty), info.Difficulty)
				fmt.Fprintf(w, "生成器:          %s（Generator=%d）\n", generatorName(info.Generator), info.Generator)
				fmt.Fprintf(w, "世界种子:        %d\n", info.RandomSeed)
				fmt.Fprintf(w, "出生点:          X=%d, Y=%d, Z=%d\n", info.SpawnX, info.SpawnY, info.SpawnZ)
				fmt.Fprintf(w, "游戏内时间:      %d tick\n", info.Time)
				fmt.Fprintf(w, "存档格式版本:    %d\n", info.StorageVersion)
			}
			fmt.Fprintf(w, "文件:            %d 个 / %s\n", st.FileCount, humanSize(st.TotalSize))
			if out.DBEncrypted {
				fmt.Fprintf(w, "db 加密:         已加密（%d/%d 个文件带魔数）\n", len(st.EncryptedDB), st.DBFiles)
				switch {
				case out.KeyDerivable:
					fmt.Fprintf(w, "密钥:            可自动推导（%s）\n", out.DerivedKey)
				case len(st.EncryptedOld) > 0:
					fmt.Fprintf(w, "密钥:            含旧版加密文件（%s），无法离线推导\n", strings.Join(st.EncryptedOld, ", "))
				default:
					fmt.Fprintf(w, "密钥:            推导失败（存档可能损坏或为资源中心二次加密）\n")
				}
			} else {
				fmt.Fprintf(w, "db 加密:         未加密（%d 个文件）\n", st.DBFiles)
			}
			if st.LevelDatPath != "" {
				fmt.Fprintf(w, "level.dat:       %s\n", st.LevelDatPath)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "以 JSON 格式输出")
	return cmd
}

// ---------------------------------------------------------------- 输出辅助

func printJSON(cmd *cobra.Command, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func worldName(store archive.Store, st *archive.WorldStats, input string) string {
	// WorldRoot 为 "." 表示世界就在存档根目录，回退到输入路径推断名称
	if st.WorldRoot != "" && st.WorldRoot != "." {
		return strings.Split(st.WorldRoot, "/")[0]
	}
	if store.Kind() == "dir" {
		return filepath.Base(filepath.Clean(input))
	}
	return strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
}

func orUnknown(s string) string {
	if s == "" {
		return "未知"
	}
	return s
}

func formatMillis(ms int64) string {
	if ms <= 0 {
		return "未知"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04:05 MST") + fmt.Sprintf("（%d）", ms)
}

func formatIntSlice(a []int32) string {
	if len(a) == 0 {
		return "（无）"
	}
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = strconv.Itoa(int(v))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func gameTypeName(v int32) string {
	switch v {
	case 0:
		return "生存"
	case 1:
		return "创造"
	case 2:
		return "冒险"
	case 3:
		return "旁观"
	}
	return "未知"
}

func difficultyName(v int32) string {
	switch v {
	case 0:
		return "和平"
	case 1:
		return "简单"
	case 2:
		return "普通"
	case 3:
		return "困难"
	}
	return "未知"
}

func generatorName(v int32) string {
	switch v {
	case 0:
		return "旧世界"
	case 1:
		return "无限"
	case 2:
		return "扁平"
	}
	return "未知"
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d 字节", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
