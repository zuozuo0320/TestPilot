"""
main.py — Python 执行服务 FastAPI 入口

提供三个核心接口：
  POST /execute/generate   — AI 直生模式：browser-use 探索 + LLM 生成 TypeScript 脚本
  POST /execute/refactor   — 录制增强模式：原始录制稿 + AI 重构为标准 TypeScript 脚本
  POST /execute/validate   — 执行 Playwright TypeScript 回放验证
"""
import asyncio
import json
import logging
import os
import tempfile
import time
import uuid
from typing import Optional, Dict, Any

import uvicorn
from fastapi import FastAPI, Request
from fastapi.staticfiles import StaticFiles

from fastapi.responses import JSONResponse
from pydantic import BaseModel

from config import SERVICE_PORT, EXECUTOR_API_KEY, CODEGEN_SESSION_TIMEOUT_SEC, SCREENSHOT_DIR, SCRIPT_OUTPUT_DIR
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

# 挂载截图静态文件服务
app.mount("/screenshots", StaticFiles(directory=SCREENSHOT_DIR), name="screenshots")

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
    elif request.url.path.startswith("/screenshots/"):
        need_auth = False
    elif request.url.path.startswith("/codegen/") and request.method == "GET":
        need_auth = False
    elif request.url.path.startswith("/auth/"):
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

# ── 录制脚本持久化（解决页面刷新/关闭后脚本丢失问题）──
_PENDING_SCRIPTS_DIR = os.path.join(SCRIPT_OUTPUT_DIR, "pending")
os.makedirs(_PENDING_SCRIPTS_DIR, exist_ok=True)


