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
import logging
import re
from dataclasses import dataclass, field
from typing import Optional

import httpx
from playwright.async_api import async_playwright, Page, BrowserContext

from auth_manager import get_auth_state_path
from config import OCR_SERVICE_URL, DEFAULT_LOGIN_URL, DEFAULT_LOGIN_USERNAME, DEFAULT_LOGIN_PASSWORD

logger = logging.getLogger(__name__)

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


async def _recognize_captcha_via_screenshot(page: Page, config: LoginConfig) -> str:
    """
    方式二（备用）：直接截取验证码图片元素 → base64 → POST OCR 服务

    适用于验证码不通过 API 返回 base64 的场景
    """
    import base64

    if not config.captcha_img_selector:
        return ""

    try:
        captcha_el = page.locator(config.captcha_img_selector)
        if not await captcha_el.is_visible():
            logger.warning("Captcha image element not visible")
            return ""

        img_bytes = await captcha_el.screenshot()
        b64_str = base64.b64encode(img_bytes).decode("utf-8")
        return await _call_ocr_service(config.ocr_service_url, b64_str)
    except Exception as e:
        logger.error(f"Screenshot captcha recognition failed: {e}")
        return ""


async def _call_ocr_service(ocr_url: str, base64_data: str) -> str:
    """调用 OCR HTTP 服务识别验证码"""
    if not ocr_url or not base64_data:
        return ""

    try:
        async with httpx.AsyncClient(timeout=10) as client:
            resp = await client.post(ocr_url, content=base64_data)
            result = resp.text.strip()
            logger.info(f"OCR recognized captcha: '{result}'")
            return result
    except Exception as e:
        logger.error(f"OCR service call failed ({ocr_url}): {e}")
        return ""


async def _check_login_success(page: Page, config: LoginConfig, login_url: str) -> bool:
    """
    检查是否登录成功，支持三种互补的判定方式：
      1. URL 正则匹配
      2. 特定元素出现
      3. URL 已不再是登录页（兜底）
    """
    current_url = page.url

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

    # 方式 C：URL 已变化且不再是登录页（兜底）
    if login_url and current_url != login_url:
        if "/login" not in current_url.lower():
            logger.info(f"Login success: URL changed from login page to '{current_url}'")
            return True

    return False


async def auto_login(start_url: str, config: LoginConfig) -> dict:
    """
    执行自动登录，成功后保存 storageState

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

            # 2. 填写用户名密码
            logger.info("[auto_login] Filling credentials")
            username_el = page.locator(config.username_selector)
            password_el = page.locator(config.password_selector)

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
                if not captcha_code and config.captcha_img_selector:
                    captcha_code = await _recognize_captcha_via_screenshot(page, config)

                # 填写验证码
                if captcha_code and config.captcha_input_selector:
                    logger.info(f"[auto_login] Filling captcha: '{captcha_code}'")
                    captcha_el = page.locator(config.captcha_input_selector)
                    await captcha_el.fill("")
                    await captcha_el.fill(captcha_code)

                # 点击登录按钮
                await page.locator(config.submit_selector).click()

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
