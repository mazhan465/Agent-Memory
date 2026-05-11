// 文件说明：提供配置文件相关 CLI 子命令。
// 实现原理：调用 internal/config 生成默认 YAML 配置文件，或输出当前默认配置路径。
// 使用方式：执行 code-context config init [--force] 或 code-context config path。
// 注意事项：生成的配置文件权限为 0600，避免 API Key 等敏感字段被其他用户读取。
// 交互模块：internal/config。

package main

import (
	"errors"
	"fmt"

	"github.com/mazhan465/Agent-Memory/internal/config"
)

func runConfig(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: code-context config <init|path> ...")
	}
	switch args[0] {
	case "init":
		return runConfigInit(args[1:])
	case "path":
		return runConfigPath(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func runConfigInit(args []string) error {
	force := false
	for _, arg := range args {
		switch arg {
		case "--force", "-f":
			force = true
		default:
			return errors.New("usage: code-context config init [--force]")
		}
	}
	path, err := config.WriteDefaultFile(force)
	if err != nil {
		return err
	}
	fmt.Printf("created config file: %s\n", path)
	return nil
}

func runConfigPath(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: code-context config path")
	}
	path, err := config.DefaultFilePath()
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}
