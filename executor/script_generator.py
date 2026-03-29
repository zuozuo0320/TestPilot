"""
script_generator.py — 基于操作轨迹生成 Playwright TypeScript 测试脚本

支持两种模式：
1. AI 直生模式：基于 browser-use 采集的轨迹 -> TypeScript 脚本
2. 录制增强模式：基于原始录制稿 + step_model_json -> 标准化 TypeScript 脚本
"""
import json
import logging
import re
from typing import Optional

from openai import OpenAI

from config import OPENAI_API_KEY, OPENAI_BASE_URL, OPENAI_MODEL

logger = logging.getLogger(__name__)


def _build_client() -> OpenAI:
    """构建 OpenAI 客户端"""
    kwargs = {"api_key": OPENAI_API_KEY}
    if OPENAI_BASE_URL:
        kwargs["base_url"] = OPENAI_BASE_URL
    return OpenAI(**kwargs)


# ── 系统提示词 ──

SYSTEM_PROMPT_AI_DIRECT = """你是一位资深的 Playwright 自动化测试工程师。
你的职责是根据浏览器探索轨迹，生成标准化的 Playwright TypeScript 测试脚本。

## 输出规范
- 框架: @playwright/test
- 语言: TypeScript
- 风格: 单文件 .spec.ts，使用 test.describe / test / test.step 三层结构
- 定位器优先级: data-testid > getByRole > getByText > getByLabel > CSS
- 必须包含至少 1 个 expect 断言
- 等待策略: page.waitForLoadState / locator.waitFor()
- 敏感信息使用环境变量: process.env.XXX

## 输出格式
你必须输出一个 JSON 对象，包含以下字段：
{
  "script_content": "完整的 TypeScript 脚本代码",
  "risk_hints": ["风险提示列表"],
  "assertion_suggestions": ["断言建议列表"],
  "generation_summary": "生成摘要"
}

只输出 JSON，不要输出任何其他内容。"""

SYSTEM_PROMPT_REFACTOR = """你是一位受控的 Playwright 脚本重构器。
你的输入是 Playwright Codegen 录制的原始脚本，你的任务是将其重构为团队标准格式。

## 重构规则
1. 保持原始录制的操作序列不变，不增加也不删除步骤
2. 将扁平操作列表重构为 test.describe > test > test.step 三层结构
3. 为每个 test.step 赋予语义化名称（中文）
4. 替换不稳定的定位器：优先用 data-testid > getByRole > getByText > getByLabel > CSS
5. 添加适当的等待策略
6. 在关键步骤后添加 expect 断言
7. 敏感信息替换为 process.env.XXX

## 输出格式
你必须输出一个 JSON 对象，包含以下字段：
{
  "script_content": "重构后的完整 TypeScript 脚本代码",
  "risk_hints": ["风险提示列表"],
  "assertion_suggestions": ["断言建议列表"],
  "generation_summary": "重构摘要"
}

只输出 JSON，不要输出任何其他内容。"""


def generate_playwright_script(
    scenario_desc: str,
    start_url: str,
    traces: list[dict],
    account_ref: Optional[str] = None,
) -> dict:
    """
    AI 直生模式：基于操作轨迹，调用 LLM 生成 Playwright TypeScript 脚本

    Returns:
        {script_content, risk_hints, assertion_suggestions, generation_summary}
    """
    # 构建轨迹描述
    trace_lines = []
    for t in traces:
        line = f"  步骤 {t['trace_no']}: [{t['action_type']}]"
        if t.get("target_summary"):
            line += f" {t['target_summary']}"
        if t.get("locator_used"):
            line += f" (定位器: {t['locator_used']})"
        if t.get("input_value_masked"):
            line += f" 输入: {t['input_value_masked']}"
        if t.get("page_url"):
            line += f" URL: {t['page_url']}"
        if t.get("action_result") == "error":
            line += f" [失败: {t.get('error_message', '')}]"
        trace_lines.append(line)

    traces_text = "\n".join(trace_lines) if trace_lines else "  （无轨迹数据）"

    user_prompt = f"""## 测试场景
{scenario_desc}

## 起始 URL
{start_url}

## 操作轨迹（browser-use 采集）
{traces_text}
"""

    if account_ref:
        user_prompt += f"\n## 测试账号参考\n{account_ref}\n"

    return _call_llm(SYSTEM_PROMPT_AI_DIRECT, user_prompt, scenario_desc, start_url, traces)


def refactor_recorded_script(
    scenario_desc: str,
    start_url: str,
    raw_script: str,
    step_model_json: Optional[dict] = None,
    account_ref: Optional[str] = None,
) -> dict:
    """
    录制增强模式：将原始录制稿重构为标准化 TypeScript 脚本

    Returns:
        {script_content, risk_hints, assertion_suggestions, generation_summary}
    """
    user_prompt = f"""## 测试场景
{scenario_desc}

## 起始 URL
{start_url}

## 原始录制稿（Playwright Codegen 输出）
```typescript
{raw_script}
```
"""

    if step_model_json:
        user_prompt += f"\n## 步骤模型（结构化解析结果）\n```json\n{json.dumps(step_model_json, ensure_ascii=False, indent=2)}\n```\n"

    if account_ref:
        user_prompt += f"\n## 测试账号参考\n{account_ref}\n"

    return _call_llm(SYSTEM_PROMPT_REFACTOR, user_prompt, scenario_desc, start_url, [], raw_script=raw_script)


