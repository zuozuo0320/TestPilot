"""
requirement_gen.py — 需求智生 Executor 端

提供两个核心能力：
  1. 文档解析：将 docx/pdf/md/txt 转为纯文本，回调 Go 后端
  2. 用例生成：读取需求文本 + Skill Prompt → LLM → 结构化用例 JSON，回调 Go 后端

端点：
  POST /requirement-gen/parse-doc    异步文档解析
  POST /requirement-gen/generate     异步 LLM 生成用例
"""
import asyncio
import json
import logging
import os
import re
import time
import traceback
from typing import Optional

import httpx
from openai import OpenAI
from pydantic import BaseModel

from config import OPENAI_API_KEY, OPENAI_BASE_URL, OPENAI_MODEL

logger = logging.getLogger("requirement_gen")

# Go 后端内部回调地址（Executor → Go）
BACKEND_INTERNAL_URL = os.getenv("BACKEND_INTERNAL_URL", "http://127.0.0.1:8080")
EXECUTOR_API_KEY = os.getenv("EXECUTOR_API_KEY", "tp-executor-secret-key-change-in-prod")


# ─── 请求/响应模型 ───

class ParseDocRequest(BaseModel):
    """文档解析请求"""
    doc_id: int
    file_path: str        # 相对于 Go 后端工作目录
    file_format: str      # docx / pdf / md / txt
    callback_url: Optional[str] = None  # 回调 URL（可选覆盖）


class SkillCandidate(BaseModel):
    """Skill Router 候选 Skill 信息"""
    skill_id: int
    skill_key: str
    name: str
    scope: str
    description: str


class SkillRouterRequest(BaseModel):
    """Skill 路由请求：分析需求文本，推荐适用的 Skill"""
    requirement_text: str
    skills: list[SkillCandidate]
    max_skills: int = 6  # 最多推荐几个 Skill


class SkillRouterResult(BaseModel):
    """单个推荐 Skill"""
    skill_id: int
    skill_key: str
    reason: str  # 推荐理由


class GenerateRequest(BaseModel):
    """用例生成请求"""
    task_id: int
    project_id: int
    requirement_text: str       # 需求原文
    skill_name: str
    prompt_template: str        # Skill 的 prompt 模板
    output_schema: str          # 输出格式标识
    max_cases: int = 30
    default_level: str = "P2"
    extra_prompt: str = ""
    model_override: Optional[str] = None
    callback_url: Optional[str] = None


# ─── 文档解析 ───

def _extract_text_from_txt(path: str) -> str:
    """纯文本文件直接读取"""
    with open(path, "r", encoding="utf-8", errors="replace") as f:
        return f.read()


def _extract_text_from_md(path: str) -> str:
    """Markdown 直接当纯文本返回"""
    return _extract_text_from_txt(path)


def _extract_text_from_docx(path: str) -> str:
    """从 docx 提取文本（需要 python-docx）"""
    try:
        from docx import Document
        doc = Document(path)
        paragraphs = [p.text for p in doc.paragraphs if p.text.strip()]
        return "\n".join(paragraphs)
    except ImportError:
        logger.warning("python-docx not installed, falling back to raw read")
        return _extract_text_from_txt(path)


def _extract_text_from_pdf(path: str) -> str:
    """从 pdf 提取文本（需要 PyPDF2 或 pdfplumber）"""
    try:
        import pdfplumber
        text_parts = []
        with pdfplumber.open(path) as pdf:
            for page in pdf.pages:
                page_text = page.extract_text()
                if page_text:
                    text_parts.append(page_text)
        return "\n".join(text_parts)
    except ImportError:
        logger.warning("pdfplumber not installed, trying PyPDF2")
        try:
            from PyPDF2 import PdfReader
            reader = PdfReader(path)
            text_parts = []
            for page in reader.pages:
                t = page.extract_text()
                if t:
                    text_parts.append(t)
            return "\n".join(text_parts)
        except ImportError:
            logger.error("No PDF library available (pdfplumber or PyPDF2)")
            raise RuntimeError("未安装 PDF 解析库，请安装 pdfplumber 或 PyPDF2")


