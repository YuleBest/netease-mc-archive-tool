package main

import (
	"os"

	"github.com/YuleBest/netease-mc-archive-tool/internal/cli"
)

// version 由构建时 -ldflags "-X main.version=..." 注入。
var version = "dev"

func main() {
	cli.Version = version
	os.Exit(cli.Execute())
}
