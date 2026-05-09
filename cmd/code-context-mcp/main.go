// 文件说明：MCP Server 命令入口占位实现。
// 实现原理：当前阶段输出计划支持的 MCP 工具，后续接入 MCP SDK 后在此启动 stdio server。
// 使用方式：执行 go run ./cmd/code-context-mcp 查看后续 MCP 工具规划。
// 注意事项：当前不提供真实 MCP 协议服务，避免在 CLI MVP 之前引入额外复杂度。
// 交互模块：internal/mcpserver。

// Package main 提供 code-context-mcp 命令入口。
package main

import (
	"fmt"
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/mcpserver"
)

func main() {
	fmt.Printf("code-context-mcp is planned. tools: %s\n", strings.Join(mcpserver.ToolNames(), ", "))
}
