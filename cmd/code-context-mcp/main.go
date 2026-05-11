// 文件说明：MCP Server 命令入口实现。
// 实现原理：加载 Agent-Memory 配置并启动基于 stdio 的 MCP JSON-RPC Server。
// 使用方式：在 MCP Client 中配置执行 code-context-mcp，由客户端通过 stdio 调用工具。
// 注意事项：stdout 只能写入 MCP 协议消息，启动和运行错误统一输出到 stderr。
// 交互模块：internal/config、internal/mcpserver。

// Package main 提供 code-context-mcp 命令入口。
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mazhan465/Agent-Memory/internal/config"
	"github.com/mazhan465/Agent-Memory/internal/mcpserver"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	server, err := mcpserver.NewServer(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