def _save_pending_script(task_id: int, script_content: str, session_id: str = ""):
    """将录制完成但未提交的脚本持久化到磁盘"""
    if not script_content:
        return
    pending_file = os.path.join(_PENDING_SCRIPTS_DIR, f"task_{task_id}.json")
    data = {
        "task_id": task_id,
        "session_id": session_id,
        "script_content": script_content,
        "captured_at": time.strftime("%Y-%m-%d %H:%M:%S"),
        "timestamp": time.time(),
    }
    with open(pending_file, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
    logger.info(
        f"[pending] Saved pending script for task {task_id}, "
        f"length={len(script_content)}"
    )


def _load_pending_script(task_id: int) -> Optional[dict]:
    """加载指定任务的待提交脚本"""
    pending_file = os.path.join(_PENDING_SCRIPTS_DIR, f"task_{task_id}.json")
    if not os.path.exists(pending_file):
        return None
    try:
        with open(pending_file, "r", encoding="utf-8") as f:
            return json.load(f)
    except Exception as e:
        logger.warning(f"[pending] Failed to load pending script for task {task_id}: {e}")
        return None


def _clear_pending_script(task_id: int):
    """清除指定任务的待提交脚本（提交后调用）"""
    pending_file = os.path.join(_PENDING_SCRIPTS_DIR, f"task_{task_id}.json")
    if os.path.exists(pending_file):
        os.remove(pending_file)
        logger.info(f"[pending] Cleared pending script for task {task_id}")


# ── Codegen 超时清理后台任务 ──
def _read_text_file_with_fallback(path: str) -> str:
    """使用 UTF-8 兜底读取日志文件，避免 Windows 默认编码吞掉关键信息。"""
    if not os.path.exists(path):
        return ""
    with open(path, "r", encoding="utf-8", errors="replace") as file:
        return file.read()


def _tail_text_for_log(text: str, limit: int = 600) -> str:
    """裁剪日志尾部，确保错误信息既可读又不会过长。"""
    normalized = (text or "").strip()
    if len(normalized) <= limit:
        return normalized
    return normalized[-limit:]


def _build_codegen_failure_message(
    returncode: int,
    elapsed_seconds: float,
    output_file: str,
    stdout_file: str,
    stderr_file: str,
) -> str:
    """统一拼装录制失败原因，方便前端直接展示和排查。"""
    stderr_text = _tail_text_for_log(_read_text_file_with_fallback(stderr_file))
    stdout_text = _tail_text_for_log(_read_text_file_with_fallback(stdout_file))

    reasons = [f"录制进程已退出（退出码 {returncode}）"]
    if elapsed_seconds < 3:
        reasons.append("录制窗口可能未正常拉起或刚启动就退出")

    if stderr_text:
        reasons.append(f"stderr: {stderr_text}")
    elif stdout_text:
        reasons.append(f"stdout: {stdout_text}")
    elif os.path.exists(output_file):
        reasons.append("检测到了输出文件，但文件内容为空")
    else:
        reasons.append("未检测到 Playwright codegen 输出文件")

    return "；".join(reasons)


def _npx_command() -> str:
    """按平台返回可直接执行的 npx 命令。"""
    return "npx.cmd" if os.name == "nt" else "npx"


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
    project_scope: Optional[dict] = None  # V1 多项目工程化：ProjectScope 信息


class ValidateRequest(BaseModel):
    task_id: int
    script_version_id: int
    script_content: str
    start_url: str
    callback_url: Optional[str] = None
    project_scope: Optional[dict] = None      # V1 多项目工程化：ProjectScope 信息
    spec_relative_path: Optional[str] = None  # V1：项目内 spec 相对路径


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
        project_scope=req.project_scope,  # V1 透传
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

    # 将 step_model 的每个步骤转换为 traces，供后端持久化并在前端展示
    traces = _step_model_to_traces(step_model)
    logger.info(f"Converted step_model to {len(traces)} traces for task {req.task_id}")

    return {
        "success": True,
        "script_content": gen_result["script_content"],
        "risk_hints": gen_result.get("risk_hints", []),
        "assertion_suggestions": gen_result.get("assertion_suggestions", []),
        "generation_summary": gen_result.get("generation_summary", ""),
        "step_model_json": step_model,
        "traces": traces,
        "screenshots": [],
        "error_message": "",
        # V1 多文件结果（仅当 project_scope 存在时有值）
        "spec_file": gen_result.get("spec_file"),
        "page_creates": gen_result.get("page_creates", []),
        "page_updates": gen_result.get("page_updates", []),
        "registry_updates": gen_result.get("registry_updates"),
        "manual_review_items": gen_result.get("manual_review_items", []),
        # V1 元数据（Go 端 applyV1VersionFields 依赖这些字段）
        "project_key_snapshot": gen_result.get("project_key_snapshot"),
        "workspace_root_snapshot": gen_result.get("workspace_root_snapshot"),
        "registry_snapshot": gen_result.get("registry_snapshot"),
        "base_fixture_hash": gen_result.get("base_fixture_hash"),
        "version_status": gen_result.get("version_status"),
        "files_created": gen_result.get("files_created", []),
        "files_updated": gen_result.get("files_updated", []),
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
        req.project_scope,          # V1 透传
        req.spec_relative_path,     # V1 透传
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
    auth_config: Optional[dict] = None  # 登录配置 JSON（可选）


async def _run_codegen(
    session_id: str,
    start_url: str,
    output_file: str,
    auth_config: Optional[dict] = None,
):
    """后台运行 playwright codegen，集成认证状态管理"""
    try:
        from auth_manager import has_valid_auth_state, get_auth_state_path
        from login_handler import auto_login, build_login_config

        # 统一改成绝对路径，避免服务从不同工作目录启动时找不到 auth_state。
        auth_state_path = os.path.abspath(get_auth_state_path(start_url))

        # ── 认证状态检查与自动登录 ──
        if not has_valid_auth_state(start_url):
            logger.info(f"[codegen:{session_id}] No valid auth state, attempting auto login")
            _codegen_sessions[session_id]["status"] = "logging_in"

            login_cfg = build_login_config(start_url, auth_config)

            if login_cfg.username and login_cfg.login_url:
                result = await auto_login(start_url, login_cfg)
                if not result["success"]:
                    logger.error(
                        f"[codegen:{session_id}] Auto login failed: {result['error']}"
                    )
                    # 自动登录失败不阻塞录制，继续以无登录态方式启动
                    logger.info(
                        f"[codegen:{session_id}] Proceeding without auth state"
                    )
                else:
                    logger.info(
                        f"[codegen:{session_id}] Auto login succeeded "
                        f"(attempts={result['attempts']})"
                    )
            else:
                logger.info(
                    f"[codegen:{session_id}] No login config provided, "
                    f"proceeding without auto login"
                )
        else:
            logger.info(f"[codegen:{session_id}] Valid auth state found, will load it")

        # ── 构建 codegen 命令 ──
        cmd = [
            _npx_command(),
            "-y",
            "playwright",
            "codegen",
            "--ignore-https-errors",
            "--target",
            "playwright-test",
        ]

        # 如果存在有效的 auth_state，加载它
        if os.path.exists(auth_state_path):
            cmd.extend(["--load-storage", auth_state_path])
            logger.info(f"[codegen:{session_id}] Loading auth state: {auth_state_path}")

        # 每次录制后都保存最新的 auth_state
        cmd.extend(["--save-storage", auth_state_path])
        cmd.extend(["--output", output_file, start_url])

        import subprocess as _sp
        logger.info(f"[codegen:{session_id}] Command: {_sp.list2cmdline(cmd)}")

        # 使用 subprocess.Popen 替代 asyncio.create_subprocess_shell，
        # 避免 Windows ProactorEventLoop 在创建管道时出现 [WinError 5] 拒绝访问。
        import subprocess as _sp
        proc = _sp.Popen(
            cmd,
            shell=True,
            stdout=_sp.DEVNULL,
            stderr=_sp.DEVNULL,
        )

        _codegen_sessions[session_id]["pid"] = proc.pid
        _codegen_sessions[session_id]["status"] = "recording"
        logger.info(f"[codegen:{session_id}] Process started, PID={proc.pid}")

        # 在线程池中等待进程退出，避免阻塞 event loop
        loop = asyncio.get_event_loop()
        await loop.run_in_executor(None, proc.wait)

        logger.info(f"[codegen:{session_id}] Process exited, reading output file")

        script_content = ""
        if os.path.exists(output_file):
            try:
                # 首先尝试 UTF-8 (标准且最通用)
                with open(output_file, "r", encoding="utf-8") as f:
                    script_content = f.read()
            except UnicodeDecodeError:
                try:
                    # 其次尝试 GBK (Windows 中文环境常见编码)
                    with open(output_file, "r", encoding="gbk") as f:
                        script_content = f.read()
                except UnicodeDecodeError:
                    # 最后尝试带错误容忍的 utf-8 或原生 bytes
                    with open(output_file, "rb") as f:
                        raw_data = f.read()
                        script_content = raw_data.decode("utf-8", errors="replace")
            
            os.remove(output_file)  # 清理临时文件

        _codegen_sessions[session_id]["status"] = "completed"
        _codegen_sessions[session_id]["script_content"] = script_content
        logger.info(
            f"[codegen:{session_id}] Script captured, length={len(script_content)}"
        )

        # 持久化到磁盘，防止页面刷新/关闭后丢失
        task_id = _codegen_sessions[session_id].get("task_id")
        if task_id and script_content:
            _save_pending_script(task_id, script_content, session_id)

    except Exception as e:
        logger.error(f"[codegen:{session_id}] Error: {e}", exc_info=True)
        _codegen_sessions[session_id]["status"] = "error"
        _codegen_sessions[session_id]["error"] = str(e)


async def _run_codegen_v2(
    session_id: str,
    start_url: str,
    output_file: str,
    auth_config: Optional[dict] = None,
):
    """后台运行 playwright codegen，并把录制异常收敛成可诊断的错误信息。"""
    try:
        from auth_manager import has_valid_auth_state, get_auth_state_path
        from login_handler import auto_login, build_login_config
        import subprocess as _sp

        # 统一改成绝对路径，避免服务从不同工作目录启动时找不到 auth_state。
        auth_state_path = os.path.abspath(get_auth_state_path(start_url))

        # 认证态优先复用，失效时再尝试自动登录，尽量减少录制前的人工操作。
        if not has_valid_auth_state(start_url):
            logger.info(f"[codegen:{session_id}] No valid auth state, attempting auto login")
            _codegen_sessions[session_id]["status"] = "logging_in"

            login_cfg = build_login_config(start_url, auth_config)
            if login_cfg.username and login_cfg.login_url:
                result = await auto_login(start_url, login_cfg)
                if not result["success"]:
                    logger.error(
                        f"[codegen:{session_id}] Auto login failed: {result['error']}"
                    )
                    logger.info(f"[codegen:{session_id}] Proceeding without auth state")
                else:
                    logger.info(
                        f"[codegen:{session_id}] Auto login succeeded "
                        f"(attempts={result['attempts']})"
                    )
            else:
                logger.info(
                    f"[codegen:{session_id}] No login config provided, "
                    f"proceeding without auto login"
                )
        else:
            logger.info(f"[codegen:{session_id}] Valid auth state found, will load it")

        cmd = [
            _npx_command(),
            "-y",
            "playwright",
            "codegen",
            "--ignore-https-errors",
            "--target",
            "playwright-test",
        ]

        if os.path.exists(auth_state_path):
            cmd.extend(["--load-storage", auth_state_path])
            logger.info(f"[codegen:{session_id}] Loading auth state: {auth_state_path}")

        cmd.extend(["--save-storage", auth_state_path])
        cmd.extend(["--output", output_file, start_url])
        logger.info(f"[codegen:{session_id}] Command: {_sp.list2cmdline(cmd)}")

        stdout_file = os.path.join(tempfile.gettempdir(), f"codegen_{session_id}.stdout.log")
        stderr_file = os.path.join(tempfile.gettempdir(), f"codegen_{session_id}.stderr.log")
        _codegen_sessions[session_id]["stdout_file"] = stdout_file
        _codegen_sessions[session_id]["stderr_file"] = stderr_file

        # 录制链路优先保证窗口能稳定拉起，因此改为参数数组直启并保留诊断日志。
        with open(stdout_file, "wb") as stdout_handle, open(stderr_file, "wb") as stderr_handle:
            proc = _sp.Popen(
                cmd,
                cwd=os.path.dirname(os.path.abspath(__file__)),
                shell=False,
                stdout=stdout_handle,
                stderr=stderr_handle,
            )
            _codegen_sessions[session_id]["pid"] = proc.pid
            _codegen_sessions[session_id]["status"] = "recording"
            logger.info(f"[codegen:{session_id}] Process started, PID={proc.pid}")

            # 在线程池中等待退出，避免阻塞 FastAPI 事件循环。
            loop = asyncio.get_event_loop()
            started_at = time.time()
            returncode = await loop.run_in_executor(None, proc.wait)
            elapsed_seconds = time.time() - started_at

        logger.info(f"[codegen:{session_id}] Process exited, reading output file")

        script_content = ""
        if os.path.exists(output_file):
            try:
                with open(output_file, "r", encoding="utf-8") as f:
                    script_content = f.read()
            except UnicodeDecodeError:
                try:
                    with open(output_file, "r", encoding="gbk") as f:
                        script_content = f.read()
                except UnicodeDecodeError:
                    with open(output_file, "rb") as f:
                        raw_data = f.read()
                        script_content = raw_data.decode("utf-8", errors="replace")
            os.remove(output_file)

        # 空脚本本质上属于录制失败，不能再返回 completed 误导前端。
        if not script_content.strip():
            failure_message = _build_codegen_failure_message(
                returncode=returncode,
                elapsed_seconds=elapsed_seconds,
                output_file=output_file,
                stdout_file=stdout_file,
                stderr_file=stderr_file,
            )
            logger.error(f"[codegen:{session_id}] Empty script: {failure_message}")
            _codegen_sessions[session_id]["status"] = "error"
            _codegen_sessions[session_id]["error"] = failure_message
            return

        _codegen_sessions[session_id]["status"] = "completed"
        _codegen_sessions[session_id]["script_content"] = script_content
        logger.info(
            f"[codegen:{session_id}] Script captured, length={len(script_content)}"
        )

        # 落盘保留最近一次待提交脚本，避免页面刷新后丢失录制内容。
        task_id = _codegen_sessions[session_id].get("task_id")
        if task_id and script_content:
            _save_pending_script(task_id, script_content, session_id)

    except Exception as e:
        logger.error(f"[codegen:{session_id}] Error: {e}", exc_info=True)
        _codegen_sessions[session_id]["status"] = "error"
        _codegen_sessions[session_id]["error"] = str(e)


@app.post("/recording/codegen")
async def start_codegen(req: CodegenRequest):
    """启动 Playwright Codegen 录制：弹出浏览器供用户操作（集成认证状态管理）"""
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

    # 在后台运行，不阻塞请求（传递 auth_config）
    asyncio.create_task(
        _run_codegen_v2(session_id, req.start_url, output_file, req.auth_config)
    )

    logger.info(f"[codegen] New session {session_id} for task {req.task_id}")
    return {"session_id": session_id, "status": "starting"}


@app.get("/recording/codegen/{session_id}")
async def poll_codegen(session_id: str):
    """轮询 Codegen 录制状态：recording → completed (返回脚本)"""
    session = _codegen_sessions.get(session_id)
    if not session:
        return {"status": "not_found", "script_content": "", "error": "session not found"}

    return {
        "status": session["status"],  # starting / logging_in / recording / completed / error
        "script_content": session.get("script_content", ""),
        "error": session.get("error", ""),
    }


@app.get("/recording/codegen/task/{task_id}/pending")
async def get_pending_script(task_id: int):
    """获取指定任务的待提交录制脚本（页面刷新后恢复用）"""
    # 先检查内存中是否有活跃的 completed session
    for sid, info in _codegen_sessions.items():
        if info.get("task_id") == task_id and info.get("status") == "completed":
            script = info.get("script_content", "")
            if script:
                return {
                    "found": True,
                    "script_content": script,
                    "session_id": sid,
                    "source": "memory",
                }

    # 再检查磁盘持久化文件
    pending = _load_pending_script(task_id)
    if pending and pending.get("script_content"):
        return {
            "found": True,
            "script_content": pending["script_content"],
            "session_id": pending.get("session_id", ""),
            "source": "disk",
            "captured_at": pending.get("captured_at", ""),
        }

    return {"found": False, "script_content": "", "session_id": "", "source": ""}


@app.delete("/recording/codegen/task/{task_id}/pending")
async def clear_pending_script_api(task_id: int):
    """清除指定任务的待提交脚本（提交成功后由前端调用）"""
    _clear_pending_script(task_id)
    # 同时清理内存中该任务的 completed session
    stale_sids = [
        sid for sid, info in _codegen_sessions.items()
        if info.get("task_id") == task_id and info.get("status") == "completed"
    ]
    for sid in stale_sids:
        _codegen_sessions.pop(sid, None)
    return {"message": f"Pending script cleared for task {task_id}"}


# ── 认证状态管理 API ──

@app.get("/auth/status")
async def auth_status(start_url: str):
    """查询指定 URL 的认证状态"""
    from auth_manager import get_auth_state_info
    info = get_auth_state_info(start_url)
    return info


@app.post("/auth/invalidate")
async def auth_invalidate(start_url: str):
    """手动清除指定 URL 的认证状态（强制下次重新登录）"""
    from auth_manager import invalidate_auth_state
    invalidate_auth_state(start_url)
    return {"message": f"Auth state invalidated for {start_url}"}



def _step_model_to_traces(step_model: dict) -> list:
    """
    将 parse_step_model 返回的 step_model 转换为 traces 格式。
    录制增强模式下，Python 端从 step_model.steps 生成 traces 返回给 Go 后端，
    Go 后端持久化到 ai_script_traces 表，前端操作步骤时间线直接使用 DB 数据。
    """
    import re as _re
    steps = step_model.get("steps") or []
    traces = []
    for step in steps:
        step_no = step.get("stepNo") or step.get("step_no") or (len(traces) + 1)
        action_type = (step.get("actionType") or step.get("action_type") or "CUSTOM").upper()
        locator = step.get("locator") or ""
        input_value = step.get("inputValue") or step.get("input_value") or ""
        page_url = step.get("pageUrl") or step.get("page_url") or ""

        # 从 Playwright 定位器中提取人类可读的元素名
        def _readable_from_locator(loc):
            m = _re.search(r'getBy(?:Role|Text|Label|Placeholder|TestId|AltText|Title)\([\'"]([^\'"]{1,40})', loc)
            return m.group(1) if m else (loc[:40] if loc else "")

        if action_type == "NAVIGATE":
            summary = f"导航到 {page_url}" if page_url else f"步骤 {step_no}"
        elif action_type == "CLICK":
            readable = _readable_from_locator(locator) or f"元素 {step_no}"
            summary = f"点击「{readable}」"
        elif action_type == "INPUT":
            field = _readable_from_locator(locator) or "输入框"
            val = (input_value[:20] + ("..." if len(input_value) > 20 else "")) if input_value else ""
            summary = f"在「{field}」输入 {val}" if val else f"在「{field}」输入"
        elif action_type == "KEY_PRESS":
            summary = f"按键 {input_value}" if input_value else f"步骤 {step_no}"
        elif action_type == "SELECT":
            summary = f"选择「{input_value}」" if input_value else f"步骤 {step_no}"
        elif action_type == "WAIT":
            summary = "等待页面响应"
        elif action_type == "ASSERT":
            summary = "断言验证"
        else:
            summary = step.get("description") or step.get("targetSummary") or f"步骤 {step_no}"

        traces.append({
            "trace_no": step_no,
            "action_type": action_type,
            "page_url": page_url,
            "target_summary": summary,
            "locator_used": locator[:500] if locator else "",
            "input_value_masked": _mask_input(input_value),
            "action_result": "success",
            "error_message": "",
            "screenshot_url": "",
            "occurred_at": f"00:{step_no:02d}.00",
        })
    return traces


def _mask_input(value: str) -> str:
    """对输入值脱敏处理"""
    if not value:
        return ""
    if len(value) <= 4:
        return "****"
    return value[:2] + "***" + value[-2:]


# ── 用例质量 AI 分析 ──

class TestCaseAnalyzeRequest(BaseModel):
    title: str = ""
    precondition: str = ""
    postcondition: str = ""
    steps: str = ""  # "操作描述 | 预期结果\n..." 格式


_ANALYZE_SYSTEM_PROMPT = """你是一位资深测试工程师，擅长评审测试用例的质量。
请基于用户提供的测试用例内容，从以下三个维度进行分析，并给出 JSON 格式的结果：

1. coverage（覆盖率分析）：检查测试步骤是否完整覆盖了前置条件和后置条件描述的场景，是否有遗漏的测试路径。给出 score (0-100) 和 issues 列表。
2. boundary（边界值检查）：检查是否考虑了边界情况、异常路径、极端输入、空值处理等。给出 score (0-100) 和 issues 列表。
3. quality（综合质量评分）：综合评估用例标题清晰度、步骤描述完整性、预期结果明确性。给出 score (0-100) 和 suggestions 列表。

严格按以下 JSON 格式返回，不要包含任何其他文字：
{
  "coverage": { "score": 85, "issues": ["缺少XXX场景的覆盖"] },
  "boundary": { "score": 70, "issues": ["未考虑空值输入", "缺少超长文本测试"] },
  "quality": { "score": 80, "suggestions": ["建议细化步骤3的预期结果", "标题可以更具体"] },
  "summary": "一句话总结该用例的整体质量状况"
}"""


@app.post("/api/testcase/analyze")
async def analyze_testcase(req: TestCaseAnalyzeRequest):
    """AI 分析测试用例质量：覆盖率、边界值、综合评分"""
    from openai import OpenAI
    from config import OPENAI_API_KEY, OPENAI_BASE_URL, OPENAI_MODEL

    if not OPENAI_API_KEY:
        return JSONResponse(status_code=500, content={"error": "LLM API key not configured"})

    user_content = f"""请分析以下测试用例：

【标题】{req.title or '(无)'}

【前置条件】
{req.precondition or '(无)'}

【后置条件】
{req.postcondition or '(无)'}

【测试步骤】(格式：操作描述 | 预期结果)
{req.steps or '(无)'}
"""

    try:
        kwargs = {"api_key": OPENAI_API_KEY}
        if OPENAI_BASE_URL:
            kwargs["base_url"] = OPENAI_BASE_URL
        client = OpenAI(**kwargs)

        response = client.chat.completions.create(
            model=OPENAI_MODEL,
            messages=[
                {"role": "system", "content": _ANALYZE_SYSTEM_PROMPT},
                {"role": "user", "content": user_content},
            ],
            temperature=0.3,
            max_tokens=1500,
        )

        raw = response.choices[0].message.content.strip()
        # 尝试提取 JSON
        import re
        json_match = re.search(r'\{[\s\S]*\}', raw)
        if json_match:
            result = json.loads(json_match.group())
        else:
            result = json.loads(raw)

        return {"status": "ok", "result": result}

    except json.JSONDecodeError:
        logger.warning(f"LLM returned non-JSON: {raw[:200]}")
        return {"status": "ok", "result": {
            "coverage": {"score": 0, "issues": ["AI 返回格式异常，请重试"]},
            "boundary": {"score": 0, "issues": []},
            "quality": {"score": 0, "suggestions": []},
            "summary": raw[:200],
        }}
    except Exception as e:
        logger.error(f"Testcase analyze failed: {e}")
        return JSONResponse(status_code=500, content={"error": str(e)})


if __name__ == "__main__":
    logger.info(f"Starting executor service on port {SERVICE_PORT}")
    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=SERVICE_PORT,
        reload=False,
        log_level="info",
    )