def _call_llm(system_prompt: str, user_prompt: str, scenario_desc: str, start_url: str, traces: list, raw_script: str = "") -> dict:
    """调用 LLM 生成脚本，支持重试"""
    import time
    max_retries = 3
    last_error = None

    for attempt in range(max_retries):
        try:
            client = _build_client()
            response = client.chat.completions.create(
                model=OPENAI_MODEL,
                messages=[
                    {"role": "system", "content": system_prompt},
                    {"role": "user", "content": user_prompt},
                ],
                temperature=0.1,
                max_tokens=8192,
            )

            content = response.choices[0].message.content.strip()

            # 提取 JSON（处理 markdown 代码块包裹）
            if content.startswith("```json"):
                content = content[len("```json"):].strip()
            if content.startswith("```"):
                content = content[3:].strip()
            if content.endswith("```"):
                content = content[:-3].strip()

            result = json.loads(content)
            return {
                "script_content": result.get("script_content", ""),
                "risk_hints": result.get("risk_hints", []),
                "assertion_suggestions": result.get("assertion_suggestions", []),
                "generation_summary": result.get("generation_summary", ""),
            }

        except json.JSONDecodeError:
            logger.warning("LLM returned non-JSON, treating as raw script content")
            return {
                "script_content": content if 'content' in dir() else "",
                "risk_hints": [],
                "assertion_suggestions": [],
                "generation_summary": "LLM 输出未按 JSON 格式，直接使用原始内容",
            }
        except Exception as e:
            last_error = e
            error_str = str(e).lower()

            # 403: 鉴权失败 — 不重试，直接 fallback
            if "403" in error_str or "forbidden" in error_str or "authentication" in error_str:
                logger.error(f"LLM authentication failed (403), skipping retries: {e}")
                break

            # 429: 限流 — 用更长等待
            if "429" in error_str or "rate_limit" in error_str or "too_many_requests" in error_str:
                wait_sec = 5 * (attempt + 1)
                logger.warning(f"LLM rate limited (429), attempt {attempt + 1}/{max_retries}, waiting {wait_sec}s")
                if attempt < max_retries - 1:
                    time.sleep(wait_sec)
                continue

            # 503/500: 服务过载/内部错误 — 正常递增等待重试
            if "503" in error_str or "500" in error_str or "overloaded" in error_str:
                logger.warning(f"LLM service error, attempt {attempt + 1}/{max_retries}: {e}")
                if attempt < max_retries - 1:
                    time.sleep(2 * (attempt + 1))
                continue

            # 其他错误 — 正常重试
            logger.warning(f"LLM call attempt {attempt + 1}/{max_retries} failed: {e}")
            if attempt < max_retries - 1:
                time.sleep(2 * (attempt + 1))

    logger.error(f"All {max_retries} LLM attempts failed: {last_error}")
    return _generate_fallback_script(scenario_desc, start_url, traces, raw_script)


def _generate_fallback_script(scenario_desc: str, start_url: str, traces: list[dict], raw_script: str = "") -> dict:
    """
    当 LLM 调用失败时的 Fallback 策略：
    - 如果有原始录制脚本 (raw_script)，直接返回它（录制增强模式）
    - 如果有轨迹数据 (traces)，基于轨迹生成模板（AI 直生模式）
    - 都没有则生成最小骨架
    """

    # ★ 录制增强模式：直接使用原始录制脚本
    if raw_script and raw_script.strip():
        logger.info("Fallback: using raw recorded script directly (LLM unavailable)")
        return {
            "script_content": raw_script.strip(),
            "risk_hints": ["AI 重构服务暂不可用，当前为原始录制脚本，建议稍后重试 AI 重构"],
            "assertion_suggestions": ["手动添加业务断言"],
            "generation_summary": "LLM 不可用，已直接使用原始录制脚本",
        }

    # AI 直生模式：基于轨迹生成模板
    lines = [
        f'// 自动生成的 Playwright TypeScript 测试脚本',
        f'// 场景: {scenario_desc}',
        "import { test, expect } from '@playwright/test';",
        "",
        f"test.describe('{scenario_desc}', () => {{",
        f"  test('主流程验证', async ({{ page }}) => {{",
        "",
        f"    // 步骤 1: 导航到起始页面",
        f"    await test.step('打开起始页面', async () => {{",
        f"      await page.goto('{start_url}');",
        f"      await page.waitForLoadState('networkidle');",
        f"    }});",
    ]

    step_no = 1
    for t in traces:
        step_no += 1
        action = t.get("action_type", "CUSTOM")
        locator = t.get("locator_used", "")
        summary = t.get("target_summary", f"步骤 {step_no}")

        lines.append("")
        lines.append(f"    // 步骤 {step_no}: {summary}")
        lines.append(f"    await test.step('{summary}', async () => {{")

        if action == "CLICK" and locator:
            lines.append(f"      await page.locator('{locator}').click();")
        elif action == "INPUT" and locator:
            value = t.get("input_value_masked", "test_input")
            lines.append(f"      await page.locator('{locator}').fill('{value}');")
        elif action == "NAVIGATE":
            url = t.get("page_url", "")
            if url:
                lines.append(f"      await page.goto('{url}');")
        elif action == "WAIT":
            lines.append("      await page.waitForTimeout(1000);")
        elif action == "ASSERT_CANDIDATE":
            lines.append(f"      // TODO: 添加断言 - {summary}")

        lines.append("    });")

    lines.extend([
        "",
        "    // 最终断言",
        "    await test.step('验证页面状态', async () => {",
        "      await expect(page).not.toHaveURL('about:blank');",
        "    });",
        "  });",
        "});",
    ])

    return {
        "script_content": "\n".join(lines),
        "risk_hints": ["此脚本为 fallback 模板，LLM 调用失败，请手动检查定位器"],
        "assertion_suggestions": ["添加业务结果断言"],
        "generation_summary": "LLM 调用失败，已生成基础模板",
    }


