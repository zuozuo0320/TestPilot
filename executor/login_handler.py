"""
login_handler.py — 自动登录处理器

核心流程（借鉴 fobrain Cypress 项目的 getCaptchaAndRecognize + tryLogin 模式）：
  1. Playwright Python 打开登录页
  2. 填写用户名密码
  3. 拦截 /api/captcha 接口获取 base64 验证码图片
  4. POST 到 OCR 服务识别
  5. 填写验证码并提交
  6. 识别失败自动重试（最多 MAX_RETRIES 次）
  7. 登录成功后保存 storageState
"""
import asyncio
import base64
import logging
import re
from dataclasses import dataclass, field
from typing import Optional

import httpx
from playwright.async_api import async_playwright, Page, BrowserContext

from auth_manager import get_auth_state_path
from config import OCR_SERVICE_URL, DEFAULT_LOGIN_URL, DEFAULT_LOGIN_USERNAME, DEFAULT_LOGIN_PASSWORD

logger = logging.getLogger(__name__)

# ── 内置 ddddocr 单例（懒加载，避免影响启动速度）──
_ocr_instance = None

def _get_local_ocr():
    """获取内置 ddddocr 实例（首次调用时初始化）"""
    global _ocr_instance
    if _ocr_instance is None:
        try:
            import ddddocr
            _ocr_instance = ddddocr.DdddOcr(show_ad=False)
            logger.info("[OCR] Local ddddocr initialized successfully")
        except ImportError:
            logger.warning("[OCR] ddddocr not installed, local OCR unavailable")
            _ocr_instance = False  # 标记为不可用避免重复 import
        except Exception as e:
            logger.error(f"[OCR] Failed to initialize ddddocr: {e}")
            _ocr_instance = False
    return _ocr_instance if _ocr_instance is not False else None

MAX_RETRIES = 5


@dataclass
class LoginConfig:
    """登录配置（可通过 auth_config JSON 传入，也可使用环境变量默认值）"""

    login_url: str = ""
    username: str = ""
    password: str = ""

    # 选择器（默认值适配常见登录页）
    username_selector: str = "#login-username"
    password_selector: str = "#login-password"
    captcha_input_selector: str = "#login-captcha"
    captcha_img_selector: str = ""  # 验证码图片元素（备用截图方式）
    submit_selector: str = "#login-submit"

    # 验证码 API 路径
    captcha_api: str = "/api/captcha"

    # OCR 服务地址
    ocr_service_url: str = ""

    # 登录成功判断
    success_url_pattern: str = ""  # 登录成功后 URL 正则
    success_element: str = ""  # 登录成功后出现的元素选择器

    # 登录失败判断
    error_element: str = ""  # 验证码错误提示元素
    error_text: str = "验证码错误"  # 验证码错误提示文本

    # 重试次数
    max_retries: int = MAX_RETRIES


def build_login_config(
    start_url: str,
    auth_config: Optional[dict] = None,
) -> LoginConfig:
    """
    从请求参数或环境变量构建 LoginConfig

    优先级：auth_config JSON > 环境变量默认值
    """
    cfg = LoginConfig()
    cfg.ocr_service_url = OCR_SERVICE_URL

    if auth_config:
        # 从任务传入的 auth_config JSON 构建
        cfg.login_url = auth_config.get("login_url", start_url)
        cfg.username = auth_config.get("username", "")
        cfg.password = auth_config.get("password", "")
        cfg.captcha_api = auth_config.get("captcha_api", cfg.captcha_api)

        selectors = auth_config.get("selectors", {})
        if selectors:
            cfg.username_selector = selectors.get("username", cfg.username_selector)
            cfg.password_selector = selectors.get("password", cfg.password_selector)
            cfg.captcha_input_selector = selectors.get(
                "captcha_input", cfg.captcha_input_selector
            )
            cfg.captcha_img_selector = selectors.get("captcha_img", "")
            cfg.submit_selector = selectors.get("submit", cfg.submit_selector)

        success = auth_config.get("success", {})
        if success:
            cfg.success_url_pattern = success.get("url_pattern", "")
            cfg.success_element = success.get("element", "")

        error = auth_config.get("error", {})
        if error:
            cfg.error_element = error.get("element", "")
            cfg.error_text = error.get("text", cfg.error_text)

        cfg.max_retries = auth_config.get("max_retries", MAX_RETRIES)
    else:
        # 使用环境变量默认值
        cfg.login_url = DEFAULT_LOGIN_URL or start_url
        cfg.username = DEFAULT_LOGIN_USERNAME
        cfg.password = DEFAULT_LOGIN_PASSWORD

    return cfg


