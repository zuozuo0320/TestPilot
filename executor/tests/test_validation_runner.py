import tempfile
import unittest
from pathlib import Path

from validation_runner import _prepare_requested_v1_spec, _sanitize_script_for_execution


class ValidationRunnerTestCase(unittest.TestCase):
    """校验 V1 编排验证执行前的 spec 文件同步行为。"""

    def test_prepare_requested_v1_spec_creates_missing_file(self) -> None:
        """后端指定的 spec 不存在时，应写入当前生成代码再执行。"""
        with tempfile.TemporaryDirectory() as temp_dir:
            workspace_root = Path(temp_dir)
            script_content = "import { test } from '@playwright/test'\n\ntest('demo', async () => {})\n"

            spec_path = _prepare_requested_v1_spec(
                workspace_root,
                "tests/composition/demo.spec.ts",
                script_content,
            )

            self.assertEqual(spec_path, workspace_root / "tests/composition/demo.spec.ts")
            self.assertEqual(spec_path.read_text(encoding="utf-8"), script_content)

    def test_prepare_requested_v1_spec_updates_stale_file(self) -> None:
        """目标 spec 内容落后于数据库生成代码时，应覆盖为当前代码。"""
        with tempfile.TemporaryDirectory() as temp_dir:
            workspace_root = Path(temp_dir)
            spec_path = workspace_root / "tests/composition/demo.spec.ts"
            spec_path.parent.mkdir(parents=True)
            spec_path.write_text("test('old', async () => {})\n", encoding="utf-8")

            script_content = "test('new', async () => {})\n"
            _prepare_requested_v1_spec(
                workspace_root,
                "tests/composition/demo.spec.ts",
                script_content,
            )

            self.assertEqual(spec_path.read_text(encoding="utf-8"), script_content)

    def test_prepare_requested_v1_spec_injects_validation_variables(self) -> None:
        """验证请求变量应写入生成脚本的 ScenarioContext。"""
        with tempfile.TemporaryDirectory() as temp_dir:
            workspace_root = Path(temp_dir)
            script_content = "\n".join(
                [
                    "test('demo', async () => {",
                    "  const ctx = {",
                    "    variables: {},",
                    "  }",
                    "})",
                ]
            )

            spec_path = _prepare_requested_v1_spec(
                workspace_root,
                "tests/composition/demo.spec.ts",
                script_content,
                {"taskName": "自动化验证任务"},
            )

            content = spec_path.read_text(encoding="utf-8")
            self.assertIn('"taskName": "自动化验证任务"', content)
            self.assertNotIn("variables: {},", content)

    def test_prepare_requested_v1_spec_rejects_path_escape(self) -> None:
        """拒绝写入工作区外路径，避免 spec_relative_path 路径逃逸。"""
        with tempfile.TemporaryDirectory() as temp_dir:
            workspace_root = Path(temp_dir)

            with self.assertRaises(ValueError):
                _prepare_requested_v1_spec(workspace_root, "../escape.spec.ts", "test('x', async () => {})")

    def test_sanitize_script_keeps_composition_flow_calls(self) -> None:
        """编排生成代码中的 flows 调用是内置步骤函数，不应被 PageObject 清洗逻辑移除。"""
        with tempfile.TemporaryDirectory() as temp_dir:
            workspace_root = Path(temp_dir)
            pages_dir = workspace_root / "pages"
            pages_dir.mkdir()
            (pages_dir / "AssetPage.ts").write_text(
                "export class AssetPage { async existingMethod(): Promise<void> {} }",
                encoding="utf-8",
            )
            script_content = "\n".join(
                [
                    "test('demo', async () => {",
                    "  const result = await flows.asset_scan_add_but(ctx, inputs)",
                    "  await assetPage.removedMethod()",
                    "})",
                ]
            )

            sanitized = _sanitize_script_for_execution(script_content, workspace_root)

            self.assertIn("await flows.asset_scan_add_but(ctx, inputs)", sanitized)
            self.assertNotIn("removedMethod", sanitized)


if __name__ == "__main__":
    unittest.main()
