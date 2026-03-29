"""
browser_runner.py — 封装 browser-use 调用，执行页面探索并采集操作轨迹
"""
import asyncio
import logging
import time
import traceback
from typing import Optional

from browser_use import Agent, ChatOpenAI

from config import (
    OPENAI_API_KEY,
    OPENAI_BASE_URL,
    OPENAI_MODEL,
    BROWSER_HEADLESS,
    BROWSER_TIMEOUT_MS,
    SCREENSHOT_DIR,
)

logger = logging.getLogger(__name__)


def _build_llm() -> ChatOpenAI:
    """构建 LLM 客户端（使用 browser-use 内置 ChatOpenAI）"""
    kwargs = {
        "model": OPENAI_MODEL,
        "api_key": OPENAI_API_KEY,
        "temperature": 0,
    }
    if OPENAI_BASE_URL:
        kwargs["base_url"] = OPENAI_BASE_URL
    return ChatOpenAI(**kwargs)


async def run_browser_exploration(
    task_id: int,
    scenario_desc: str,
    start_url: str,
    account_ref: Optional[str] = None,
) -> dict:
    """
    使用 browser-use 执行页面探索

    返回: {
        "success": bool,
        "traces": [轨迹列表],
        "screenshots": [截图列表],
        "error_message": str
    }
    """
    traces = []
    screenshots = []

    prompt = f"""
你是一位专业的 Web 自动化测试工程师。请执行以下测试场景：

**场景描述**: {scenario_desc}
**起始 URL**: {start_url}
"""
    if account_ref:
        prompt += f"\n**测试账号参考**: {account_ref}"

    prompt += """

请执行以上场景，仔细记录每一步操作。在执行过程中：
1. 详细记录每个操作步骤（点击、输入、导航等）
2. 注意页面的变化和响应
3. 完成场景后验证预期结果
"""

    try:
        llm = _build_llm()

        agent = Agent(
            task=prompt,
            llm=llm,
            use_vision=False,              # 不发送截图，大幅减少 Token
            max_actions_per_step=3,         # 每步最多 3 个动作，减少复杂度
            use_thinking=False,             # 关闭思考模式，减少 Token
            use_judge=False,                # 关闭评判，少一次 LLM 调用
            max_clickable_elements_length=15000,  # 限制 DOM 大小（默认 40000）
            llm_timeout=120,                # LLM 超时 120 秒
            message_compaction=True,        # 压缩历史消息
        )

        history = await agent.run(max_steps=15)

        # 从 agent 历史中提取结构化轨迹
        trace_no = 0
        for step in history.history:
            for action_result in step.result:
                trace_no += 1

                action_type = "CUSTOM"
                target_summary = ""
                locator_used = ""
                input_value = ""
                page_url = ""
                action_result_str = "success"
                error_msg = ""

                if action_result.extracted_content:
                    target_summary = action_result.extracted_content[:500]

                if action_result.error:
                    action_result_str = "error"
                    error_msg = str(action_result.error)[:500]

                # 尝试从 action 中提取更多信息
                if step.model_output and step.model_output.action:
                    for act in step.model_output.action:
                        act_dict = act.model_dump() if hasattr(act, "model_dump") else {}
                        if "go_to_url" in act_dict:
                            action_type = "NAVIGATE"
                            page_url = act_dict.get("go_to_url", {}).get("url", "")
                            target_summary = f"导航到: {page_url}"
                        elif "click_element" in act_dict:
                            action_type = "CLICK"
                            elem = act_dict.get("click_element", {})
                            locator_used = str(elem.get("index", ""))
                            target_summary = f"点击元素 #{locator_used}"
                        elif "input_text" in act_dict:
                            action_type = "INPUT"
                            inp = act_dict.get("input_text", {})
                            locator_used = str(inp.get("index", ""))
                            input_value = inp.get("text", "")
                            target_summary = f"在元素 #{locator_used} 输入文本"
                        elif "scroll" in act_dict:
                            action_type = "SCROLL"
                            target_summary = "页面滚动"

                traces.append({
                    "trace_no": trace_no,
                    "action_type": action_type,
                    "page_url": page_url,
                    "target_summary": target_summary,
                    "locator_used": locator_used,
                    "input_value_masked": _mask_sensitive(input_value),
                    "action_result": action_result_str,
                    "error_message": error_msg,
                    "screenshot_url": "",
                    "occurred_at": f"00:{trace_no:02d}.00",
                })

        return {
            "success": True,
            "traces": traces,
            "screenshots": screenshots,
            "error_message": "",
        }

    except Exception as e:
        logger.error(f"Browser exploration failed for task {task_id}: {e}")
        logger.error(traceback.format_exc())
        return {
            "success": False,
            "traces": traces,
            "screenshots": screenshots,
            "error_message": str(e)[:1000],
        }


def _mask_sensitive(value: str) -> str:
    """对敏感输入值进行脱敏处理"""
    if not value:
        return ""
    if len(value) <= 4:
        return "****"
    return value[:2] + "***" + value[-2:]
