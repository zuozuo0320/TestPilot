"""
project_workspace.py — 项目工作区初始化与管理

职责：
  1. 解析 ProjectScope
  2. 初始化项目目录结构（幂等）
  3. 复制 Shared Page 模板到项目
  4. 初始化 page-registry.json
  5. 初始化项目级 playwright.config.ts 和 .env
  6. 根据 registry 重建 base.fixture.ts（委托给 fixture_builder）
"""
import hashlib
import json
import logging
import os
import shutil
from pathlib import Path
from typing import Any

from shared_registry_defaults import build_builtin_shared_registry

logger = logging.getLogger(__name__)

# 多项目根目录
PW_PROJECTS_ROOT = os.path.join(os.path.dirname(__file__), "pw_projects")
TEMPLATES_DIR = os.path.join(PW_PROJECTS_ROOT, "templates")
PROJECTS_DIR = os.path.join(PW_PROJECTS_ROOT, "projects")


class ProjectScope:
    """项目作用域，封装项目级元数据和路径解析"""

    def __init__(
        self,
        project_id: int,
        project_key: str,
        project_name: str = "",
    ):
        if not project_key or not project_key.strip():
            raise ValueError("project_key 不能为空")

        # 防止路径逃逸
        safe_key = project_key.strip().replace("..", "").replace("/", "").replace("\\", "")
        if not safe_key:
            raise ValueError(f"project_key 包含非法字符: {project_key}")

        self.project_id = project_id
        self.project_key = safe_key
        self.project_name = project_name or safe_key
        self.workspace_root = os.path.join(PROJECTS_DIR, safe_key)

    @property
    def registry_file(self) -> str:
        return os.path.join(self.workspace_root, "registry", "page-registry.json")

    @property
    def env_file(self) -> str:
        return os.path.join(self.workspace_root, ".env")

    @property
    def auth_state_dir(self) -> str:
        return os.path.join(self.workspace_root, "auth_states")

    @property
    def pages_dir(self) -> str:
        return os.path.join(self.workspace_root, "pages")

    @property
    def shared_dir(self) -> str:
        return os.path.join(self.workspace_root, "pages", "shared")

    @property
    def tests_dir(self) -> str:
        return os.path.join(self.workspace_root, "tests")

    @property
    def fixtures_dir(self) -> str:
        return os.path.join(self.workspace_root, "fixtures")

    @property
    def config_file(self) -> str:
        return os.path.join(self.workspace_root, "playwright.config.ts")

    def to_dict(self) -> dict[str, Any]:
        """导出为 JSON 可序列化的字典（用于传递给 LLM）"""
        return {
            "project_id": self.project_id,
            "project_key": self.project_key,
            "project_name": self.project_name,
            "workspace_root": f"pw_projects/projects/{self.project_key}",
            "registry_file": "registry/page-registry.json",
            "env_file": ".env",
            "auth_state_dir": "auth_states",
            "base_url_env": "BASE_URL",
            "auth_strategy": "storage_state",
        }

    def validate_path(self, target_path: str) -> bool:
        """校验目标路径是否在项目工作区内，防止路径逃逸"""
        real_workspace = os.path.realpath(self.workspace_root)
        real_target = os.path.realpath(
            os.path.join(self.workspace_root, target_path)
            if not os.path.isabs(target_path)
            else target_path
        )
        return real_target.startswith(real_workspace)


def resolve_project_scope(
    project_id: int,
    project_key: str,
    project_name: str = "",
) -> ProjectScope:
    """解析并返回 ProjectScope 实例"""
    return ProjectScope(
        project_id=project_id,
        project_key=project_key,
        project_name=project_name,
    )


def ensure_project_ready(scope: ProjectScope) -> str:
    """
    幂等入口：确保项目工作区已初始化。
    若已初始化（project.json 存在）则跳过。
    返回工作区根目录绝对路径。
    """
    project_json = os.path.join(scope.workspace_root, "project.json")
    if os.path.exists(project_json):
        sync_workspace_support_files(scope.workspace_root)
        logger.info(f"[workspace] 项目 {scope.project_key} 已初始化，跳过")
        return scope.workspace_root

    logger.info(f"[workspace] 初始化项目 {scope.project_key} ...")
    return init_project_workspace(scope)


