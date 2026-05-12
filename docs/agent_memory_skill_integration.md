# Agent-Memory Skill 与 Agent 闭环集成

## 目标

为 `Agent-Memory` 项目提供一个可复用 skill，并让 CodeBuddy 与 Codex 在运行时尽量自动遵守以下闭环：

1. 用户提示进入模型前，先根据提示检索 Agent-Memory 上下文。
2. 首次检索保存返回的 `session_id`。
3. 同一会话后续检索持续传入该 `session_id`，让 Agent-Memory 去重已返回内容。
4. Assistant 回合或会话结束时执行增量索引，让变更进入下一轮检索。

## 单目录封装

所有 skill 相关源文件统一封装在：

```text
.codebuddy/skills/agent-memory-context/
```

目录内容：

```text
.codebuddy/skills/agent-memory-context/SKILL.md
.codebuddy/skills/agent-memory-context/README.md
.codebuddy/skills/agent-memory-context/install.py
.codebuddy/skills/agent-memory-context/hooks/agent_memory_hook.py
.codebuddy/skills/agent-memory-context/templates/codebuddy/settings.json
.codebuddy/skills/agent-memory-context/templates/codebuddy/rules/agent-memory-context.md
.codebuddy/skills/agent-memory-context/templates/codex/config.toml
.codebuddy/skills/agent-memory-context/templates/codex/AGENTS.md
.codebuddy/skills/agent-memory-context/templates/env/agent-memory.env.example
```

安装脚本会把模板安装到目标项目：

```text
<project>/.codebuddy/settings.json
<project>/.codebuddy/rules/agent-memory-context.md
<project>/.codex/config.toml
<project>/AGENTS.md
```

这些目标文件是安装产物，源模板仍以 skill 单目录为准。

## 安装脚本

快速安装：

```bash
python3 .codebuddy/skills/agent-memory-context/install.py --project-root .
```

非交互示例：

```bash
python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root . \
  --yes \
  --embedding ollama \
  --install-ollama
```

主要能力：

1. 将 skill 目录安装/复制到目标项目的 `.codebuddy/skills/agent-memory-context`。
2. 合并 CodeBuddy hooks 到 `.codebuddy/settings.json`。
3. 安装 CodeBuddy 项目规则到 `.codebuddy/rules/agent-memory-context.md`。
4. 安装 Codex hooks 到 `.codex/config.toml`。
5. 追加 Codex `AGENTS.md` 强约束块。
6. 从 GitHub Release 下载 `code-context` 和 `code-context-mcp` 到 `<install-root>/bin`。
7. 将 `<install-root>/bin` 加入用户 `PATH`。
8. 下载 Milvus 官方 standalone Docker Compose 文件并启动 Milvus。
9. 引导选择 `hash`、`ollama` 或 `openai-compatible` embedding，并写入 `<install-root>/agent-memory.env`。

## CodeBuddy 集成

CodeBuddy 支持项目级 skills 与 hooks。

- Skill 源目录：`.codebuddy/skills/agent-memory-context/`
- Hooks 模板：`.codebuddy/skills/agent-memory-context/templates/codebuddy/settings.json`
- 规则模板：`.codebuddy/skills/agent-memory-context/templates/codebuddy/rules/agent-memory-context.md`
- 共享脚本：`.codebuddy/skills/agent-memory-context/hooks/agent_memory_hook.py`

Hook 事件：

| 事件 | 作用 |
| --- | --- |
| `SessionStart` | 会话启动或恢复时，检查索引；已索引则增量同步 |
| `UserPromptSubmit` | 读取用户 prompt，执行 `code-context search`，注入检索结果 |
| `Stop` | 主 Agent 回合结束时执行 `code-context sync` |
| `SessionEnd` | 会话结束时再次执行 `code-context sync` |

CodeBuddy 修改项目级 hook 配置后，通常需要在 `/hooks` 面板审查并启用。

## Codex 集成

Codex 使用项目级 `.codex/config.toml` 启用 hooks，并使用 `AGENTS.md` 作为项目级长期指令。