async def _recognize_captcha_via_api(page: Page, config: LoginConfig) -> str:
    """
    方式一（推荐）：拦截验证码 API 响应 → 提取 base64 → POST OCR 服务

    与 Cypress 项目的 getCaptchaAndRecognize() 完全对应：
      cy.intercept('GET', '/api/captcha') → 获取 response.body.data.image
      → POST http://10.10.10.200:9898/ocr/b64/text (body = base64.split(',')[1])
    """
    captcha_base64 = None

    async def capture_captcha(route):
        """路由拦截回调：放行请求并捕获响应中的 base64 验证码"""
        nonlocal captcha_base64
        try:
            response = await route.fetch()
            body = await response.json()
            # 适配常见响应格式
            captcha_base64 = (
                body.get("data", {}).get("image")
                or body.get("data", {}).get("captchaImage")
                or body.get("data", {}).get("img")
                or body.get("image", "")
            )
            await route.fulfill(response=response)
        except Exception as e:
            logger.warning(f"Captcha route intercept error: {e}")
            await route.continue_()

    # 注册路由拦截
    route_pattern = f"**{config.captcha_api}*"
    await page.route(route_pattern, capture_captcha)

    # 如果有验证码图片元素，点击它触发刷新
    if config.captcha_img_selector:
        try:
            captcha_el = page.locator(config.captcha_img_selector)
            if await captcha_el.is_visible():
                await captcha_el.click()
                await page.wait_for_timeout(800)
        except Exception:
            pass

    # 等待拦截生效
    if not captcha_base64:
        await page.wait_for_timeout(1500)

    # 取消路由拦截
    try:
        await page.unroute(route_pattern)
    except Exception:
        pass

    if not captcha_base64:
        logger.warning("Failed to capture captcha base64 via API intercept")
        return ""

    # 去掉 data:image/...;base64, 前缀（与 Cypress 中 split(',')[1] 一致）
    if "," in captcha_base64:
        captcha_base64 = captcha_base64.split(",")[1]

    # POST 到 OCR 服务识别
    return await _call_ocr_service(config.ocr_service_url, captcha_base64)


# 常见验证码图片元素选择器（用于自动探测）
_CAPTCHA_IMG_CANDIDATES = [
    'img[src*="captcha"]',
    'img[src*="verify"]',
    'img[src*="code"]',
    'img[src*="kaptcha"]',
    'img[src*="vcode"]',
    'img.captcha',
    'img.code-img',
    '.captcha-img img',
    '.verify-code img',
    'canvas.captcha',
]


async def _recognize_captcha_via_screenshot(page: Page, config: LoginConfig) -> str:
    """
    方式二：截取验证码图片元素 → base64 → OCR 识别。
    支持配置的选择器和自动探测。
    """
    captcha_el = None

    # 先尝试配置的选择器
    if config.captcha_img_selector:
        try:
            el = page.locator(config.captcha_img_selector)
            if await el.count() > 0 and await el.first.is_visible():
                captcha_el = el.first
                logger.info(f"[auto_login] Found captcha image via config: {config.captcha_img_selector}")
        except Exception:
            pass

    # 自动探测常见验证码图片元素
    if not captcha_el:
        for selector in _CAPTCHA_IMG_CANDIDATES:
            try:
                el = page.locator(selector)
                if await el.count() > 0 and await el.first.is_visible():
                    captcha_el = el.first
                    logger.info(f"[auto_login] Found captcha image via auto-detect: {selector}")
                    break
            except Exception:
                continue

    if not captcha_el:
        logger.info("[auto_login] No captcha image found on page (may not require captcha)")
        return ""

    try:
        img_bytes = await captcha_el.screenshot()
        b64_str = base64.b64encode(img_bytes).decode("utf-8")
        return await _call_ocr_service(config.ocr_service_url, b64_str)
    except Exception as e:
        logger.error(f"Screenshot captcha recognition failed: {e}")
        return ""


