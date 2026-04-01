"""
page_registry.py — Page Registry 引擎

职责：
  1. 读写 page-registry.json（V3 schema）
  2. 页面判定：新建 vs 更新 vs 共享页面复用
  3. 提供 LLM 上下文注入（已存在页面摘要）
  4. 注册表合并和增量更新
"""
import json
import logging
import os
from copy import deepcopy
from typing import Any, Optional

from shared_registry_defaults import (
    build_builtin_shared_deprecations,
    build_builtin_shared_registry,
)

logger = logging.getLogger(__name__)


class PageRegistry:
    """项目级页面注册表管理器"""

    def __init__(self, registry_path: str):
        self.registry_path = registry_path
        self._data: dict = {}
        self._load()

    def _load(self) -> None:
        """加载 registry 文件"""
        needs_save = False
        if not os.path.exists(self.registry_path):
            logger.warning(f"[registry] 文件不存在: {self.registry_path}")
            self._data = self._empty_registry()
            if self._normalize_builtin_shared_entries():
                needs_save = True
            if needs_save:
                self._save()
            return
        try:
            with open(self.registry_path, "r", encoding="utf-8") as f:
                self._data = json.load(f)
            if self._normalize_builtin_shared_entries():
                needs_save = True
            if needs_save:
                self._save()
            logger.info(f"[registry] 加载成功: {len(self.pages)} pages, {len(self.shared)} shared")
        except Exception as e:
            logger.error(f"[registry] 加载失败: {e}")
            self._data = self._empty_registry()
            if self._normalize_builtin_shared_entries():
                self._save()

    def _save(self) -> None:
        """持久化 registry 到文件"""
        os.makedirs(os.path.dirname(self.registry_path), exist_ok=True)
        with open(self.registry_path, "w", encoding="utf-8") as f:
            json.dump(self._data, f, ensure_ascii=False, indent=2)

    @staticmethod
    def _empty_registry() -> dict:
        return {"version": 3, "pages": {}, "shared": {}}

    def _normalize_builtin_shared_entries(self) -> bool:
        """
        统一内置 Shared Page 的 registry 结构。

        目标：
        1. 新旧项目都对齐到同一套 shared 标准定义。
        2. 移除已经废弃的内置 shared 字段，避免继续误导 LLM。
        3. 保留标准定义之外的扩展字段，兼容历史项目的 append_only 增量扩展。
        """
        builtin_shared = build_builtin_shared_registry()
        deprecated_map = build_builtin_shared_deprecations()

        if "shared" not in self._data or not isinstance(self._data["shared"], dict):
            self._data["shared"] = {}

        changed = False

        for page_name, default_entry in builtin_shared.items():
            existing_entry = self._data["shared"].get(page_name)
            if not existing_entry:
                self._data["shared"][page_name] = default_entry
                changed = True
                continue

            normalized_entry = deepcopy(default_entry)
            deprecated = deprecated_map.get(page_name, {})
            deprecated_locators = set(deprecated.get("locators", set()))
            deprecated_actions = set(deprecated.get("actions", set()))

            existing_locators = existing_entry.get("locators", {}) or {}
            extra_locators = {
                key: value
                for key, value in existing_locators.items()
                if key not in normalized_entry.get("locators", {}) and key not in deprecated_locators
            }
            normalized_entry["locators"].update(extra_locators)

            existing_actions = existing_entry.get("actions", {}) or {}
            extra_actions = {
                key: value
                for key, value in existing_actions.items()
                if key not in normalized_entry.get("actions", {}) and key not in deprecated_actions
            }
            normalized_entry["actions"].update(extra_actions)

            normalized_entry["shared_dependencies"] = sorted(
                set(normalized_entry.get("shared_dependencies", []))
                | set(existing_entry.get("shared_dependencies", []) or [])
            )

            # 保留标准定义之外的附加元数据，避免迁移时丢失历史信息。
            for key, value in existing_entry.items():
                if key not in normalized_entry:
                    normalized_entry[key] = value

            if normalized_entry != existing_entry:
                self._data["shared"][page_name] = normalized_entry
                changed = True

        return changed

    # ── 属性访问 ──

    @property
    def pages(self) -> dict:
        return self._data.get("pages", {})

    @property
    def shared(self) -> dict:
        return self._data.get("shared", {})

    @property
    def version(self) -> int:
        return self._data.get("version", 3)

    # ── 页面判定 ──

    def resolve_page(self, page_class_name: str) -> dict:
        """
        判定页面归属：
        - 如果已在 pages 中注册 → 返回 {action: 'update', entry: ...}
        - 如果已在 shared 中注册 → 返回 {action: 'reuse_shared', entry: ...}
        - 否则 → 返回 {action: 'create'}
        """
        # 先检查业务页面
        if page_class_name in self.pages:
            return {
                "action": "update",
                "entry": self.pages[page_class_name],
                "kind": "page",
            }

        # 再检查共享页面
        if page_class_name in self.shared:
            return {
                "action": "reuse_shared",
                "entry": self.shared[page_class_name],
                "kind": "shared",
            }

        # 全新页面
        return {"action": "create", "kind": "page"}

    def has_page(self, page_class_name: str) -> bool:
        """判断页面是否已注册（业务或共享）"""
        return page_class_name in self.pages or page_class_name in self.shared

    def get_page_entry(self, page_class_name: str) -> Optional[dict]:
        """获取页面注册条目"""
        if page_class_name in self.pages:
            return self.pages[page_class_name]
        if page_class_name in self.shared:
            return self.shared[page_class_name]
        return None

    def get_fixture_name(self, page_class_name: str) -> Optional[str]:
        """获取页面对应的 fixture 名称"""
        entry = self.get_page_entry(page_class_name)
        if entry:
            return entry.get("fixture_name")
        return None

    # ── 注册操作 ──

    def register_page(self, page_class_name: str, entry: dict, save: bool = True) -> None:
        """注册新的业务页面"""
        if "pages" not in self._data:
            self._data["pages"] = {}
        self._data["pages"][page_class_name] = entry
        logger.info(f"[registry] 注册页面: {page_class_name}")
        if save:
            self._save()

    def update_page(self, page_class_name: str, updates: dict, save: bool = True) -> None:
        """增量更新已存在的业务页面条目"""
        if page_class_name not in self.pages:
            logger.warning(f"[registry] 页面不存在，跳过更新: {page_class_name}")
            return

        entry = self.pages[page_class_name]

        # 合并 locators（增量追加）
        if "locators" in updates:
            existing_locators = entry.get("locators", {})
            existing_locators.update(updates["locators"])
            entry["locators"] = existing_locators

        # 合并 actions（增量追加）
        if "actions" in updates:
            existing_actions = entry.get("actions", {})
            existing_actions.update(updates["actions"])
            entry["actions"] = existing_actions

        # 合并 shared_dependencies（去重追加）
        if "shared_dependencies" in updates:
            existing_deps = set(entry.get("shared_dependencies", []))
            existing_deps.update(updates["shared_dependencies"])
            entry["shared_dependencies"] = sorted(existing_deps)

        # 其他简单字段直接覆盖
        for key in ["file", "fixture_name", "kind"]:
            if key in updates:
                entry[key] = updates[key]

        self._data["pages"][page_class_name] = entry
        if save:
            self._save()

    def update_shared(self, page_class_name: str, updates: dict, save: bool = True) -> None:
        """增量更新已存在的共享页面条目"""
        if page_class_name not in self.shared:
            logger.warning(f"[registry] 共享页面不存在，跳过更新: {page_class_name}")
            return

        entry = self.shared[page_class_name]

        # 合并 locators（增量追加）
        if "locators" in updates:
            existing_locators = entry.get("locators", {})
            existing_locators.update(updates["locators"])
            entry["locators"] = existing_locators

        # 合并 actions（增量追加）
        if "actions" in updates:
            existing_actions = entry.get("actions", {})
            existing_actions.update(updates["actions"])
            entry["actions"] = existing_actions

        # 其他简单字段直接覆盖
        for key in ["file", "fixture_name", "kind"]:
            if key in updates:
                entry[key] = updates[key]

        self._data["shared"][page_class_name] = entry
        if save:
            self._save()

    def batch_update(self, page_updates: dict[str, dict], save: bool = True) -> None:
        """批量更新/注册页面"""
        for page_name, update_data in page_updates.items():
            if page_name in self.shared:
                # shared 页面走 shared 更新路径
                self.update_shared(page_name, update_data, save=False)
            elif page_name in self.pages:
                self.update_page(page_name, update_data, save=False)
            else:
                self.register_page(page_name, update_data, save=False)
        if save:
            self._save()

    # ── LLM 上下文生成 ──

    def build_llm_context(self) -> dict:
        """
        构建传递给 LLM 的结构化 registry 上下文。

        和早期只传“类名 + locator 名称列表”的做法不同，
        这里保留页面身份、依赖关系、locator 摘要和 action 签名，
        让 LLM 能按统一架构规则做复用/增量更新判断。
        """
        context = {
            "project": self._data.get("project", {}),
            "existing_pages": {},
            "shared_pages": {},
        }

        for name, entry in self.pages.items():
            context["existing_pages"][name] = self._build_context_entry(entry)

        for name, entry in self.shared.items():
            context["shared_pages"][name] = self._build_context_entry(entry)

        return context

    def _build_context_entry(self, entry: dict) -> dict:
        """将单个 registry 条目转换为适合传递给 LLM 的上下文结构。"""
        return {
            "kind": entry.get("kind", ""),
            "fixture_name": entry.get("fixture_name", ""),
            "file": entry.get("file", ""),
            "module_key": entry.get("module_key", ""),
            "page_identity": entry.get("page_identity", {}),
            "shared_dependencies": entry.get("shared_dependencies", []),
            "page_update_mode": entry.get("page_update_mode", "append_only"),
            "locators": self._build_locator_context(entry.get("locators", {})),
            "actions": self._build_action_context(entry.get("actions", {})),
        }

    @staticmethod
    def _build_locator_context(locators: dict) -> dict:
        """保留 locator 名称与摘要，避免上下文里丢掉 locator 语义。"""
        result: dict[str, dict] = {}
        for name, value in (locators or {}).items():
            if isinstance(value, dict):
                result[name] = {
                    "summary": value.get("summary", ""),
                }
            else:
                result[name] = {"summary": str(value)}
        return result

    @staticmethod
    def _build_action_context(actions: dict) -> dict:
        """保留 action 的摘要、签名和依赖 locator，便于 LLM 做增量复用。"""
        result: dict[str, dict] = {}
        for name, value in (actions or {}).items():
            if isinstance(value, dict):
                result[name] = {
                    "summary": value.get("summary", ""),
                    "params_signature": value.get("params_signature", ""),
                    "uses_locators": value.get("uses_locators", []),
                    "update_mode": value.get("update_mode", "non_breaking_only"),
                }
            else:
                result[name] = {
                    "summary": str(value),
                    "params_signature": "",
                    "uses_locators": [],
                    "update_mode": "non_breaking_only",
                }
        return result

    def build_fixture_list(self) -> list[dict]:
        """
        构建 base.fixture.ts 所需的 fixture 注册列表
        格式：[{class_name, fixture_name, file_path, kind}]
        """
        fixtures = []

        # 业务页面
        for name, entry in sorted(self.pages.items()):
            fixtures.append({
                "class_name": name,
                "fixture_name": entry.get("fixture_name", ""),
                "file_path": entry.get("file", ""),
                "kind": "page",
            })

        # 共享页面
        for name, entry in sorted(self.shared.items()):
            fixtures.append({
                "class_name": name,
                "fixture_name": entry.get("fixture_name", ""),
                "file_path": entry.get("file", ""),
                "kind": "shared",
            })

        return fixtures

    # ── 快照与恢复 ──

    def snapshot(self) -> dict:
        """返回当前 registry 完整快照（用于版本记录）"""
        import copy
        return copy.deepcopy(self._data)

    def restore_from_snapshot(self, snapshot: dict) -> None:
        """从快照恢复 registry"""
        self._data = snapshot
        self._save()

    # ── 工具方法 ──

    def to_json(self) -> str:
        """返回 JSON 字符串表示"""
        return json.dumps(self._data, ensure_ascii=False, indent=2)