- Codex 只会在项目被信任时加载项目级 `.codex/config.toml`。
- Hooks 通过 `[features].codex_hooks = true` 启用。
- `AGENTS.md` 作为 hooks 不可用时的强约束兜底。

Hook 事件：

| 事件 | 作用 |
| --- | --- |
| `SessionStart` | 会话启动或恢复时准备索引 |
| `UserPromptSubmit` | 根据用户 prompt 检索并注入 Agent-Memory 上下文 |
| `Stop` | Agent 回合结束时增量同步索引 |

## 组件安装与配置

### 二进制文件

安装脚本默认从 GitHub Release 下载以下二进制到 `<install-root>/bin`：

- `code-context`
- `code-context-mcp`

安装脚本会尝试将 `<install-root>/bin` 写入用户 `PATH`。如果自动写入失败，用户需要手动配置 `PATH` 或设置：

```bash
AGENT_MEMORY_CODE_CONTEXT_BIN=/path/to/code-context
```

### Milvus

安装脚本默认下载 Milvus 官方 standalone Docker Compose 文件，并执行：

```bash
docker compose -f <install-root>/milvus/docker-compose.yml up -d
```

macOS 和 Windows 需要先安装 Docker Desktop；Linux 需要 Docker Engine 与 Docker Compose V2。

### Embedding 模型

安装脚本会让用户选择：

- `hash`：零依赖，适合快速试用，但语义效果弱。
- `ollama`：本地小模型方案，可使用 `--install-ollama` 尝试安装 Ollama 并拉取模型。
- `openai-compatible`：远端 embedding API，需要用户自行配置 API Key。

安装脚本会写出 `<install-root>/agent-memory.env` 作为配置引导。

## Hook 脚本行为

`.codebuddy/skills/agent-memory-context/hooks/agent_memory_hook.py` 支持三个模式：

```bash
python3 .codebuddy/skills/agent-memory-context/hooks/agent_memory_hook.py session-start
python3 .codebuddy/skills/agent-memory-context/hooks/agent_memory_hook.py prompt
python3 .codebuddy/skills/agent-memory-context/hooks/agent_memory_hook.py sync
```

脚本会从 hook 的 stdin JSON 中读取：

- `cwd`：当前工作目录
- `session_id` / `thread-id`：外部工具会话 ID
- `prompt`：用户输入
- `hook_event_name`：当前 hook 事件

脚本会把外部工具会话 ID 映射到 Agent-Memory 的 `session_id`，状态默认保存到：

```text
~/.agent-memory/hook-state/sessions/
```

Windows 默认保存到：

```text
%LOCALAPPDATA%\AgentMemory\hook-state\sessions\
```

## CLI 等价操作

如果 hooks 不可用，Agent 必须手动执行等价命令：

```bash
code-context search <repo> "<query>" 8 all
code-context search <repo> "<query>" 8 code --session-id=<session_id>
code-context sync <repo>
```

## 验证方式

基础静态验证：

```bash
python3 -m py_compile .codebuddy/skills/agent-memory-context/install.py
python3 -m py_compile .codebuddy/skills/agent-memory-context/hooks/agent_memory_hook.py
python3 -m json.tool .codebuddy/skills/agent-memory-context/templates/codebuddy/settings.json >/dev/null
python3 - <<'PY'
import tomllib
from pathlib import Path
tomllib.loads(Path('.codebuddy/skills/agent-memory-context/templates/codex/config.toml').read_text())
PY
```

轻量运行验证：

```bash
printf '{}' | python3 .codebuddy/skills/agent-memory-context/hooks/agent_memory_hook.py prompt >/dev/null
```

## 注意事项

- Hook 会自动执行本地命令，启用前必须审查脚本与配置。
- 首次索引可能耗时较长，后续会走增量同步。
- 当前 MCP `search_code` 主要面向代码搜索；需要 `session_id` 去重或知识/记忆检索时，应使用 CLI `search`。
- 不要把 hook 运行时状态提交到仓库；默认状态目录位于用户主目录或 `%LOCALAPPDATA%`。
