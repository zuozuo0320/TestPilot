"""
planner.py — LLM Planner 语义匹配：基于录制上下文与候选资产清单生成编排计划

输入由 Go 后端的 PlannerContextBuilder 构建（已脱敏），输出严格遵循 14.2 章计划 JSON Schema，
最终的防幻觉引用校验与置信度加权由 Go 后端 PlanValidator 完成。
"""
import json
import logging
import re
from typing import Any, Dict

import config as cfg
from script_generator import _build_client, _completion_params

logger = logging.getLogger(__name__)

PLANNER_SYSTEM_PROMPT = """你是一位资深的测试编排规划专家。
你的职责是基于录制任务上下文与候选资产清单，规划可复用的测试编排步骤。

## 强约束
- 只能引用候选资产清单中给出的固定场景（FLOW_CALL）和断言（ASSERTION），禁止编造 id/version_id/key。
- 无法用候选资产覆盖的片段，输出 ATOMIC_ACTION 或 AI_GENERATED 占位步骤并在 reason 说明。
- inputs 中的参数表达式只允许：${env.<白名单KEY>}、${steps.<step_key>.outputs.<name>}、${variables.<name>}、${literal.<value>}。
- 敏感值（密码、token、密钥等）必须使用 ${env.*} 表达式，禁止明文。
- 步骤数量不得超过 max_steps。

## 输出格式
只输出一个 JSON 对象，不要输出任何解释文字或 Markdown 代码块，字段如下：
{
  "plan_id": "字符串",
  "summary": "计划摘要",
  "confidence": 0.0 到 1.0 之间的数值,
  "steps": [
    {
      "type": "FLOW_CALL | ASSERTION | ATOMIC_ACTION | AI_GENERATED",
      "flow_id": 数字（仅 FLOW_CALL，取候选 id）,
      "flow_version_id": 数字（仅 FLOW_CALL，取候选 version_id）,
      "flow_key": "字符串（仅 FLOW_CALL）",
      "assertion_id": 数字（仅 ASSERTION，取候选 id）,
      "assertion_key": "字符串（仅 ASSERTION）",
      "confidence": 0.0 到 1.0 之间的数值,
      "reason": "推荐理由",
      "inputs": { "参数名": "值或表达式" }
    }
  ],
  "warnings": ["警告信息"]
}
"""


def _strip_json_fences(text: str) -> str:
    text = text.strip()
    match = re.match(r"^```(?:json)?\s*(.*?)\s*```$", text, re.DOTALL)
    if match:
        return match.group(1)
    return text


def generate_plan(context: Dict[str, Any]) -> Dict[str, Any]:
    """调用 LLM 生成编排计划，返回 {success, plan, model, usage, error_message}。"""
    if not cfg.OPENAI_API_KEY:
        return {
            "success": False,
            "plan": None,
            "model": cfg.OPENAI_MODEL,
            "usage": {},
            "error_message": "OPENAI_API_KEY 未配置",
        }
    user_prompt = json.dumps(
        {
            "task_name": context.get("task_name", ""),
            "scenario_desc": context.get("scenario_desc", ""),
            "start_url_path": context.get("start_url_path", ""),
            "recording_steps": context.get("recording_steps", []),
            "candidates": context.get("candidates", []),
            "env_keys": context.get("env_keys", []),
            "max_steps": context.get("max_steps", 20),
            "expression_doc": context.get("expression_doc", ""),
        },
        ensure_ascii=False,
    )
    usage: Dict[str, int] = {}
    try:
        client = _build_client()
        response = client.chat.completions.create(
            model=cfg.OPENAI_MODEL,
            messages=[
                {"role": "system", "content": PLANNER_SYSTEM_PROMPT},
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
        plan = json.loads(_strip_json_fences(content))
        return {
            "success": True,
            "plan": plan,
            "model": cfg.OPENAI_MODEL,
            "usage": usage,
            "error_message": "",
        }
    except json.JSONDecodeError as exc:
        logger.warning(f"Planner output is not valid JSON: {exc}")
        return {
            "success": False,
            "plan": None,
            "model": cfg.OPENAI_MODEL,
            "usage": usage,
            "error_message": f"LLM 输出不是合法 JSON: {exc}",
        }
    except Exception as exc:  # noqa: BLE001
        logger.error(f"Planner LLM call failed: {exc}")
        return {
            "success": False,
            "plan": None,
            "model": cfg.OPENAI_MODEL,
            "usage": usage,
            "error_message": str(exc),
        }
