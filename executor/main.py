"""
main.py — Python 执行服务 FastAPI 入口

提供三个核心接口：
  POST /execute/generate   — AI 直生模式：browser-use 探索 + LLM 生成 TypeScript 脚本
  POST /execute/refactor   — 录制增强模式：原始录制稿 + AI 重构为标准 TypeScript 脚本
  POST /execute/validate   — 执行 Playwright TypeScript 回放验证
"""
import asyncio
import logging
import os
import tempfile
import uuid
from typing import Optional, Dict, Any

import uvicorn
from fastapi import FastAPI, Request

from fastapi.responses import JSONResponse
from pydantic import BaseModel

from config import SERVICE_PORT, EXECUTOR_API_KEY, CODEGEN_SESSION_TIMEOUT_SEC
from browser_runner import run_browser_exploration
from script_generator import generate_playwright_script, refactor_recorded_script, parse_step_model
from validation_runner import run_validation

# 日志配置
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(name)s] %(levelname)s: %(message)s",
)
logger = logging.getLogger("executor")

app = FastAPI(title="TestPilot Executor Service", version="2.0.0")

# ── 统一 CORS + API Key 鉴权中间件 ──
CORS_HEADERS = {
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Methods": "*",
    "Access-Control-Allow-Headers": "*",
    "Access-Control-Max-Age": "3600",
}


@app.middleware("http")
async def cors_and_auth(request: Request, call_next):
    # CORS 预检请求 (OPTIONS) 直接返回，不走任何后续逻辑
    if request.method == "OPTIONS":
        return JSONResponse(content={}, status_code=200, headers=CORS_HEADERS)

    # 免鉴权路径
    skip_paths = ["/health", "/docs", "/openapi.json"]
    need_auth = True
    if any(request.url.path.startswith(p) for p in skip_paths):
        need_auth = False
    elif request.url.path.startswith("/recording/"):
        need_auth = False
    elif request.url.path.startswith("/codegen/") and request.method == "GET":
        need_auth = False

    # API Key 鉴权
    if need_auth and EXECUTOR_API_KEY:
        api_key = request.headers.get("X-API-Key", "")
        if api_key != EXECUTOR_API_KEY:
            logger.warning(f"Unauthorized request from {request.client.host}: {request.url.path}")
            return JSONResponse(status_code=401, content={"detail": "Unauthorized: Invalid API Key"})

    response = await call_next(request)
    # 为所有响应添加 CORS 头
    for key, value in CORS_HEADERS.items():
        response.headers[key] = value
    return response


# ── Codegen 会话管理 ──
# 存储活跃的 codegen 进程信息
_codegen_sessions: Dict[str, Dict[str, Any]] = {}


# ── Codegen 超时清理后台任务 ──
async def _cleanup_stale_sessions():
    """每 60 秒检查并清理超时的 codegen 会话"""
    import time
    while True:
        await asyncio.sleep(60)
        now = time.time()
        stale = []
        for sid, info in _codegen_sessions.items():
            created = info.get("created_at", now)
            if now - created > CODEGEN_SESSION_TIMEOUT_SEC:
                stale.append(sid)
        for sid in stale:
            info = _codegen_sessions.pop(sid, {})
            proc = info.get("process")
            if proc and proc.returncode is None:
                try:
                    proc.terminate()
                    logger.info(f"Terminated stale codegen session {sid}")
                except Exception:
                    pass
            logger.info(f"Cleaned up stale codegen session {sid}")


@app.on_event("startup")
async def startup_event():
    asyncio.create_task(_cleanup_stale_sessions())


# ── 请求/响应模型 ──

class GenerateRequest(BaseModel):
    task_id: int
    scenario_desc: str
    start_url: str
    account_ref: Optional[str] = None
    callback_url: Optional[str] = None


class RefactorRequest(BaseModel):
    task_id: int
    recording_id: int
    raw_script: str
    step_model_json: Optional[dict] = None
    scenario_desc: Optional[str] = ""
    start_url: Optional[str] = ""
    account_ref: Optional[str] = None


class ValidateRequest(BaseModel):
    task_id: int
    script_version_id: int
    script_content: str
    start_url: str
    callback_url: Optional[str] = None


# ── 接口 ──

@app.get("/health")
async def health():
    return {"status": "ok", "service": "executor", "version": "2.0.0"}


@app.post("/execute/generate")
async def execute_generate(req: GenerateRequest):
    """AI 直生模式：browser-use 探索 + LLM 生成 Playwright TypeScript 脚本"""
    logger.info(f"Received generate request: task_id={req.task_id}")

    # 1. 执行 browser-use 探索
    exploration_result = await run_browser_exploration(
        task_id=req.task_id,
        scenario_desc=req.scenario_desc,
        start_url=req.start_url,
        account_ref=req.account_ref,
    )

    if not exploration_result["success"]:
        return {
            "success": False,
            "script_content": "",
            "traces": exploration_result.get("traces", []),
            "screenshots": exploration_result.get("screenshots", []),
            "error_message": exploration_result.get("error_message", "探索执行失败"),
        }

    # 2. 基于轨迹生成 Playwright TypeScript 脚本
    logger.info(f"Generating TypeScript script for task {req.task_id}, traces count: {len(exploration_result['traces'])}")

    gen_result = generate_playwright_script(
        scenario_desc=req.scenario_desc,
        start_url=req.start_url,
        traces=exploration_result["traces"],
        account_ref=req.account_ref,
    )

    logger.info(f"Script generated for task {req.task_id}, length: {len(gen_result.get('script_content', ''))}")

    return {
        "success": True,
        "script_content": gen_result["script_content"],
        "risk_hints": gen_result.get("risk_hints", []),
        "assertion_suggestions": gen_result.get("assertion_suggestions", []),
        "generation_summary": gen_result.get("generation_summary", ""),
        "traces": exploration_result["traces"],
        "screenshots": exploration_result.get("screenshots", []),
        "error_message": "",
    }