def sync_workspace_support_files(workspace_root: str) -> None:
    """
    为已存在项目工作区补齐运行期必需的基础文件。

    兼容场景：
    1. 老项目在平台升级后新增了内置 shared 条目，但物理模板文件尚未补齐。
    2. 验证阶段直接复用历史工作区时，需要先确保 shared / fixture / auth_state 基础文件存在。
    """
    os.makedirs(workspace_root, exist_ok=True)

    pages_dir = os.path.join(workspace_root, "pages")
    shared_dir = os.path.join(pages_dir, "shared")
    fixtures_dir = os.path.join(workspace_root, "fixtures")
    auth_state_dir = os.path.join(workspace_root, "auth_states")

    for path in [pages_dir, shared_dir, fixtures_dir, auth_state_dir]:
        os.makedirs(path, exist_ok=True)

    template_mapping = {
        "NavigationPage.template.ts": os.path.join(shared_dir, "NavigationPage.ts"),
        "DialogPage.template.ts": os.path.join(shared_dir, "DialogPage.ts"),
        "ToastPage.template.ts": os.path.join(shared_dir, "ToastPage.ts"),
        "LoginPage.template.ts": os.path.join(pages_dir, "LoginPage.ts"),
    }
    _copy_templates_if_missing(template_mapping)

    auth_fixture_template = os.path.join(TEMPLATES_DIR, "fixtures", "auth.fixture.template.ts")
    auth_fixture_target = os.path.join(fixtures_dir, "auth.fixture.ts")
    if os.path.exists(auth_fixture_template) and not os.path.exists(auth_fixture_target):
        shutil.copy2(auth_fixture_template, auth_fixture_target)
        logger.info(f"[workspace] 补齐 auth.fixture.ts: {auth_fixture_target}")

    auth_state_target = os.path.join(auth_state_dir, "default.json")
    if not os.path.exists(auth_state_target):
        _write_json(auth_state_target, {"cookies": [], "origins": []})
        logger.info(f"[workspace] 补齐默认 auth_state: {auth_state_target}")


def init_project_workspace(scope: ProjectScope) -> str:
    """
    初始化项目目录结构，步骤：
    1. 创建目录结构
    2. 写入 project.json
    3. 复制 Shared Page 模板
    4. 初始化 page-registry.json
    5. 初始化 playwright.config.ts 和 .env
    6. 复制 auth.fixture.ts 模板
    7. 安装 npm 依赖（package.json）
    """
    root = scope.workspace_root

    # 1. 创建目录结构
    dirs = [
        root,
        scope.pages_dir,
        scope.shared_dir,
        scope.tests_dir,
        scope.fixtures_dir,
        scope.auth_state_dir,
        os.path.join(root, "registry"),
        os.path.join(root, "utils"),
    ]
    for d in dirs:
        os.makedirs(d, exist_ok=True)

    # 2. 写入 project.json
    project_meta = {
        "project_id": scope.project_id,
        "project_key": scope.project_key,
        "project_name": scope.project_name,
        "version": 1,
        "created_at": _now_iso(),
    }
    _write_json(os.path.join(root, "project.json"), project_meta)

    # 3. 复制 Shared Page 模板
    _copy_shared_templates(scope)

    # 4. 初始化 page-registry.json
    _init_page_registry(scope)

    # 5. 初始化配置文件
    _init_playwright_config(scope)
    _init_env_file(scope)

    # 6. 复制 auth.fixture.ts 模板
    _copy_auth_fixture(scope)

    # 7. 初始化 package.json
    _init_package_json(scope)

    # 8. 初始化空 auth_state
    _init_auth_state(scope)

    logger.info(f"[workspace] 项目 {scope.project_key} 初始化完成: {root}")
    return root


def _copy_shared_templates(scope: ProjectScope) -> None:
    """从模板目录复制 4 个 Shared Page 到项目中（去掉 .template 后缀）"""
    template_mapping = {
        "NavigationPage.template.ts": os.path.join(scope.shared_dir, "NavigationPage.ts"),
        "DialogPage.template.ts": os.path.join(scope.shared_dir, "DialogPage.ts"),
        "ToastPage.template.ts": os.path.join(scope.shared_dir, "ToastPage.ts"),
        "LoginPage.template.ts": os.path.join(scope.pages_dir, "LoginPage.ts"),
    }
    _copy_templates_if_missing(template_mapping)