EXTRACTORS = {
    "txt": _extract_text_from_txt,
    "text": _extract_text_from_txt,
    "md": _extract_text_from_md,
    "docx": _extract_text_from_docx,
    "pdf": _extract_text_from_pdf,
}


async def parse_doc_async(req: ParseDocRequest):
    """异步执行文档解析并回调 Go 后端"""
    callback_base = req.callback_url or BACKEND_INTERNAL_URL
    callback_url = f"{callback_base}/internal/requirement-docs/{req.doc_id}/parse-callback"

    headers = {"Authorization": f"Bearer {EXECUTOR_API_KEY}", "Content-Type": "application/json"}

    # 通知开始解析
    try:
        async with httpx.AsyncClient(timeout=10.0) as client:
            # Go 后端的 MarkParsingStarted 通过直接 CAS 推进，此处直接解析
            pass
    except Exception:
        pass

    try:
        extractor = EXTRACTORS.get(req.file_format.lower())
        if not extractor:
            raise ValueError(f"不支持的文件格式: {req.file_format}")

        content = await asyncio.get_event_loop().run_in_executor(
            None, extractor, req.file_path
        )

        if not content or not content.strip():
            raise ValueError("文档内容为空，无法解析出有效文本")

        # 回调成功
        payload = {"status": "parsed", "content": content}
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(callback_url, json=payload, headers=headers)
            logger.info(f"Parse callback success: doc_id={req.doc_id}, status={resp.status_code}, chars={len(content)}")

    except Exception as e:
        logger.error(f"Parse doc failed: doc_id={req.doc_id}, error={e}")
        error_payload = {"status": "parse_failed", "error": str(e)}
        try:
            async with httpx.AsyncClient(timeout=10.0) as client:
                await client.post(callback_url, json=error_payload, headers=headers)
        except Exception as cb_err:
            logger.error(f"Parse callback failed: {cb_err}")


# ─── LLM 用例生成 ───

_DEFAULT_SYSTEM_PROMPT = """你是一名资深测试工程师，擅长从软件需求文档中提取高质量的测试用例。

请根据用户提供的需求文本，生成结构化的测试用例列表。

输出要求：
1. 返回一个 JSON 数组，每个元素代表一条测试用例
2. 每条用例包含以下字段：
   - title: 用例标题（简明概括，20-60字）
   - level: 优先级（P0/P1/P2/P3）
   - precondition: 前置条件（可选）
   - steps: 测试步骤，格式为 JSON 数组，每个元素 {"action": "操作描述", "expected": "预期结果"}
   - postcondition: 后置条件（可选）
   - remark: 备注（可选）
   - tags_suggested: 建议标签，逗号分隔字符串（可选）
   - ai_confidence: AI 置信度 0.0-1.0

请只返回 JSON，不要包含其他说明文字。"""


def _build_user_prompt(req: GenerateRequest) -> str:
    """构建用户 Prompt：将 Skill 模板中的占位符替换为实际值"""
    prompt = req.prompt_template

    # 替换标准占位符
    prompt = prompt.replace("{{requirement_text}}", req.requirement_text)
    prompt = prompt.replace("{{max_cases}}", str(req.max_cases))
    prompt = prompt.replace("{{default_level}}", req.default_level)

    # 追加额外提示
    if req.extra_prompt:
        prompt += f"\n\n用户补充要求：{req.extra_prompt}"

    return prompt


def _openai_params(model: str, temperature: float = 0.3, max_tokens: int = 8000) -> dict:
    """构建 OpenAI 调用参数，兼容 o 系列推理模型"""
    params: dict = {}
    model_lower = model.lower()
    if model_lower.startswith("o1") or model_lower.startswith("o3") or model_lower.startswith("o4"):
        # 推理模型使用 max_completion_tokens
        params["max_completion_tokens"] = max_tokens
    else:
        params["temperature"] = temperature
        params["max_tokens"] = max_tokens
    return params


