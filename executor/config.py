"""
执行服务配置 — 从环境变量读取
"""
import os

from dotenv import load_dotenv

load_dotenv()

# OpenAI 配置
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY", "")
OPENAI_MODEL = os.getenv("OPENAI_MODEL", "gpt-4.1")
OPENAI_BASE_URL = os.getenv("OPENAI_BASE_URL", "")

# browser-use 配置
BROWSER_HEADLESS = os.getenv("BROWSER_HEADLESS", "true").lower() == "true"
BROWSER_TIMEOUT_MS = int(os.getenv("BROWSER_TIMEOUT_MS", "60000"))

# 截图存储目录
SCREENSHOT_DIR = os.getenv("SCREENSHOT_DIR", "./screenshots")
os.makedirs(SCREENSHOT_DIR, exist_ok=True)

# 生成的脚本存储目录
SCRIPT_OUTPUT_DIR = os.getenv("SCRIPT_OUTPUT_DIR", "./generated_scripts")
os.makedirs(SCRIPT_OUTPUT_DIR, exist_ok=True)

# 服务端口
SERVICE_PORT = int(os.getenv("EXECUTOR_PORT", "8100"))

# 鉴权 API Key（Go 后端调用时需在 Header 中携带）
EXECUTOR_API_KEY = os.getenv("EXECUTOR_API_KEY", "")

# Codegen 会话超时（秒），超时后自动清理
CODEGEN_SESSION_TIMEOUT_SEC = int(os.getenv("CODEGEN_SESSION_TIMEOUT_SEC", "600"))

# ── 认证状态管理 ──
AUTH_STATE_DIR = os.getenv("AUTH_STATE_DIR", "./auth_states")
os.makedirs(AUTH_STATE_DIR, exist_ok=True)

# 认证状态最大有效期（小时）
AUTH_STATE_MAX_AGE_HOURS = int(os.getenv("AUTH_STATE_MAX_AGE_HOURS", "24"))

# ── OCR 验证码识别服务（复用已部署的 ddddocr HTTP 服务）──
OCR_SERVICE_URL = os.getenv("OCR_SERVICE_URL", "http://10.10.10.200:9898/ocr/b64/text")

# ── 默认登录配置（可选，适用于固定被测系统）──
DEFAULT_LOGIN_URL = os.getenv("DEFAULT_LOGIN_URL", "")
DEFAULT_LOGIN_USERNAME = os.getenv("DEFAULT_LOGIN_USERNAME", "")
DEFAULT_LOGIN_PASSWORD = os.getenv("DEFAULT_LOGIN_PASSWORD", "")
