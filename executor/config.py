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
