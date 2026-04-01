"""
fixture_builder.py — Fixture 自动生成器

职责：
  1. 根据 page-registry.json 自动重建 base.fixture.ts
  2. 按字母序排列 import + fixture 注册
  3. 计算 fixture hash 用于增量判断
"""
import hashlib
import logging
import os
from typing import Optional

from page_registry import PageRegistry

logger = logging.getLogger(__name__)


def build_base_fixture(
    registry: PageRegistry,
    workspace_root: str,
) -> str:
    """
    根据 registry 生成 base.fixture.ts 内容

    Returns:
        生成的 TypeScript 代码字符串
    """
    fixture_list = registry.build_fixture_list()
    fixture_list = _filter_existing_fixtures(fixture_list, workspace_root)

    if not fixture_list:
        return _build_empty_fixture()

    # 按照 fixture_name 字母序排列
    fixture_list.sort(key=lambda f: f["fixture_name"])

    imports = []
    fixture_defs = []

    for item in fixture_list:
        class_name = item["class_name"]
        fixture_name = item["fixture_name"]
        file_path = item["file_path"]

        # 计算相对路径：从 fixtures/ 到 pages/
        rel_path = _compute_import_path(file_path)

        imports.append(f'import {{ {class_name} }} from "{rel_path}";')
        fixture_defs.append(_build_fixture_def(class_name, fixture_name))

    # 组装完整文件
    content = _assemble_fixture_file(imports, fixture_defs)
    return content


def write_base_fixture(
    registry: PageRegistry,
    workspace_root: str,
) -> tuple[str, str]:
    """
    生成并写入 base.fixture.ts

    Returns:
        (content, content_hash) 元组
    """
    content = build_base_fixture(registry, workspace_root)
    fixture_path = os.path.join(workspace_root, "fixtures", "base.fixture.ts")

    os.makedirs(os.path.dirname(fixture_path), exist_ok=True)
    with open(fixture_path, "w", encoding="utf-8") as f:
        f.write(content)

    content_hash = compute_hash(content)
    logger.info(f"[fixture] 写入 base.fixture.ts, hash={content_hash[:8]}")
    return content, content_hash


def compute_hash(content: str) -> str:
    """计算内容 SHA-256 哈希"""
    return hashlib.sha256(content.encode("utf-8")).hexdigest()


def _compute_import_path(page_file_path: str) -> str:
    """
    计算从 fixtures/base.fixture.ts 到页面文件的相对 import 路径
    例如：pages/LoginPage.ts → ../pages/LoginPage
    """
    # 去掉 .ts 扩展名
    path_no_ext = page_file_path
    if path_no_ext.endswith(".ts"):
        path_no_ext = path_no_ext[:-3]
    # 从 fixtures/ 引用，需要加 ../
    return f"../{path_no_ext}"


def _filter_existing_fixtures(fixture_list: list[dict], workspace_root: str) -> list[dict]:
    """过滤掉 registry 中存在但物理文件缺失的 fixture，避免生成坏 import。"""
    filtered: list[dict] = []

    for item in fixture_list:
        file_path = item.get("file_path", "")
        if not file_path:
            logger.warning("[fixture] 跳过缺少 file_path 的 fixture: %s", item.get("class_name", "unknown"))
            continue

        full_path = os.path.join(workspace_root, file_path)
        if not os.path.exists(full_path):
            logger.warning(
                "[fixture] 跳过缺少物理文件的 fixture: %s (%s)",
                item.get("class_name", "unknown"),
                full_path,
            )
            continue

        filtered.append(item)

    return filtered


def _build_fixture_def(class_name: str, fixture_name: str) -> str:
    """构建单个 fixture 定义代码块"""
    return f"""  {fixture_name}: async ({{ page }}, use) => {{
    await use(new {class_name}(page));
  }},"""


def _build_empty_fixture() -> str:
    """构建空 fixture 文件（无注册页面时）"""
    return '''/**
 * base.fixture.ts — 自动生成，请勿手动修改
 * 由 fixture_builder.py 根据 page-registry.json 生成
 */
import { test as base } from "@playwright/test";

// 暂无注册页面
export const test = base;
export { expect } from "@playwright/test";
'''


def _assemble_fixture_file(imports: list[str], fixture_defs: list[str]) -> str:
    """组装完整的 base.fixture.ts 文件"""
    imports_block = "\n".join(imports)
    fixtures_block = "\n".join(fixture_defs)

    # 构建泛型类型声明（与架构文档示例一致，提供 IDE 类型提示）
    type_entries = []
    for imp in imports:
        # 从 import { ClassName } from "..." 提取类名
        class_name = imp.split("{")[1].split("}")[0].strip()
        # 从 fixture_defs 中找到对应的 fixture_name
        for fd in fixture_defs:
            # fixture_defs 格式: "  fixtureName: async ({ page }, use) => {"
            fd_stripped = fd.strip()
            if class_name in fd_stripped:
                fixture_name = fd_stripped.split(":")[0].strip()
                type_entries.append(f"  {fixture_name}: {class_name};")
                break

    type_block = "\n".join(type_entries)

    return f'''/**
 * base.fixture.ts — 自动生成，请勿手动修改
 * 由 fixture_builder.py 根据 page-registry.json 生成
 */
import {{ test as base, expect }} from "@playwright/test";
{imports_block}

export const test = base.extend<{{
{type_block}
}}>({{
{fixtures_block}
}});

export {{ expect }};
'''
