package cli

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/YuleBest/netease-mc-archive-tool/internal/archive"
	"github.com/YuleBest/netease-mc-archive-tool/internal/crypt"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	var (
		output    string
		keyFlag   string
		overwrite bool
	)
	cmd := &cobra.Command{
		Use:     "export <存档zip或目录>",
		Aliases: []string{"mcworld"},
		Short:   "解密并导出为 .mcworld（国际版一键导入）",
		Long: `把网易加密存档解密并重打包为国际版可一键导入的 .mcworld 文件。

.mcworld 是把世界目录内容（level.dat 位于压缩包根）打包成的 zip：
国际版我的世界打开该文件后会自动完成导入，无需手动复制存档目录。
注意 Mojira MCPE-19966：世界内容嵌套在子目录内的 zip 无法被新版
游戏导入，本命令会自动把世界内容提升到压缩包根。

输入支持网易加密存档（zip/目录，自动解密）或已解密存档。
输出默认为 <输入名>.mcworld，不修改输入。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := args[0]
			store, err := archive.OpenStore(input)
			if err != nil {
				return err
			}
			if closer, ok := store.(interface{ Close() error }); ok {
				defer closer.Close()
			}

			// 定位 db 与世界根，得到"内容在根"的虚拟 Store
			entries, err := store.Entries()
			if err != nil {
				return err
			}
			dbPrefix, err := archive.LocateDB(entries)
			if err != nil {
				return err
			}
			worldRoot := archive.WorldRoot(dbPrefix)
			inner, err := archive.StripWorldRoot(store, worldRoot)
			if err != nil {
				return err
			}

			outPath, err := resolveExportOutput(input, output)
			if err != nil {
				return err
			}
			inAbs, err := filepath.Abs(input)
			if err != nil {
				return err
			}
			outAbs, err := filepath.Abs(outPath)
			if err != nil {
				return err
			}
			if filepath.Clean(inAbs) == filepath.Clean(outAbs) {
				return &UsageError{fmt.Errorf("输出路径不能与输入相同: %s", outPath)}
			}
			if _, err := os.Stat(outPath); err == nil {
				if !overwrite {
					return fmt.Errorf("输出已存在: %s（--overwrite 可覆盖）", outPath)
				}
				if err := os.Remove(outPath); err != nil {
					return err
				}
			}

			var keyBytes []byte
			if keyFlag != "" {
				keyBytes, err = crypt.ParseKey(keyFlag)
				if err != nil {
					return &UsageError{err}
				}
			}

			sink, err := archive.NewSink(outPath, true)
			if err != nil {
				return err
			}
			res, err := archive.Transform(inner, sink, archive.Options{Mode: archive.ModeDecrypt, Key: keyBytes})
			if err != nil {
				os.Remove(outPath)
				return err
			}

			manifestName, err := crypt.PickManifest(archive.ManifestNames(entries, dbPrefix))
			if err == nil {
				if err := verifyMcworld(outPath, manifestName); err != nil {
					return err
				}
			}

			keySource := res.KeySource
			if keyFlag != "" {
				keySource = "指定"
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "[i] 输入:      %s (%s)\n", input, store.Kind())
			fmt.Fprintf(w, "[i] 世界根:    %s（内容已提升到压缩包根）\n", displayPrefix(worldRoot))
			fmt.Fprintf(w, "[i] 密钥:      %s（hex %s，来源: %s）\n", formatKeyASCII(res.Key), formatKeyHex(res.Key), keySource)
			fmt.Fprintf(w, "[i] 已解密: %d 个文件\n", len(res.Transformed))
			if res.Verified != "" {
				fmt.Fprintf(w, "[✓] 校验:     %s\n", res.Verified)
			}
			fmt.Fprintf(w, "[✓] 已导出:   %s（%d 个条目）\n", outPath, res.Copied)
			fmt.Fprintf(w, "[i] 提示:     将该文件发送到手机后，用国际版我的世界打开即可自动导入\n")
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出路径（默认 <输入名>.mcworld）")
	cmd.Flags().StringVarP(&keyFlag, "key", "k", "", "解密密钥（默认自动推导；默认按 ASCII，hex:/0x 前缀表示十六进制）")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "输出已存在时覆盖")
	return cmd
}

// resolveExportOutput 计算导出文件默认路径并规范化 -o 参数（强制 .mcworld 后缀）。
func resolveExportOutput(input, flagOut string) (string, error) {
	st, err := os.Stat(input)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		cleaned := filepath.Clean(input)
		if flagOut == "" {
			flagOut = filepath.Join(filepath.Dir(cleaned), filepath.Base(cleaned)+".mcworld")
		}
	} else {
		dir, file := filepath.Split(input)
		stem := strings.TrimSuffix(file, filepath.Ext(file))
		if flagOut == "" {
			flagOut = filepath.Join(dir, stem+".mcworld")
		}
	}
	if !strings.HasSuffix(strings.ToLower(flagOut), ".mcworld") {
		flagOut += ".mcworld"
	}
	return flagOut, nil
}

// verifyMcworld 校验导出产物：level.dat 位于压缩包根、db/CURRENT 为明文已知格式。
func verifyMcworld(path, manifestName string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		return fmt.Errorf("导出产物不是有效 zip: %w", err)
	}
	hasLevelDat := false
	var current []byte
	for _, e := range zr.File {
		if e.Name == "" {
			return fmt.Errorf("导出产物含空名条目，将导致游戏导入失败")
		}
		switch {
		case e.Name == "level.dat":
			hasLevelDat = true
		case e.Name == "db/CURRENT":
			r, err := e.Open()
			if err != nil {
				return err
			}
			current, err = io.ReadAll(r)
			r.Close()
			if err != nil {
				return err
			}
		}
	}
	if !hasLevelDat {
		return fmt.Errorf("导出产物缺少根级 level.dat，国际版将无法导入")
	}
	if current == nil {
		return fmt.Errorf("导出产物缺少 db/CURRENT")
	}
	if expect := manifestName + "\n"; string(current) != expect {
		return fmt.Errorf("导出产物 db/CURRENT 校验失败: %q != %q", current, expect)
	}
	return nil
}
