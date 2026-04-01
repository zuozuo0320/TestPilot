import json
import tempfile
import unittest
from pathlib import Path

from page_registry import PageRegistry


class PageRegistryTestCase(unittest.TestCase):
    """校验 registry 上下文构建与内置 shared 归一化逻辑。"""

    def test_build_llm_context_keeps_full_schema_and_normalizes_builtin_shared(self) -> None:
        """应当保留 page_identity / action 签名等字段，并清理过时的内置 shared 定义。"""
        registry_data = {
            "version": 3,
            "project": {
                "project_id": 1,
                "project_key": "project_1",
                "project_name": "AiSight Demo",
            },
            "pages": {
                "AssetDiscoveryTaskPage": {
                    "kind": "business",
                    "file": "pages/AssetDiscoveryTaskPage.ts",
                    "fixture_name": "assetDiscoveryTaskPage",
                    "module_key": "asset-discovery-task",
                    "page_identity": {
                        "active_menu": ["任务管理", "资产探知"],
                        "url_patterns": ["/workbench"],
                    },
                    "shared_dependencies": ["NavigationPage", "DialogPage"],
                    "locators": {
                        "createTaskButton": {"summary": "新建任务按钮"},
                    },
                    "actions": {
                        "createTask": {
                            "summary": "创建资产探知任务",
                            "params_signature": "(options: { target: string; taskName?: string })",
                            "uses_locators": ["createTaskButton"],
                            "update_mode": "non_breaking_only",
                        }
                    },
                    "page_update_mode": "append_only",
                }
            },
            "shared": {
                "NavigationPage": {
                    "kind": "shared",
                    "file": "pages/shared/NavigationPage.ts",
                    "fixture_name": "navigationPage",
                    "shared_dependencies": [],
                    "locators": {
                        "sidebarNav": {"summary": "旧版侧边栏容器"},
                        "breadcrumb": {"summary": "旧版面包屑容器"},
                    },
                    "actions": {
                        "goToMenu": {
                            "summary": "旧版菜单导航",
                            "params_signature": "(menuName: string)",
                            "uses_locators": ["sidebarNav"],
                            "update_mode": "non_breaking_only",
                        },
                        "expectBreadcrumbContains": {
                            "summary": "旧版面包屑断言",
                            "params_signature": "(text: string)",
                            "uses_locators": ["breadcrumb"],
                            "update_mode": "non_breaking_only",
                        },
                    },
                    "page_update_mode": "append_only",
                }
            },
        }

        with tempfile.TemporaryDirectory() as temp_dir:
            registry_path = Path(temp_dir) / "page-registry.json"
            registry_path.write_text(json.dumps(registry_data, ensure_ascii=False, indent=2), encoding="utf-8")

            registry = PageRegistry(str(registry_path))
            context = registry.build_llm_context()

            self.assertEqual(context["project"]["project_key"], "project_1")

            existing_page = context["existing_pages"]["AssetDiscoveryTaskPage"]
            self.assertEqual(existing_page["page_identity"]["active_menu"], ["任务管理", "资产探知"])
            self.assertEqual(existing_page["locators"]["createTaskButton"]["summary"], "新建任务按钮")
            self.assertEqual(
                existing_page["actions"]["createTask"]["params_signature"],
                "(options: { target: string; taskName?: string })",
            )
            self.assertEqual(
                existing_page["actions"]["createTask"]["uses_locators"],
                ["createTaskButton"],
            )

            navigation_page = context["shared_pages"]["NavigationPage"]
            self.assertIn("expectMenuActive", navigation_page["actions"])
            self.assertIn("expectPageContainsText", navigation_page["actions"])
            self.assertNotIn("expectBreadcrumbContains", navigation_page["actions"])
            self.assertEqual(navigation_page["locators"], {})


if __name__ == "__main__":
    unittest.main()