async def _call_ocr_service(ocr_url: str, base64_data: str) -> str:
    """
    识别验证码：优先使用内置 ddddocr，不可用时回退到远程 OCR 服务。

    Args:
        ocr_url: 远程 OCR 服务地址（可为空）
        base64_data: 验证码图片的 base64 编码
    """
    if not base64_data:
        return ""

    # 策略 1：内置 ddddocr
    local_ocr = _get_local_ocr()
    if local_ocr:
        try:
            img_bytes = base64.b64decode(base64_data)
            result = local_ocr.classification(img_bytes).strip()
            logger.info(f"[OCR] Local ddddocr recognized: '{result}'")
            if result:
                return result
        except Exception as e:
            logger.warning(f"[OCR] Local ddddocr failed: {e}")

    # 策略 2：远程 OCR 服务（回退）
    if ocr_url:
        try:
            async with httpx.AsyncClient(timeout=10) as client:
                resp = await client.post(ocr_url, content=base64_data)
                result = resp.text.strip()
                logger.info(f"[OCR] Remote service recognized: '{result}'")
                return result
        except Exception as e:
            logger.warning(f"[OCR] Remote service failed ({ocr_url}): {e}")

    return ""


async def _check_login_success(page: Page, config: LoginConfig, login_url: str) -> bool:
    """
    检查是否登录成功，支持五种互补的判定方式：
      1. URL 正则匹配（配置）
      2. 特定元素出现（配置）
      3. URL 已变化且不再是登录页
      4. localStorage/cookie 变化检测（SPA 友好）
      5. 通用已登录元素探测（如退出按钮、用户头像等）
    """
    current_url = page.url
    logger.info(f"[auto_login] Checking login success, current URL: {current_url}")

    # 方式 A：URL 模式匹配
    if config.success_url_pattern:
        if re.search(config.success_url_pattern, current_url):
            logger.info(f"Login success: URL matched pattern '{config.success_url_pattern}'")
            return True

    # 方式 B：特定元素出现
    if config.success_element:
        try:
            await page.locator(config.success_element).wait_for(
                state="visible", timeout=5000
            )
            logger.info(f"Login success: element '{config.success_element}' appeared")
            return True
        except Exception:
            pass

    # 方式 C：URL 已变化且不再是登录页
    if login_url and current_url != login_url:
        if "/login" not in current_url.lower():
            logger.info(f"Login success: URL changed from login page to '{current_url}'")
            return True

    # 方式 D：检测 localStorage 中是否出现 token/auth 相关条目
    try:
        token_count = await page.evaluate("""() => {
            let count = 0;
            for (let i = 0; i < localStorage.length; i++) {
                const key = localStorage.key(i).toLowerCase();
                if (key.includes('token') || key.includes('auth') ||
                    key.includes('jwt') || key.includes('user') ||
                    key.includes('session') || key.includes('access')) {
                    count++;
                }
            }
            return count;
        }""")
        if token_count > 0:
            logger.info(f"Login success: found {token_count} auth-related localStorage entries")
            return True
    except Exception as e:
        logger.debug(f"localStorage check failed: {e}")

    # 方式 E：探测通用已登录页面元素
    _logged_in_indicators = [
        'button:has-text("退出")',
        'button:has-text("注销")',
        'a:has-text("退出")',
        'a:has-text("注销")',
        '[class*="logout"]',
        '[class*="user-avatar"]',
        '[class*="user-info"]',
        '.el-dropdown:has-text("管理员")',
        '.el-avatar',
    ]
    for selector in _logged_in_indicators:
        try:
            el = page.locator(selector)
            if await el.count() > 0 and await el.first.is_visible():
                logger.info(f"Login success: logged-in indicator found: {selector}")
                return True
        except Exception:
            continue

    # 方式 F：登录表单消失（登录页已被替换）
    try:
        login_form_visible = False
        for sel in ['#login-username', 'input[type="password"]']:
            el = page.locator(sel)
            if await el.count() > 0 and await el.first.is_visible():
                login_form_visible = True
                break
        if not login_form_visible:
            logger.info("Login success: login form no longer visible")
            return True
    except Exception:
        pass

    return False


