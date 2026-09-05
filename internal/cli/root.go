// Package cli 实现 nmcat 的命令行界面（基于 cobra）。
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version 由构建时 -ldflags 注入，`nmcat --version` 显示。
var Version = "dev"

// UsageError 标记用法类错误（退出码 2）。
type UsageError struct{ err error }

func (e *UsageError) Error() string { return e.err.Error() }
func (e *UsageError) Unwrap() error { return e.err }

// NewRootCmd 组装根命令与全部子命令。
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "nmcat <command> [flags]",
		Short: "网易我的世界（中国版）基岩存档命令行工具",
		Long: `nmcat —— 网易我的世界（中国版）基岩存档命令行工具

支持对存档（zip 压缩包或目录）进行：
  decrypt   解密网易 XOR 加密存档
  encrypt   加密存档（国际版 → 网易版）
  export    解密并导出为 .mcworld（国际版一键导入）
  version   读取存档对应的 MC（基岩引擎）版本号
  info      查看存档基本信息
  tui       交互式界面（浏览存档、解密/加密/导出）

加密算法与密钥推导原理详见项目内 docs/encryption.md。
仅供学习研究与个人存档数据迁移使用。`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}
	root.SetVersionTemplate("nmcat version {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &UsageError{err}
	})
	root.AddCommand(newDecryptCmd(), newEncryptCmd(), newExportCmd(), newVersionCmd(), newInfoCmd(), newTUICmd())
	return root
}

// Execute 运行根命令并返回进程退出码：0 成功，1 运行时错误，2 用法错误。
func Execute() int {
	cmd := NewRootCmd()
	if err := cmd.Execute(); err != nil {
		var ue *UsageError
		if errors.As(err, &ue) {
			fmt.Fprintf(os.Stderr, "用法错误: %v\n", err)
			fmt.Fprintln(os.Stderr, "使用 nmcat --help 或 nmcat <command> --help 查看用法")
			return 2
		}
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	return 0
}
