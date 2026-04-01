"""
auth_manager.py — 认证状态管理器

职责：
  - 按域名存储/加载 Playwright storageState
  - 检测 Cookie/Token 是否过期
  - 管理 auth_states/ 目录
"""
import json
import logging
import os
import time
from urllib.parse import urlparse

from config import AUTH_STATE_DIR, AUTH_STATE_MAX_AGE_HOURS

logger = logging.getLogger(__name__)


def _domain_from_url(url: str) -> str:
    """从 URL 提取域名（含端口）作为 auth_state 文件名"""
    parsed = urlparse(url)
    host = parsed.hostname or "default"
    port = parsed.port
    if port and port not in (80, 443):
        return f"{host}_{port}"
    return host


def get_auth_state_path(start_url: str) -> str:
    """获取指定 URL 对应的 auth_state 文件路径"""
    os.makedirs(AUTH_STATE_DIR, exist_ok=True)
    domain = _domain_from_url(start_url)
    return os.path.join(AUTH_STATE_DIR, f"{domain}.json")


def has_valid_auth_state(start_url: str) -> bool:
    """
    检查是否存在有效的 auth_state

    双重检测机制：
      1. 文件年龄：超过 AUTH_STATE_MAX_AGE_HOURS 则过期
      2. Cookie 过期：所有 session 相关 Cookie 的 expires < 当前时间
    """
    path = get_auth_state_path(start_url)
    if not os.path.exists(path):
        logger.info(f"No auth_state file for {start_url}")
        return False

    # 检查 1: 文件年龄
    file_age_hours = (time.time() - os.path.getmtime(path)) / 3600
    if file_age_hours > AUTH_STATE_MAX_AGE_HOURS:
        logger.info(
            f"auth_state expired by file age "
            f"({file_age_hours:.1f}h > {AUTH_STATE_MAX_AGE_HOURS}h)"
        )
        return False

    # 检查 2: Cookie 过期时间
    try:
        with open(path, "r", encoding="utf-8") as f:
            state = json.load(f)

        cookies = state.get("cookies", [])
        now = time.time()

        # 寻找 session 相关的 Cookie（name 中包含 token/session/auth）
        session_keywords = ("token", "session", "auth", "sid", "jwt")
        session_cookies = [
            c for c in cookies
            if any(kw in c.get("name", "").lower() for kw in session_keywords)
        ]

        if session_cookies:
            all_expired = all(
                c.get("expires", float("inf")) < now
                for c in session_cookies
            )
            if all_expired:
                logger.info(
                    f"All session cookies expired "
                    f"({len(session_cookies)} cookies checked)"
                )
                return False
        else:
            # 没有明确的 session Cookie，但文件存在且未超龄 → 视为有效
            logger.debug("No session-related cookies found, treating as valid")

    except Exception as e:
        logger.warning(f"Failed to parse auth_state file: {e}")
        return False

    logger.info(f"Valid auth_state found for {start_url} (age={file_age_hours:.1f}h)")
    return True


def invalidate_auth_state(start_url: str):
    """主动失效指定 URL 的 auth_state（用于强制重新登录）"""
    path = get_auth_state_path(start_url)
    if os.path.exists(path):
        os.remove(path)
        logger.info(f"Invalidated auth_state for {start_url}")
    else:
        logger.info(f"No auth_state to invalidate for {start_url}")


def get_auth_state_info(start_url: str) -> dict:
    """获取 auth_state 的详细信息（用于 API 查询）"""
    path = get_auth_state_path(start_url)
    if not os.path.exists(path):
        return {
            "exists": False,
            "valid": False,
            "file_path": None,
            "file_age_hours": None,
            "cookie_count": 0,
        }

    file_age_hours = (time.time() - os.path.getmtime(path)) / 3600
    valid = has_valid_auth_state(start_url)

    cookie_count = 0
    try:
        with open(path, "r", encoding="utf-8") as f:
            state = json.load(f)
        cookie_count = len(state.get("cookies", []))
    except Exception:
        pass

    return {
        "exists": True,
        "valid": valid,
        "file_path": path,
        "file_age_hours": round(file_age_hours, 1),
        "cookie_count": cookie_count,
    }