def _parse_llm_response(raw: str) -> list:
    """从 LLM 原始响应中提取 JSON 数组"""
    # 尝试直接解析
    try:
        result = json.loads(raw)
        if isinstance(result, list):
            return result
        if isinstance(result, dict) and "cases" in result:
            return result["cases"]
        if isinstance(result, dict) and "test_cases" in result:
            return result["test_cases"]
    except json.JSONDecodeError:
        pass

    # 尝试提取 ```json ... ``` 代码块
    code_block = re.search(r'```(?:json)?\s*([\s\S]*?)```', raw)
    if code_block:
        try:
            result = json.loads(code_block.group(1))
            if isinstance(result, list):
                return result
            if isinstance(result, dict):
                for key in ("cases", "test_cases", "data", "items"):
                    if key in result and isinstance(result[key], list):
                        return result[key]
        except json.JSONDecodeError:
            pass

    # 尝试匹配最外层数组
    arr_match = re.search(r'\[[\s\S]*\]', raw)
    if arr_match:
        try:
            return json.loads(arr_match.group())
        except json.JSONDecodeError:
            pass

    raise ValueError(f"无法从 LLM 响应中解析出 JSON 数组: {raw[:200]}")


# ─── Skill 智能路由 ───

_SKILL_ROUTER_SYSTEM_PROMPT = """你是一名资深测试架构师，擅长根据需求特征判断应该使用哪些测试策略。

你的任务：分析用户提供的需求文本，从候选 Skill 列表中选出最适合的子集。

判断原则：
1. 每个 Skill 有独立的测试视角，只选与需求内容直接相关的
2. "通用功能测试" 几乎总是适用的，除非需求明确只涉及非功能性测试
3. 如果需求涉及数值输入、长度限制、金额计算、日期范围 → 选 boundary_value
4. 如果需求涉及状态流转（如订单状态、审批流程、任务生命周期）→ 选 state_transition
5. 如果需求涉及认证、鉴权、敏感数据、加密 → 选 security_testcase
6. 如果需求涉及多端、多浏览器、多语言 → 选 compatibility_testcase
7. 如果需求涉及高并发、响应时间、大数据量 → 选 performance_scenario
8. 如果需求涉及跨模块的完整用户旅程（如注册→购买→退款）→ 选 e2e_user_journey
9. 如果需求涉及异常处理、超时、重试、降级 → 选 exception_resilience
10. 如果需求涉及多个独立输入参数的组合（如商品规格、筛选条件）→ 选 pairwise_combination
11. 如果需求涉及角色权限、操作授权 → 选 rbac_permission
12. 如果需求涉及数据增删改查（CRUD）操作 → 选 data_integrity_crud

输出要求：
- 返回一个 JSON 数组，每个元素包含 skill_id、skill_key、reason（推荐理由，中文，一句话）
- 按重要性从高到低排列
- 请只返回 JSON，不要包含其他说明文字。"""


def route_skills(req: SkillRouterRequest) -> list[dict]:
    """同步调用 LLM 分析需求文本，返回推荐的 Skill 列表"""
    # 构建候选列表描述
    candidates_text = "\n".join(
        f"- skill_id={s.skill_id}, skill_key={s.skill_key}, name={s.name}, scope={s.scope}, description={s.description}"
        for s in req.skills
    )

    user_prompt = f"""## 需求文本\n\n{req.requirement_text[:4000]}\n\n## 候选 Skill 列表\n\n{candidates_text}\n\n请从以上候选 Skill 中选出最多 {req.max_skills} 个最适合该需求的 Skill，返回 JSON 数组。"""

    kwargs = {"api_key": OPENAI_API_KEY}
    if OPENAI_BASE_URL:
        kwargs["base_url"] = OPENAI_BASE_URL

    model = OPENAI_MODEL
    client = OpenAI(**kwargs)

    logger.info(f"Skill router starting: model={model}, candidates={len(req.skills)}")

    response = client.chat.completions.create(
        model=model,
        messages=[
            {"role": "system", "content": _SKILL_ROUTER_SYSTEM_PROMPT},
            {"role": "user", "content": user_prompt},
        ],
        **_openai_params(model, temperature=0.2, max_tokens=2000),
    )

    raw_content = response.choices[0].message.content.strip()
    usage = response.usage
    logger.info(
        f"Skill router done: prompt_tokens={usage.prompt_tokens if usage else 0}, "
        f"completion_tokens={usage.completion_tokens if usage else 0}"
    )

    # 解析 JSON
    results = _parse_llm_response(raw_content)

    # 校验并过滤：只保留候选列表中存在的 skill_id
    valid_ids = {s.skill_id for s in req.skills}
    filtered = []
    for item in results[:req.max_skills]:
        sid = item.get("skill_id")
        if sid and sid in valid_ids:
            filtered.append({
                "skill_id": sid,
                "skill_key": item.get("skill_key", ""),
                "reason": item.get("reason", ""),
            })
    return filtered


