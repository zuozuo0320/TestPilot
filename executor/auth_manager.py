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

LOGIN_URL_KEYWORDS = (
    "/login",
    "/signin",
    "/sign-in",
    "/auth/login",
    "login?",
    "redirect=login",
)


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

    # 检查 2: Cookie / localStorage 认证凭据
    try:
        with open(path, "r", encoding="utf-8") as f:
            state = json.load(f)

        # ── 2a: 检查 localStorage 中是否有 token 类条目 ──
        # 许多 SPA 应用（如 foradar）将 Bearer token 存在 localStorage 而非 Cookie
        token_keywords = ("token", "auth", "jwt", "session", "sid")
        origins = state.get("origins", [])
        has_ls_token = False
        for origin in origins:
            for item in origin.get("localStorage", []):
                if any(kw in item.get("name", "").lower() for kw in token_keywords):
                    has_ls_token = True
                    break
            if has_ls_token:
                break

        if has_ls_token:
            logger.info(
                f"Valid localStorage token found for {start_url}, "
                f"skipping cookie expiry check"
            )
            # localStorage token 存在，文件未超龄 → 视为有效
        else:
            # ── 2b: 回退到 Cookie 过期检测 ──
            cookies = state.get("cookies", [])
            now = time.time()

            session_keywords_cookie = ("token", "session", "auth", "sid", "jwt")
            session_cookies = [
                c for c in cookies
                if any(
                    kw in c.get("name", "").lower()
                    for kw in session_keywords_cookie
                )
            ]

            if session_cookies:
                # expires <= 0 表示 session cookie（Playwright 用 -1 表示无显式过期时间），
                # 浏览器关闭前一直有效，Playwright 恢复后同样有效，不应判定为过期
                all_expired = all(
                    0 < c.get("expires", float("inf")) < now
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
                logger.debug(
                    "No session-related cookies found, treating as valid"
                )

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


def page_requires_login(page) -> bool:
    """根据 URL、密码框和登录文案判断当前页面是否需要重新登录"""
    current_url = (page.url or "").lower()
    if any(keyword in current_url for keyword in LOGIN_URL_KEYWORDS):
        return True

    try:
        password_inputs = page.locator("input[type='password']")
        for index in range(min(password_inputs.count(), 3)):
            if password_inputs.nth(index).is_visible(timeout=500):
                return True
    except Exception:
        pass

    try:
        body_text = page.locator("body").inner_text(timeout=1000).lower()
        has_login_text = any(text in body_text for text in ("登录", "登陆", "login", "sign in"))
        has_password_text = any(text in body_text for text in ("密码", "password"))
        if has_login_text and has_password_text:
            return True
    except Exception:
        pass

    return False


def refresh_auth_state(start_url: str) -> dict:
    """使用已保存的 storageState 打开目标站点，确认可用后刷新保存认证状态"""
    from playwright.sync_api import sync_playwright

    auth_state_path = os.path.abspath(get_auth_state_path(start_url))
    if not os.path.exists(auth_state_path):
        return {
            "success": False,
            "auth_state_path": auth_state_path,
            "error": "认证状态缺失，请在 Token 管理中完成手动登录后再执行验证",
            "checked_at": int(time.time()),
        }

    try:
        with sync_playwright() as pw:
            browser = pw.chromium.launch(
                headless=True,
                args=["--disable-blink-features=AutomationControlled"],
            )
            try:
                context = browser.new_context(
                    storage_state=auth_state_path,
                    ignore_https_errors=True,
                )
                try:
                    page = context.new_page()
                    page.goto(start_url, wait_until="domcontentloaded", timeout=30000)
                    page.wait_for_timeout(2000)
                    if page_requires_login(page):
                        return {
                            "success": False,
                            "auth_state_path": auth_state_path,
                            "error": "认证状态已失效，请在 Token 管理中重新手动登录后再执行验证",
                            "checked_at": int(time.time()),
                        }
                    context.storage_state(path=auth_state_path)
                    logger.info(f"Auth state refreshed: {auth_state_path}")
                    return {
                        "success": True,
                        "auth_state_path": auth_state_path,
                        "error": "",
                        "checked_at": int(time.time()),
                    }
                finally:
                    context.close()
            finally:
                browser.close()
    except Exception as e:
        return {
            "success": False,
            "auth_state_path": auth_state_path,
            "error": f"认证状态预检失败，请确认目标站点可访问或重新登录 Token: {e}",
            "checked_at": int(time.time()),
        }


def get_auth_state_info(start_url: str) -> dict:
    """获取 auth_state 的详细信息（用于 API 查询）"""
    path = get_auth_state_path(start_url)
    if not os.path.exists(path):
        return {
            "exists": False,
            "valid": False,
            "file_path": None,
            "file_age_hours": None,
            "max_age_hours": AUTH_STATE_MAX_AGE_HOURS,
            "remaining_hours": None,
            "cookie_count": 0,
        }

    file_age_hours = (time.time() - os.path.getmtime(path)) / 3600
    valid = has_valid_auth_state(start_url)
    remaining_hours = max(0, AUTH_STATE_MAX_AGE_HOURS - file_age_hours)

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
        "max_age_hours": AUTH_STATE_MAX_AGE_HOURS,
        "remaining_hours": round(remaining_hours, 1),
        "cookie_count": cookie_count,
    }
