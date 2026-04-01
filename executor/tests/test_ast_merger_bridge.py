import unittest
from types import SimpleNamespace
from unittest.mock import patch

from ast_merger_bridge import _call_ast_merger


class AstMergerBridgeTestCase(unittest.TestCase):
    """校验 AST 合并桥接层的通用行为。"""

    @patch("ast_merger_bridge.os.path.exists", return_value=True)
    @patch("ast_merger_bridge.subprocess.run")
    def test_call_ast_merger_uses_utf8_for_non_ascii_payload(self, mock_run, _mock_exists) -> None:
        """调用 ts-morph 合并器时应固定使用 UTF-8，避免中文定位器在 Windows 上乱码。"""
        mock_run.return_value = SimpleNamespace(
            returncode=0,
            stdout='{"success": true, "merged_files": [], "errors": [], "warnings": []}',
            stderr="",
        )

        result = _call_ast_merger(
            {
                "workspace_root": "D:/workspace",
                "updates": [
                    {
                        "page_name": "AssetDiscoveryTaskPage",
                        "file_path": "pages/AssetDiscoveryTaskPage.ts",
                        "operation": "append_action",
                        "new_locators": [
                            {
                                "name": "thirdViewTaskButton",
                                "definition": "page.getByText('查看任务').nth(2)",
                            }
                        ],
                        "new_actions": [],
                        "extend_actions": [],
                    }
                ],
            }
        )

        self.assertTrue(result["success"])
        self.assertTrue(mock_run.called)
        self.assertEqual(mock_run.call_args.kwargs["encoding"], "utf-8")
        self.assertTrue(mock_run.call_args.kwargs["text"])


if __name__ == "__main__":
    unittest.main()