async def _find_element(page: Page, configured: str, candidates: list[str], label: str):
    """
    智能查找页面元素：优先使用配置的选择器，找不到则依次尝试候选列表。

    Args:
        page: Playwright Page
        configured: 用户/默认配置的选择器
        candidates: 候选选择器列表（按优先级排列）
        label: 元素标签（用于日志）

    Returns:
        找到的 Locator，全部失败时返回 None
    """
    # 先尝试配置的选择器
    if configured:
        try:
            el = page.locator(configured)
            if await el.count() > 0 and await el.first.is_visible():
                logger.info(f"[auto_login] Found {label} via configured selector: {configured}")
                return el.first
        except Exception:
            pass

    # 依次尝试候选选择器
    for selector in candidates:
        try:
            el = page.locator(selector)
            if await el.count() > 0 and await el.first.is_visible():
                logger.info(f"[auto_login] Found {label} via fallback: {selector}")
                return el.first
        except Exception:
            continue

    logger.warning(f"[auto_login] Could not find {label} with any selector")
    return None


# 常见登录页元素的候选选择器
_USERNAME_CANDIDATES = [
    'input[name="username"]',
    'input[name="account"]',
    'input[name="loginName"]',
    'input[name="mobile"]',
    'input[type="text"][placeholder*="用户"]',
    'input[type="text"][placeholder*="账号"]',
    'input[type="text"][placeholder*="手机"]',
    'input[type="tel"]',
    'input[type="text"]:first-of-type',
]

_PASSWORD_CANDIDATES = [
    'input[name="password"]',
    'input[type="password"]',
]

_SUBMIT_CANDIDATES = [
    'button[type="submit"]',
    'button:has-text("登录")',
    'button:has-text("登 录")',
    'button:has-text("Login")',
    'button:has-text("Sign in")',
    'input[type="submit"]',
    '.el-button--primary',
    '.ant-btn-primary',
    'button.btn-primary',
]

_CAPTCHA_INPUT_CANDIDATES = [
    'input[name="captcha"]',
    'input[name="verifyCode"]',
    'input[name="code"]',
    'input[placeholder*="验证码"]',
    'input[placeholder*="captcha" i]',
]


