"""
repair.py — AI 修复建议生成：基于失败回归的脚本与失败日志生成修复 Diff 建议

输入由 Go 后端 AIRegressionService 构建，输出仅为 Diff 建议（summary/diff/patched_code），
按 14.3 约束建议必须经人工确认后才能应用，应用走后端手工补丁通道。
"""
import json
import logging
import re
from typing import Any, Dict

import config as cfg
from script_generator import _build_client, _completion_params

logger = logging.getLogger(__name__)

REPAIR_SYSTEM_PROMPT = """你是一位资深的 Playwright TypeScript 测试脚本修复专家。
你的职责是基于失败的回归场景脚本与失败日志，生成最小化的修复建议。

## 强约束
- 只修复与失败原因直接相关的代码，禁止大范围重构或无关改动。
- 保持脚本原有结构、命名与注释风格。
- 禁止引入新的外部依赖。
- 敏感值（密码、token、密钥等）必须保持 ${env.*} 或 process.env 形式，禁止明文。
- patched_code 必须是修复后的完整脚本内容，可直接替换原脚本。

## 输出格式
只输出一个 JSON 对象，不要输出任何解释文字或 Markdown 代码块，字段如下：
{
  "summary": "修复说明摘要",
  "diff": "统一 unified diff 格式的修改内容",
  "patched_code": "修复后的完整脚本内容"
}
"""


def _strip_json_fences(text: str) -> str:
    text = text.strip()
    match = re.match(r"^```(?:json)?\s*(.*?)\s*```$", text, re.DOTALL)
    if match:
        return match.group(1)
    return text


def generate_repair(context: Dict[str, Any]) -> Dict[str, Any]:
    """调用 LLM 生成修复建议，返回 {success, summary, diff, patched_code, model, usage, error_message}。"""
    if not cfg.OPENAI_API_KEY:
        return {
            "success": False,
            "summary": "",
            "diff": "",
            "patched_code": "",
            "model": cfg.OPENAI_MODEL,
            "usage": {},
            "error_message": "OPENAI_API_KEY 未配置",
        }
    user_prompt = json.dumps(
        {
            "composition_id": context.get("composition_id", 0),
            "scenario_name": context.get("scenario_name", ""),
            "script_content": context.get("script_content", ""),
            "failure_summary": context.get("failure_summary", ""),
            "failure_logs": context.get("failure_logs", ""),
        },
        ensure_ascii=False,
    )
    usage: Dict[str, int] = {}
    try:
        client = _build_client()
        response = client.chat.completions.create(
            model=cfg.OPENAI_MODEL,
            messages=[
                {"role": "system", "content": REPAIR_SYSTEM_PROMPT},
                {"role": "user", "content": user_prompt},
            ],
            **_completion_params(cfg.OPENAI_MODEL, 0.1, 8192),
        )
        content = response.choices[0].message.content or ""
        if response.usage:
            usage = {
                "prompt_tokens": response.usage.prompt_tokens or 0,
                "completion_tokens": response.usage.completion_tokens or 0,
                "total_tokens": response.usage.total_tokens or 0,
            }
        result = json.loads(_strip_json_fences(content))
        patched_code = result.get("patched_code", "") or ""
        if not patched_code.strip():
            return {
                "success": False,
                "summary": result.get("summary", "") or "",
                "diff": result.get("diff", "") or "",
                "patched_code": "",
                "model": cfg.OPENAI_MODEL,
                "usage": usage,
                "error_message": "LLM 输出缺少 patched_code",
            }
        return {
            "success": True,
            "summary": result.get("summary", "") or "",
            "diff": result.get("diff", "") or "",
            "patched_code": patched_code,
            "model": cfg.OPENAI_MODEL,
            "usage": usage,
            "error_message": "",
        }
    except json.JSONDecodeError as exc:
        logger.warning(f"Repair output is not valid JSON: {exc}")
        return {
            "success": False,
            "summary": "",
            "diff": "",
            "patched_code": "",
            "model": cfg.OPENAI_MODEL,
            "usage": usage,
            "error_message": f"LLM 输出不是合法 JSON: {exc}",
        }
    except Exception as exc:  # noqa: BLE001
        logger.error(f"Repair LLM call failed: {exc}")
        return {
            "success": False,
            "summary": "",
            "diff": "",
            "patched_code": "",
            "model": cfg.OPENAI_MODEL,
            "usage": usage,
            "error_message": str(exc),
        }
