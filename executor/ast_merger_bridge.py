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
import tempfile
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
    file_snapshots = _snapshot_workspace_files(workspace_root, updates)

    # 调用 AST 合并器
    result = _call_ast_merger(merge_input)

    if result is None:
        _restore_workspace_files(workspace_root, file_snapshots, file_snapshots.keys())
        return {
            "success": False,
            "merged_files": [],
            "errors": ["AST 合并器调用失败"],
            "warnings": [],
            "manual_review_needed": True,
        }

    corrupted_files = [
        merged_file["file_path"]
        for merged_file in result.get("merged_files", [])
        if _contains_replacement_characters(merged_file.get("content", ""))
    ]
    if corrupted_files:
        _restore_workspace_files(workspace_root, file_snapshots, corrupted_files)
        return {
            "success": False,
            "merged_files": [],
            "errors": [
                f"AST 合并结果出现乱码占位符字符 U+FFFD，已自动回滚文件: {file_path}"
                for file_path in corrupted_files
            ],
            "warnings": result.get("warnings", []),
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
    temp_input_path: str | None = None
    try:
        # 检查 node_modules 是否已安装
        node_modules = os.path.join(_AST_MERGER_DIR, "node_modules")
        if not os.path.exists(node_modules):
            logger.info("[ast-merger] 安装依赖...")
            subprocess.run(
                [_npm_command(), "install"],
                cwd=_AST_MERGER_DIR,
                capture_output=True,
                timeout=60,
                check=True,
                text=True,
                encoding="utf-8",
            )

        # Windows 下通过 shell + stdin 传递中文 JSON 时，容易在 AST 合并链路中被错误替换为 U+FFFD。
        # 这里统一落到 UTF-8 临时文件，再通过 --input 传给 merger.ts，避免命令行/标准输入编码串码。
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            suffix=".json",
            delete=False,
        ) as temp_file:
            json.dump(merge_input, temp_file, ensure_ascii=False)
            temp_input_path = temp_file.name

        proc = subprocess.run(
            [_npx_command(), "tsx", "src/merger.ts", "--input", temp_input_path],
            cwd=_AST_MERGER_DIR,
            capture_output=True,
            text=True,
            encoding="utf-8",
            timeout=30,
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
    finally:
        if temp_input_path and os.path.exists(temp_input_path):
            try:
                os.remove(temp_input_path)
            except OSError:
                logger.warning("[ast-merger] 临时输入文件删除失败: %s", temp_input_path)


def _npm_command() -> str:
    """按平台返回可直接执行的 npm 命令。"""
    return "npm.cmd" if os.name == "nt" else "npm"


def _npx_command() -> str:
    """按平台返回可直接执行的 npx 命令。"""
    return "npx.cmd" if os.name == "nt" else "npx"


def _snapshot_workspace_files(workspace_root: str, updates: list[dict]) -> dict[str, str]:
    """在 AST 合并前缓存目标文件内容，便于发现乱码后自动回滚。"""
    snapshots: dict[str, str] = {}

    for update in updates:
        file_path = update.get("file_path", "")
        if not file_path or file_path in snapshots:
            continue

        absolute_path = os.path.join(workspace_root, file_path)
        if not os.path.exists(absolute_path):
            continue

        with open(absolute_path, "r", encoding="utf-8") as file:
            snapshots[file_path] = file.read()

    return snapshots


def _restore_workspace_files(
    workspace_root: str,
    snapshots: dict[str, str],
    file_paths,
) -> None:
    """将指定文件恢复到 AST 合并前的内容。"""
    for file_path in file_paths:
        snapshot = snapshots.get(file_path)
        if snapshot is None:
            continue

        absolute_path = os.path.join(workspace_root, file_path)
        with open(absolute_path, "w", encoding="utf-8") as file:
            file.write(snapshot)


def _contains_replacement_characters(text: str) -> bool:
    """检测文本中是否包含 U+FFFD，出现时通常说明编码链路已经损坏。"""
    return "\ufffd" in (text or "")