async def generate_cases_async(req: GenerateRequest):
    """异步执行 LLM 生成用例并回调 Go 后端"""
    callback_base = req.callback_url or BACKEND_INTERNAL_URL
    callback_url = f"{callback_base}/internal/requirement-gen/tasks/{req.task_id}/callback"
    heartbeat_url = f"{callback_base}/internal/requirement-gen/tasks/{req.task_id}/heartbeat"

    headers = {"Authorization": f"Bearer {EXECUTOR_API_KEY}", "Content-Type": "application/json"}

    start_time = time.time()

    # 启动心跳任务
    heartbeat_running = True

    async def heartbeat_loop():
        while heartbeat_running:
            await asyncio.sleep(15)
            if not heartbeat_running:
                break
            try:
                async with httpx.AsyncClient(timeout=5.0) as client:
                    await client.post(heartbeat_url, headers=headers)
            except Exception:
                pass

    heartbeat_task = asyncio.create_task(heartbeat_loop())

    try:
        model = req.model_override or OPENAI_MODEL
        user_prompt = _build_user_prompt(req)

        kwargs = {"api_key": OPENAI_API_KEY}
        if OPENAI_BASE_URL:
            kwargs["base_url"] = OPENAI_BASE_URL

        client = OpenAI(**kwargs)

        logger.info(f"LLM generation starting: task_id={req.task_id}, model={model}, max_cases={req.max_cases}")

        response = client.chat.completions.create(
            model=model,
            messages=[
                {"role": "system", "content": _DEFAULT_SYSTEM_PROMPT},
                {"role": "user", "content": user_prompt},
            ],
            **_openai_params(model),
        )

        raw_content = response.choices[0].message.content.strip()
        usage = response.usage

        cases = _parse_llm_response(raw_content)
        duration_ms = int((time.time() - start_time) * 1000)

        # 构建回调产物
        results = []
        for i, case in enumerate(cases[:req.max_cases]):
            steps = case.get("steps", [])
            if isinstance(steps, list):
                steps_json = json.dumps(steps, ensure_ascii=False)
            else:
                steps_json = str(steps)

            results.append({
                "seq_no": i + 1,
                "title": case.get("title", f"用例 {i+1}"),
                "level": case.get("level", req.default_level),
                "precondition": case.get("precondition", ""),
                "steps": steps_json,
                "postcondition": case.get("postcondition", ""),
                "remark": case.get("remark", ""),
                "tags_suggested": case.get("tags_suggested", ""),
                "ai_confidence": float(case.get("ai_confidence", 0.8)),
                "raw_json": json.dumps(case, ensure_ascii=False),
            })

        # 成功回调
        payload = {
            "status": "success",
            "generated_count": len(results),
            "prompt_tokens": usage.prompt_tokens if usage else 0,
            "completion_tokens": usage.completion_tokens if usage else 0,
            "duration_ms": duration_ms,
            "results": results,
        }

        async with httpx.AsyncClient(timeout=30.0) as http_client:
            resp = await http_client.post(callback_url, json=payload, headers=headers)
            logger.info(
                f"Generate callback success: task_id={req.task_id}, "
                f"generated={len(results)}, duration={duration_ms}ms, "
                f"status={resp.status_code}"
            )

    except Exception as e:
        logger.error(f"Generate failed: task_id={req.task_id}, error={e}\n{traceback.format_exc()}")
        error_payload = {
            "status": "failed",
            "fail_reason": str(e),
        }
        try:
            async with httpx.AsyncClient(timeout=10.0) as http_client:
                await http_client.post(callback_url, json=error_payload, headers=headers)
        except Exception as cb_err:
            logger.error(f"Generate callback failed: {cb_err}")

    finally:
        heartbeat_running = False
        heartbeat_task.cancel()
        try:
            await heartbeat_task
        except asyncio.CancelledError:
            pass