def parse_step_model(raw_script: str) -> dict:
    """
    从 Playwright Codegen 录制稿中提取结构化步骤模型（纯正则，不依赖 LLM）

    返回格式：
    {
        "steps": [
            {"step_no": 1, "action_type": "NAVIGATE", "locator": "", "input_value": "", "page_url": "https://..."},
            {"step_no": 2, "action_type": "CLICK", "locator": "getByRole('link', { name: '...' })", ...},
            {"step_no": 3, "action_type": "INPUT", "locator": "getByPlaceholder('...')", "input_value": "fofa", ...},
        ],
        "total_steps": 3
    }
    """
    steps = []
    step_no = 0

    for line in raw_script.split("\n"):
        stripped = line.strip()
        if not stripped or stripped.startswith("//") or stripped.startswith("import "):
            continue

        # page.goto('url')
        m = re.search(r"page\.goto\(['\"](.+?)['\"]\)", stripped)
        if m:
            step_no += 1
            steps.append({
                "step_no": step_no,
                "action_type": "NAVIGATE",
                "locator": "",
                "input_value": "",
                "page_url": m.group(1),
            })
            continue

        # page.getByXxx(...).click()
        m = re.search(r"page\.(getBy\w+\([^)]*\)|locator\([^)]*\))\.click\(\)", stripped)
        if m:
            step_no += 1
            steps.append({
                "step_no": step_no,
                "action_type": "CLICK",
                "locator": m.group(1),
                "input_value": "",
                "page_url": "",
            })
            continue

        # page.getByXxx(...).fill('value')
        m = re.search(r"page\.(getBy\w+\([^)]*\)|locator\([^)]*\))\.fill\(['\"](.+?)['\"]\)", stripped)
        if m:
            step_no += 1
            steps.append({
                "step_no": step_no,
                "action_type": "INPUT",
                "locator": m.group(1),
                "input_value": m.group(2),
                "page_url": "",
            })
            continue

        # page.getByXxx(...).press('key')
        m = re.search(r"page\.(getBy\w+\([^)]*\)|locator\([^)]*\))\.press\(['\"](.+?)['\"]\)", stripped)
        if m:
            step_no += 1
            steps.append({
                "step_no": step_no,
                "action_type": "KEY_PRESS",
                "locator": m.group(1),
                "input_value": m.group(2),
                "page_url": "",
            })
            continue

        # page.getByXxx(...).selectOption('value')
        m = re.search(r"page\.(getBy\w+\([^)]*\)|locator\([^)]*\))\.selectOption\(['\"](.+?)['\"]\)", stripped)
        if m:
            step_no += 1
            steps.append({
                "step_no": step_no,
                "action_type": "SELECT",
                "locator": m.group(1),
                "input_value": m.group(2),
                "page_url": "",
            })
            continue

        # page.waitForURL / page.waitForLoadState
        m = re.search(r"page\.waitFor(URL|LoadState)\(['\"]?(.+?)['\"]?\)", stripped)
        if m:
            step_no += 1
            steps.append({
                "step_no": step_no,
                "action_type": "WAIT",
                "locator": "",
                "input_value": m.group(2),
                "page_url": "",
            })
            continue

        # expect(...) assertions
        m = re.search(r"expect\((.+?)\)", stripped)
        if m:
            step_no += 1
            steps.append({
                "step_no": step_no,
                "action_type": "ASSERT_CANDIDATE",
                "locator": m.group(1).strip(),
                "input_value": "",
                "page_url": "",
            })
            continue

    return {
        "steps": steps,
        "total_steps": len(steps),
    }