@app.post("/execute/refactor")
async def execute_refactor(req: RefactorRequest):
    """录制增强模式：将原始录制稿 AI 重构为标准化 TypeScript 脚本"""
    logger.info(f"Received refactor request: task_id={req.task_id}, recording_id={req.recording_id}")

    # 先解析步骤模型（纯正则，不依赖 LLM）
    step_model = parse_step_model(req.raw_script)
    logger.info(f"Parsed step model: {step_model['total_steps']} steps")

    gen_result = refactor_recorded_script(
        scenario_desc=req.scenario_desc or "",
        start_url=req.start_url or "",
        raw_script=req.raw_script,
        step_model_json=req.step_model_json or step_model,
        account_ref=req.account_ref,
    )

    if not gen_result.get("script_content"):
        return {
            "success": False,
            "script_content": "",
            "traces": [],
            "screenshots": [],
            "error_message": gen_result.get("generation_summary", "AI 重构失败"),
        }

    logger.info(f"Refactored script for task {req.task_id}, length: {len(gen_result['script_content'])}")

    return {
        "success": True,
        "script_content": gen_result["script_content"],
        "risk_hints": gen_result.get("risk_hints", []),
        "assertion_suggestions": gen_result.get("assertion_suggestions", []),
        "generation_summary": gen_result.get("generation_summary", ""),
        "step_model_json": step_model,
        "traces": [],
        "screenshots": [],
        "error_message": "",
    }


@app.post("/execute/validate")
async def execute_validate(req: ValidateRequest):
    """执行回放验证：使用 npx playwright test 运行 TypeScript 脚本"""
    logger.info(
        f"Received validate request: task_id={req.task_id}, "
        f"script_version_id={req.script_version_id}"
    )

    # 在线程池中执行同步的 subprocess 操作
    loop = asyncio.get_event_loop()
    result = await loop.run_in_executor(
        None,
        run_validation,
        req.task_id,
        req.script_version_id,
        req.script_content,
        req.start_url,
    )

    logger.info(
        f"Validation done for task {req.task_id}: "
        f"success={result['success']}, "
        f"steps={result['passed_step_count']}/{result['total_step_count']}"
    )

    return result


# ── Playwright Codegen 录制管理 ──

class CodegenRequest(BaseModel):
    task_id: int
    start_url: str


async def _run_codegen(session_id: str, start_url: str, output_file: str):
    """后台运行 playwright codegen，当用户关闭浏览器后收回脚本"""
    try:
        logger.info(f"[codegen:{session_id}] Launching playwright codegen -> {start_url}")

        # Windows 下 npx 是 .cmd 脚本，必须通过 shell 执行
        cmd = f'npx -y playwright codegen --target playwright-test --output "{output_file}" "{start_url}"'
        logger.info(f"[codegen:{session_id}] Command: {cmd}")

        proc = await asyncio.create_subprocess_shell(
            cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )

        _codegen_sessions[session_id]["pid"] = proc.pid
        _codegen_sessions[session_id]["status"] = "recording"
        logger.info(f"[codegen:{session_id}] Process started, PID={proc.pid}")

        # 等待用户关闭浏览器 (进程退出)
        await proc.wait()

        logger.info(f"[codegen:{session_id}] Process exited, reading output file")

        script_content = ""
        if os.path.exists(output_file):
            with open(output_file, "r", encoding="utf-8") as f:
                script_content = f.read()
            os.remove(output_file)  # 清理临时文件

        _codegen_sessions[session_id]["status"] = "completed"
        _codegen_sessions[session_id]["script_content"] = script_content
        logger.info(f"[codegen:{session_id}] Script captured, length={len(script_content)}")

    except Exception as e:
        logger.error(f"[codegen:{session_id}] Error: {e}")
        _codegen_sessions[session_id]["status"] = "error"
        _codegen_sessions[session_id]["error"] = str(e)


@app.post("/recording/codegen")
async def start_codegen(req: CodegenRequest):
    """启动 Playwright Codegen 录制：弹出浏览器供用户操作"""
    session_id = str(uuid.uuid4())[:8]
    output_file = os.path.join(tempfile.gettempdir(), f"codegen_{session_id}.ts")

    _codegen_sessions[session_id] = {
        "task_id": req.task_id,
        "start_url": req.start_url,
        "status": "starting",
        "script_content": "",
        "output_file": output_file,
        "pid": None,
        "error": "",
        "created_at": __import__("time").time(),
    }

    # 在后台运行，不阻塞请求
    asyncio.create_task(_run_codegen(session_id, req.start_url, output_file))

    logger.info(f"[codegen] New session {session_id} for task {req.task_id}")
    return {"session_id": session_id, "status": "starting"}


@app.get("/recording/codegen/{session_id}")
async def poll_codegen(session_id: str):
    """轮询 Codegen 录制状态：recording → completed (返回脚本)"""
    session = _codegen_sessions.get(session_id)
    if not session:
        return {"status": "not_found", "script_content": "", "error": "session not found"}

    return {
        "status": session["status"],
        "script_content": session.get("script_content", ""),
        "error": session.get("error", ""),
    }


if __name__ == "__main__":
    logger.info(f"Starting executor service on port {SERVICE_PORT}")
    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=SERVICE_PORT,
        reload=False,
        log_level="info",
    )
