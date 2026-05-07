// 文件说明：预留 MCP Server 模块入口。
// 实现原理：当前阶段仅定义后续 MCP 工具名称，避免 MVP 过早绑定具体 MCP SDK。
// 使用方式：后续在 cmd/code-context-mcp 中创建 Server 并注册 index_codebase、search_code、clear_index、get_indexing_status。
// 注意事项：MCP 协议实现将在 CLI MVP 稳定后接入，避免初期引入不稳定外部依赖。
// 交互模块：cmd/code-context-mcp、internal/indexer、internal/searcher、internal/snapshot、internal/vectorstore。

// Package mcpserver 预留 MCP Server 能力。
package mcpserver

// ToolNames 返回计划暴露给 MCP Client 的工具名称。
func ToolNames() []string {
	return []string{
		"index_codebase",
		"search_code",
		"clear_index",
		"get_indexing_status",
	}
}
