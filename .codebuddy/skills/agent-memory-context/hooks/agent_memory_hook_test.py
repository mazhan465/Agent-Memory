#!/usr/bin/env python3
"""Tests for the Agent-Memory hook project indexing helpers."""

from __future__ import annotations

import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

HOOK_PATH = Path(__file__).with_name("agent_memory_hook.py")
SPEC = importlib.util.spec_from_file_location("agent_memory_hook", HOOK_PATH)
hook = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(hook)


class ProjectIndexingTest(unittest.TestCase):
    def test_code_context_prefix_prefers_source_checkout(self) -> None:
        with tempfile.TemporaryDirectory() as root_dir:
            root = Path(root_dir)
            (root / "cmd" / "code-context").mkdir(parents=True)
            (root / "go.mod").write_text("module example.com/demo\n", encoding="utf-8")
            (root / "bin").mkdir()
            (root / "bin" / "code-context").write_text("stale binary\n", encoding="utf-8")

            got = hook.code_context_prefix(root)

            self.assertEqual(["go", "run", "./cmd/code-context"], got)

    def test_project_index_targets_include_prompt_directory(self) -> None:
        with tempfile.TemporaryDirectory() as root_dir, tempfile.TemporaryDirectory() as repo_dir:
            root = Path(root_dir)
            repo = Path(repo_dir)
            (repo / "go.mod").write_text("module example.com/demo\n", encoding="utf-8")

            targets = hook.project_index_targets(root, {}, f"请先索引 `{repo}` 这个项目")

            self.assertIn(root.resolve(), targets)
            self.assertIn(repo.resolve(), targets)

    def test_choose_search_types_prefers_split_code_and_doc(self) -> None:
        self.assertEqual("code", hook.choose_search_types("修复 Go 编译报错"))
        self.assertEqual("doc", hook.choose_search_types("查看 README 和架构说明"))
        self.assertEqual("doc", hook.choose_search_types("参考之前的经验"))
        self.assertEqual("code,doc", hook.choose_search_types("分析这个项目的实现和文档"))

    def test_search_type_plan_splits_code_and_doc(self) -> None:
        self.assertEqual(["code", "doc"], hook.search_type_plan("all"))
        self.assertEqual(["code", "doc"], hook.search_type_plan("code,doc"))
        self.assertEqual(["code"], hook.search_type_plan("code"))
        self.assertEqual(["doc"], hook.search_type_plan("doc"))
        self.assertEqual(["code", "experience"], hook.search_type_plan("code,experience"))

    def test_project_markdown_import_classifies_and_skips_unchanged_docs(self) -> None:
        with tempfile.TemporaryDirectory() as state_dir, tempfile.TemporaryDirectory() as root_dir:
            root = Path(root_dir)
            (root / "README.md").write_text("# Demo\n\n安装和使用方案。\n", encoding="utf-8")
            (root / "notes.md").write_text("# 会议纪要\n\nuser: hello\nassistant: hi\n", encoding="utf-8")
            hidden_dir = root / ".codebuddy"
            hidden_dir.mkdir()
            (hidden_dir / "README.md").write_text("# ignored\n", encoding="utf-8")

            completed = subprocess.CompletedProcess(args=[], returncode=0, stdout="imported\n", stderr="")
            with mock.patch.dict(os.environ, {"AGENT_MEMORY_HOOK_STATE_DIR": state_dir}, clear=False):
                with mock.patch.object(hook, "run_code_context", return_value=completed) as run_code_context:
                    summary = hook.import_project_markdown_memory(root, root)
                    self.assertIn("conversation=1", summary)
                    self.assertIn("experience=1", summary)
                    calls = [call.args[1] for call in run_code_context.call_args_list]
                    self.assertIn("conversation", [args[2] for args in calls])
                    self.assertIn("experience", [args[2] for args in calls])

                with mock.patch.object(hook, "run_code_context", return_value=completed) as run_code_context:
                    summary = hook.import_project_markdown_memory(root, root)
                    self.assertEqual("", summary)
                    run_code_context.assert_not_called()


if __name__ == "__main__":
    unittest.main()
