"""
ast_merger_bridge.py — AST 合并器 Python 桥接

职责：
  1. 将 LLM 输出的 page_updates 转换为 AST 合并器输入
  2. 通过 subprocess 调用 ts-morph 合并器
  3. 解析合并结果，返回给 V1 管线
"""
import json
import logging
import os
import subprocess
from typing import Optional

logger = logging.getLogger(__name__)

# AST 合并器项目路径
_AST_MERGER_DIR = os.path.join(os.path.dirname(__file__), "ast_merger")


def apply_page_updates(
    workspace_root: str,
    page_updates: list[dict],
    registry_data: dict,
) -> dict:
    """
    应用页面增量更新

    Args:
        workspace_root: 项目工作区根路径
        page_updates: LLM 输出的 page_updates 列表
        registry_data: 当前 registry 数据（用于查找文件路径）

    Returns:
        {
            success: bool,
            merged_files: [...],
            errors: [...],
            warnings: [...],
            manual_review_needed: bool,
        }
    """
    if not page_updates:
        return {
            "success": True,
            "merged_files": [],
            "errors": [],
            "warnings": [],
            "manual_review_needed": False,
        }

    # 构建合并器输入
    updates = []
    manual_review_items = []

    for pu in page_updates:
        page_name = pu.get("page_name", "")
        operation = pu.get("operation", "")

        if not page_name:
            continue

        # 从 registry 查找文件路径
        file_path = _resolve_file_path(page_name, registry_data)
        if not file_path:
            manual_review_items.append(f"页面 {page_name} 在 registry 中未找到文件路径")
            continue

        update_entry = {
            "page_name": page_name,
            "file_path": file_path,
            "operation": operation,
            "new_locators": pu.get("new_locators", []),
            "new_actions": pu.get("new_actions", []),
            "extend_actions": pu.get("extend_actions", []),
        }
        updates.append(update_entry)

    if not updates:
        return {
            "success": True,
            "merged_files": [],
            "errors": manual_review_items,
            "warnings": [],
            "manual_review_needed": len(manual_review_items) > 0,
        }

    # 构建合并器 JSON 输入
    merge_input = {
        "workspace_root": workspace_root,
        "updates": updates,
    }

    # 调用 AST 合并器
    result = _call_ast_merger(merge_input)

    if result is None:
        return {
            "success": False,
            "merged_files": [],
            "errors": ["AST 合并器调用失败"],
            "warnings": [],
            "manual_review_needed": True,
        }

    result["manual_review_needed"] = (
        not result.get("success", False) or
        len(result.get("errors", [])) > 0 or
        len(manual_review_items) > 0
    )
    result.setdefault("errors", []).extend(manual_review_items)

    return result


def _resolve_file_path(page_name: str, registry_data: dict) -> Optional[str]:
    """从 registry 解析页面的文件路径"""
    # 检查 pages
    pages = registry_data.get("pages", {})
    if page_name in pages:
        return pages[page_name].get("file", "")

    # 检查 shared
    shared = registry_data.get("shared", {})
    if page_name in shared:
        return shared[page_name].get("file", "")

    return None


def _call_ast_merger(merge_input: dict) -> Optional[dict]:
    """通过 subprocess 调用 ts-morph AST 合并器"""
    try:
        # 检查 node_modules 是否已安装
        node_modules = os.path.join(_AST_MERGER_DIR, "node_modules")
        if not os.path.exists(node_modules):
            logger.info("[ast-merger] 安装依赖...")
            subprocess.run(
                ["npm", "install"],
                cwd=_AST_MERGER_DIR,
                capture_output=True,
                timeout=60,
                check=True,
                shell=True,
            )

        input_json = json.dumps(merge_input, ensure_ascii=False)

        proc = subprocess.run(
            ["npx", "tsx", "src/merger.ts"],
            cwd=_AST_MERGER_DIR,
            input=input_json,
            capture_output=True,
            text=True,
            encoding="utf-8",
            timeout=30,
            shell=True,
        )

        if proc.returncode != 0:
            logger.error(f"[ast-merger] 进程退出码 {proc.returncode}: {proc.stderr}")
            return None

        result = json.loads(proc.stdout)
        logger.info(f"[ast-merger] 合并完成: {len(result.get('merged_files', []))} 文件")
        return result

    except subprocess.TimeoutExpired:
        logger.error("[ast-merger] 执行超时")
        return None
    except json.JSONDecodeError as e:
        logger.error(f"[ast-merger] 输出解析失败: {e}")
        return None
    except Exception as e:
        logger.error(f"[ast-merger] 调用异常: {e}")
        return None
