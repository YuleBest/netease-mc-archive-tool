package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YuleBest/netease-mc-archive-tool/internal/archive"
	"github.com/YuleBest/netease-mc-archive-tool/internal/crypt"
	"github.com/spf13/cobra"
)

const (
	suffixDecrypted = "_decrypted"
	suffixEncrypted = "_encrypted"
)

func newDecryptCmd() *cobra.Command {
	var (
		output    string
		key       string
		overwrite bool
	)
	cmd := &cobra.Command{
		Use:   "decrypt <存档zip或目录>",
		Short: "解密网易 XOR 加密存档",
		Long: `解密网易我的世界（中国版）基岩存档。

输入可为存档 zip 压缩包或世界目录；自动定位 db（LevelDB）目录并从
CURRENT + MANIFEST 推导密钥（亦可用 --key 指定）。db 内所有带魔数
80 1D 30 01 的文件都会被解密，其余文件原样保留。默认输出到
<输入名>_decrypted（zip 输入 → zip 输出，目录输入 → 目录输出），
不会修改输入。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTransform(cmd, args[0], output, key, overwrite, archive.ModeDecrypt, suffixDecrypted)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出路径（默认 <输入名>_decrypted）")
	cmd.Flags().StringVarP(&key, "key", "k", "", "密钥（默认按 ASCII；hex: 或 0x 前缀表示十六进制；默认自动推导）")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "输出已存在时覆盖")
	return cmd
}

func newEncryptCmd() *cobra.Command {
	var (
		output    string
		key       string
		overwrite bool
	)
	cmd := &cobra.Command{
		Use:   "encrypt <存档zip或目录>",
		Short: "加密存档（国际版 → 网易版）",
		Long: `加密基岩存档以便导入网易我的世界（中国版）。

与游戏行为一致：仅加密 db 内的 CURRENT、MANIFEST-* 与 *.ldb，
其余文件（含 .log）保持原样。默认密钥为网易官方内置的 88329851
（可用 --key 覆盖）。默认输出到 <输入名>_encrypted，不会修改输入。
导入网易版前可能还需将 netease_world_*.json 置为 null，详见 docs/encryption.md。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTransform(cmd, args[0], output, key, overwrite, archive.ModeEncrypt, suffixEncrypted)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出路径（默认 <输入名>_encrypted）")
	cmd.Flags().StringVarP(&key, "key", "k", "", "密钥（默认按 ASCII；hex: 或 0x 前缀表示十六进制；默认 88329851）")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "输出已存在时覆盖")
	return cmd
}

func runTransform(cmd *cobra.Command, input, output, keyFlag string, overwrite bool, mode archive.Mode, suffix string) error {
	store, err := archive.OpenStore(input)
	if err != nil {
		return err
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	outPath, asZip, err := archive.ResolveOutput(input, output, suffix)
	if err != nil {
		return err
	}
	// 输出不能指向输入自身：zip 会在打开状态下被截断，目录形态则可能连带删除源存档
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
		if err := os.RemoveAll(outPath); err != nil {
			return err
		}
	}

	var key []byte
	switch {
	case mode == archive.ModeEncrypt && keyFlag == "":
		key = crypt.DefaultKey
	case keyFlag != "":
		var err error
		key, err = crypt.ParseKey(keyFlag)
		if err != nil {
			return &UsageError{err}
		}
	}

	sink, err := archive.NewSink(outPath, asZip)
	if err != nil {
		return err
	}
	res, err := archive.Transform(store, sink, archive.Options{Mode: mode, Key: key})
	if err != nil {
		// 失败时清理半成品输出
		os.Remove(outPath)
		return err
	}

	action := "已加密"
	if mode == archive.ModeDecrypt {
		action = "已解密"
	}
	keySource := res.KeySource
	if mode == archive.ModeEncrypt && keyFlag == "" {
		keySource = "官方默认"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "[i] 输入:      %s (%s)\n", input, store.Kind())
	fmt.Fprintf(cmd.OutOrStdout(), "[i] db 目录:   %s\n", archive.DisplayPrefix(res.DBPrefix))
	fmt.Fprintf(cmd.OutOrStdout(), "[i] 密钥:      %s（hex %s，来源: %s）\n", crypt.FormatKeyASCII(res.Key), crypt.FormatKeyHex(res.Key), keySource)
	fmt.Fprintf(cmd.OutOrStdout(), "[i] %s: %d 个文件: %s\n", action, len(res.Transformed), strings.Join(res.Transformed, ", "))
	if len(res.Skipped) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "[i] 已是加密文件，跳过: %s\n", strings.Join(res.Skipped, ", "))
	}
	if res.Verified != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "[✓] 校验:     %s\n", res.Verified)
	}
	for _, n := range res.Notes {
		fmt.Fprintf(cmd.OutOrStdout(), "[!] %s\n", n)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "[✓] 已写出:   %s（%d 个条目）\n", outPath, res.Copied)
	return nil
}