def _init_page_registry(scope: ProjectScope) -> None:
    """初始化空的 page-registry.json（V3 schema）"""
    registry = {
        "version": 3,
        "project": scope.to_dict(),
        "pages": {},
        "shared": build_builtin_shared_registry(),
    }
    _write_json(scope.registry_file, registry)
    logger.info(f"[workspace] 初始化 registry: {scope.registry_file}")


def _init_playwright_config(scope: ProjectScope) -> None:
    """从模板初始化项目级 playwright.config.ts"""
    template = os.path.join(TEMPLATES_DIR, "config", "playwright.config.template.ts")
    if not os.path.exists(template):
        logger.warning(f"[workspace] 配置模板不存在: {template}")
        return

    content = Path(template).read_text(encoding="utf-8")
    # 替换占位符
    content = content.replace("{{PROJECT_KEY}}", scope.project_key)
    content = content.replace("{{PROJECT_NAME}}", scope.project_name)

    Path(scope.config_file).write_text(content, encoding="utf-8")
    logger.info(f"[workspace] 初始化配置: {scope.config_file}")


def _init_env_file(scope: ProjectScope) -> None:
    """从模板初始化 .env.example，复制为 .env"""
    template = os.path.join(TEMPLATES_DIR, "config", ".env.example")
    env_example = os.path.join(scope.workspace_root, ".env.example")
    env_file = scope.env_file

    if os.path.exists(template):
        shutil.copy2(template, env_example)
        if not os.path.exists(env_file):
            shutil.copy2(template, env_file)
        logger.info(f"[workspace] 初始化环境变量: {env_file}")


def _copy_auth_fixture(scope: ProjectScope) -> None:
    """从模板复制 auth.fixture.ts"""
    template = os.path.join(TEMPLATES_DIR, "fixtures", "auth.fixture.template.ts")
    target = os.path.join(scope.fixtures_dir, "auth.fixture.ts")
    if os.path.exists(template) and not os.path.exists(target):
        shutil.copy2(template, target)
        logger.info(f"[workspace] 复制 auth.fixture.ts: {target}")


def _init_package_json(scope: ProjectScope) -> None:
    """初始化项目级 package.json"""
    pkg_file = os.path.join(scope.workspace_root, "package.json")
    if os.path.exists(pkg_file):
        return

    pkg = {
        "name": f"testpilot-{scope.project_key}",
        "version": "1.0.0",
        "private": True,
        "devDependencies": {
            "@playwright/test": "^1.40.0",
            "dotenv": "^16.3.1",
        },
    }
    _write_json(pkg_file, pkg)
    logger.info(f"[workspace] 初始化 package.json: {pkg_file}")


def _init_auth_state(scope: ProjectScope) -> None:
    """初始化空的认证状态文件"""
    auth_file = os.path.join(scope.auth_state_dir, "default.json")
    if not os.path.exists(auth_file):
        _write_json(auth_file, {"cookies": [], "origins": []})


def compute_file_hash(content: str) -> str:
    """计算文件内容的 SHA-256 哈希"""
    return hashlib.sha256(content.encode("utf-8")).hexdigest()


# ── 工具函数 ──

def _write_json(filepath: str, data: dict) -> None:
    """写入 JSON 文件（UTF-8，4 空格缩进）"""
    os.makedirs(os.path.dirname(filepath), exist_ok=True)
    with open(filepath, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


def _copy_templates_if_missing(template_mapping: dict[str, str]) -> None:
    """按模板映射补齐缺失文件，不覆盖用户已有内容。"""
    shared_template_dir = os.path.join(TEMPLATES_DIR, "shared")
    if not os.path.exists(shared_template_dir):
        logger.warning(f"[workspace] 共享模板目录不存在: {shared_template_dir}")
        return

    for template_name, target_path in template_mapping.items():
        src = os.path.join(shared_template_dir, template_name)
        if not os.path.exists(src):
            logger.warning(f"[workspace] 模板文件不存在: {src}")
            continue
        if os.path.exists(target_path):
            continue
        os.makedirs(os.path.dirname(target_path), exist_ok=True)
        shutil.copy2(src, target_path)
        logger.info(f"[workspace] 复制模板: {template_name} → {target_path}")


def _now_iso() -> str:
    """返回当前时间的 ISO 格式字符串"""
    from datetime import datetime, timezone
    return datetime.now(timezone.utc).isoformat()
