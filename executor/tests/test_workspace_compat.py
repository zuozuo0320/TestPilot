import json
import tempfile
import unittest
from pathlib import Path

from fixture_builder import build_base_fixture
from page_registry import PageRegistry
from project_workspace import sync_workspace_support_files


class WorkspaceCompatTestCase(unittest.TestCase):
    """校验平台升级后老项目工作区的兼容补齐逻辑。"""

    def test_sync_workspace_support_files_backfills_builtin_shared_templates(self) -> None:
        """历史项目缺少 LoginPage.ts 时，应自动补齐内置 shared 模板文件。"""
        with tempfile.TemporaryDirectory() as temp_dir:
            workspace_root = Path(temp_dir) / "project_legacy"
            (workspace_root / "pages" / "shared").mkdir(parents=True, exist_ok=True)
            (workspace_root / "fixtures").mkdir(parents=True, exist_ok=True)

            sync_workspace_support_files(str(workspace_root))

            self.assertTrue((workspace_root / "pages" / "LoginPage.ts").exists())
            self.assertTrue((workspace_root / "pages" / "shared" / "NavigationPage.ts").exists())
            self.assertTrue((workspace_root / "fixtures" / "auth.fixture.ts").exists())
            self.assertTrue((workspace_root / "auth_states" / "default.json").exists())

    def test_build_base_fixture_skips_missing_fixture_files(self) -> None:
        """当 registry 条目存在但物理文件缺失时，不应继续生成坏 import。"""
        registry_data = {
            "version": 3,
            "project": {"project_id": 1, "project_key": "project_1"},
            "pages": {},
            "shared": {
                "LoginPage": {
                    "kind": "shared",
                    "file": "pages/LoginPage.ts",
                    "fixture_name": "loginPage",
                    "shared_dependencies": [],
                    "locators": {},
                    "actions": {},
                    "page_update_mode": "append_only",
                },
                "NavigationPage": {
                    "kind": "shared",
                    "file": "pages/shared/NavigationPage.ts",
                    "fixture_name": "navigationPage",
                    "shared_dependencies": [],
                    "locators": {},
                    "actions": {},
                    "page_update_mode": "append_only",
                },
            },
        }

        with tempfile.TemporaryDirectory() as temp_dir:
            workspace_root = Path(temp_dir) / "project_fixture"
            (workspace_root / "registry").mkdir(parents=True, exist_ok=True)
            (workspace_root / "pages" / "shared").mkdir(parents=True, exist_ok=True)
            (workspace_root / "pages" / "shared" / "NavigationPage.ts").write_text(
                "export class NavigationPage {}",
                encoding="utf-8",
            )

            registry_path = workspace_root / "registry" / "page-registry.json"
            registry_path.write_text(json.dumps(registry_data, ensure_ascii=False, indent=2), encoding="utf-8")

            registry = PageRegistry(str(registry_path))
            content = build_base_fixture(registry, str(workspace_root))

            self.assertIn('import { NavigationPage } from "../pages/shared/NavigationPage";', content)
            self.assertNotIn('import { LoginPage } from "../pages/LoginPage";', content)
            self.assertNotIn("loginPage:", content)


if __name__ == "__main__":
    unittest.main()