async def auto_login(start_url: str, config: LoginConfig) -> dict:
    """
    执行自动登录，成功后保存 storageState。
    支持智能选择器查找：当配置的选择器无法命中时，自动回退到常见模式。

    Args:
        start_url: 录制的起始 URL（用于确定 auth_state 文件路径）
        config: 登录配置

    Returns:
        {"success": bool, "auth_state_path": str, "error": str, "attempts": int}
    """
    auth_state_path = get_auth_state_path(start_url)
    login_url = config.login_url or start_url

    if not config.username:
        return {
            "success": False,
            "auth_state_path": "",
            "error": "未配置登录用户名（请在 auth_config 或环境变量中设置）",
            "attempts": 0,
        }

    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True)
        context = await browser.new_context(ignore_https_errors=True)
        page = await context.new_page()

        try:
            # 1. 导航到登录页
            logger.info(f"[auto_login] Navigating to: {login_url}")
            await page.goto(login_url, wait_until="networkidle", timeout=30000)
            await page.wait_for_timeout(1000)

            # 2. 智能查找并填写用户名密码
            logger.info("[auto_login] Finding and filling credentials")
            username_el = await _find_element(
                page, config.username_selector, _USERNAME_CANDIDATES, "username"
            )
            password_el = await _find_element(
                page, config.password_selector, _PASSWORD_CANDIDATES, "password"
            )

            if not username_el:
                return {
                    "success": False,
                    "auth_state_path": "",
                    "error": f"无法找到用户名输入框（尝试了 {config.username_selector} 及 {len(_USERNAME_CANDIDATES)} 个候选选择器）",
                    "attempts": 0,
                }
            if not password_el:
                return {
                    "success": False,
                    "auth_state_path": "",
                    "error": f"无法找到密码输入框（尝试了 {config.password_selector} 及 {len(_PASSWORD_CANDIDATES)} 个候选选择器）",
                    "attempts": 0,
                }

            await username_el.fill("")
            await username_el.fill(config.username)
            await password_el.fill("")
            await password_el.fill(config.password)

            # 3. 识别验证码并提交（带重试）
            for attempt in range(1, config.max_retries + 1):
                logger.info(f"[auto_login] Attempt {attempt}/{config.max_retries}")

                # 识别验证码
                captcha_code = ""
                if config.captcha_api:
                    captcha_code = await _recognize_captcha_via_api(page, config)
                if not captcha_code:
                    captcha_code = await _recognize_captcha_via_screenshot(page, config)

                # 填写验证码
                if captcha_code and config.captcha_input_selector:
                    logger.info(f"[auto_login] Filling captcha: '{captcha_code}'")
                    captcha_el = await _find_element(
                        page, config.captcha_input_selector,
                        _CAPTCHA_INPUT_CANDIDATES, "captcha_input"
                    )
                    if captcha_el:
                        await captcha_el.fill("")
                        await captcha_el.fill(captcha_code)

                # 智能查找并点击登录按钮
                submit_el = await _find_element(
                    page, config.submit_selector, _SUBMIT_CANDIDATES, "submit"
                )
                if not submit_el:
                    return {
                        "success": False,
                        "auth_state_path": "",
                        "error": f"无法找到登录按钮（尝试了 {config.submit_selector} 及 {len(_SUBMIT_CANDIDATES)} 个候选选择器）",
                        "attempts": attempt,
                    }
                await submit_el.click()

                # 等待 3 秒（与 Cypress cy.wait(3000) 一致）
                await page.wait_for_timeout(3000)

                # 判断是否登录成功
                if await _check_login_success(page, config, login_url):
                    # 再等待 2 秒让页面稳定（与 Cypress 一致）
                    await page.wait_for_timeout(2000)
                    # 保存 storageState
                    await context.storage_state(path=auth_state_path)
                    logger.info(
                        f"[auto_login] Success on attempt {attempt}, "
                        f"auth_state saved to {auth_state_path}"
                    )
                    return {
                        "success": True,
                        "auth_state_path": auth_state_path,
                        "error": "",
                        "attempts": attempt,
                    }

                # 登录失败 — 记录错误信息
                if config.error_element:
                    try:
                        error_el = page.locator(config.error_element)
                        if await error_el.is_visible():
                            error_text = await error_el.text_content()
                            logger.info(f"[auto_login] Error message: '{error_text}'")
                    except Exception:
                        pass

                # 保存调试截图（仅首次失败时）
                if attempt == 1:
                    try:
                        debug_path = f"./auth_states/_debug_login_{page.url.split('//')[1].split('/')[0]}.png"
                        await page.screenshot(path=debug_path, full_page=True)
                        logger.info(f"[auto_login] Debug screenshot saved: {debug_path}")
                    except Exception as e:
                        logger.debug(f"[auto_login] Failed to save debug screenshot: {e}")

                logger.warning(
                    f"[auto_login] Attempt {attempt} failed, "
                    f"{'retrying...' if attempt < config.max_retries else 'giving up'}"
                )

                # 重试前等待 2 秒（与 Cypress cy.wait(2000) 一致）
                if attempt < config.max_retries:
                    await page.wait_for_timeout(2000)

            # 所有尝试用完
            return {
                "success": False,
                "auth_state_path": "",
                "error": f"登录失败，验证码识别重试 {config.max_retries} 次后仍未成功",
                "attempts": config.max_retries,
            }

        except Exception as e:
            logger.error(f"[auto_login] Unexpected error: {e}", exc_info=True)
            return {
                "success": False,
                "auth_state_path": "",
                "error": str(e),
                "attempts": 0,
            }
        finally:
            await browser.close()
