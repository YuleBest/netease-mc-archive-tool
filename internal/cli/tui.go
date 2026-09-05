package cli

import (
	"github.com/YuleBest/netease-mc-archive-tool/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui [存档路径]",
		Short: "交互式界面：浏览存档信息并执行解密/加密/导出",
		Long: `以交互式终端界面操作 nmcat。

方向键选择操作，回车确认，Esc 返回。可在进入界面后输入存档路径，
也可以直接传入存档路径（zip 或目录）跳过输入步骤。

需要在交互式终端（TTY）中运行；非交互场景请使用
decrypt / encrypt / export / version / info 子命令。`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var path string
			if len(args) > 0 {
				path = args[0]
			}
			return tui.Run(path, Version)
		},
	}
}
