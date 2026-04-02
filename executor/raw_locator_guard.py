"""
raw_locator_guard.py - 录制脚本原始定位器守卫

职责：
1. 从录制脚本中提取原始 locator chain。
2. 基于同一套解析逻辑构建 step_model，避免链式定位器被截断。
3. 校验 V1 输出中是否出现了录制脚本之外的新定位器表达式。
"""
from __future__ import annotations

import logging
import re
from typing import Optional

logger = logging.getLogger(__name__)

# 这些方法可以作为 Playwright locator 链的起点。
_LOCATOR_START_METHODS = {
    "locator",
    "frameLocator",
    "getByRole",
    "getByText",
    "getByLabel",
    "getByPlaceholder",
    "getByTestId",
    "getByAltText",
    "getByTitle",
    "getByDisplayValue",
}

# 这些方法允许继续追加在 locator chain 后面。
_LOCATOR_CHAIN_METHODS = _LOCATOR_START_METHODS | {
    "filter",
    "first",
    "last",
    "nth",
    "and",
    "or",
}

# 这些方法意味着 locator chain 在这里结束，后面进入动作执行阶段。
_TERMINAL_ACTION_METHODS = {
    "blur",
    "check",
    "clear",
    "click",
    "count",
    "dblclick",
    "dispatchEvent",
    "dragTo",
    "fill",
    "focus",
    "hover",
    "innerText",
    "inputValue",
    "isDisabled",
    "isEnabled",
    "isHidden",
    "isVisible",
    "press",
    "scrollIntoViewIfNeeded",
    "selectOption",
    "selectText",
    "setInputFiles",
    "tap",
    "textContent",
    "type",
    "uncheck",
    "waitFor",
}


def _dedupe_keep_order(items: list[str]) -> list[str]:
    """对列表去重并保留原顺序，避免重复返回同一条定位器信息。"""
    seen: set[str] = set()
    result: list[str] = []
    for item in items:
        if item in seen:
            continue
        seen.add(item)
        result.append(item)
    return result



def _skip_whitespace(text: str, index: int) -> int:
    """跳过源码中的空白字符，方便做链式解析。"""
    while index < len(text) and text[index].isspace():
        index += 1
    return index



def _read_identifier(text: str, index: int) -> tuple[str, int]:
    """从指定位置读取 TypeScript 标识符。"""
    if index >= len(text) or not (text[index].isalpha() or text[index] == "_"):
        return "", index

    start = index
    index += 1
    while index < len(text) and (text[index].isalnum() or text[index] == "_"):
        index += 1
    return text[start:index], index



def _consume_string(text: str, index: int) -> int:
    """跳过字符串字面量，避免把字符串内的括号误判为源码结构。"""
    quote = text[index]
    index += 1

    while index < len(text):
        char = text[index]
        if char == "\\":
            index += 2
            continue
        if char == quote:
            return index + 1
        index += 1

    return index



def _consume_line_comment(text: str, index: int) -> int:
    """跳过单行注释。"""
    while index < len(text) and text[index] != "\n":
        index += 1
    return index



def _consume_block_comment(text: str, index: int) -> int:
    """跳过块注释。"""
    index += 2
    while index < len(text) - 1:
        if text[index] == "*" and text[index + 1] == "/":
            return index + 2
        index += 1
    return len(text)



def _find_matching_parenthesis(text: str, start: int) -> int:
    """查找与起始左括号匹配的右括号位置。"""
    depth = 0
    index = start

    while index < len(text):
        char = text[index]

        if char in {"'", '"', '`'}:
            index = _consume_string(text, index)
            continue

        if char == "/" and index + 1 < len(text):
            if text[index + 1] == "/":
                index = _consume_line_comment(text, index)
                continue
            if text[index + 1] == "*":
                index = _consume_block_comment(text, index)
                continue

        if char == "(":
            depth += 1
        elif char == ")":
            depth -= 1
            if depth == 0:
                return index

        index += 1

    return -1



def _normalize_source_prefix(locator: str) -> str:
    """统一源码中的 page / this.page 前缀，便于跨文件比对。"""
    return re.sub(r"\bthis\.page\.", "page.", locator.strip())



def normalize_locator_expression(locator: str) -> str:
    """标准化定位器表达式，只移除字符串字面量之外的空白差异。"""
    locator = _normalize_source_prefix(locator).strip().rstrip(";")
    if not locator:
        return ""

    result: list[str] = []
    index = 0
    while index < len(locator):
        char = locator[index]
        if char in {"'", '"', '`'}:
            end = _consume_string(locator, index)
            result.append(locator[index:end])
            index = end
            continue
        if char.isspace():
            index += 1
            continue
        result.append(char)
        index += 1
    return "".join(result)



def _is_start_boundary(text: str, index: int) -> bool:
    """确认当前 page. 不是其它标识符的一部分。"""
    if index == 0:
        return True
    prev = text[index - 1]
    return not (prev.isalnum() or prev in {"_", "$", "."})



def _consume_locator_chain(text: str, index: int) -> tuple[Optional[str], int]:
    """从 page. / this.page. 起点消费完整的 locator chain。"""
    if text.startswith("this.page.", index):
        prefix = "this.page."
    elif text.startswith("page.", index):
        prefix = "page."
    else:
        return None, index + 1

    if not _is_start_boundary(text, index):
        return None, index + 1

    cursor = index + len(prefix)
    method_name, cursor = _read_identifier(text, cursor)
    if method_name not in _LOCATOR_START_METHODS:
        return None, index + 1

    cursor = _skip_whitespace(text, cursor)
    if cursor >= len(text) or text[cursor] != "(":
        return None, index + 1

    end = _find_matching_parenthesis(text, cursor)
    if end == -1:
        return None, len(text)

    locator = prefix + method_name + text[cursor:end + 1]
    cursor = end + 1

    while cursor < len(text):
        dot_index = _skip_whitespace(text, cursor)
        if dot_index >= len(text) or text[dot_index] != ".":
            return _normalize_source_prefix(locator), dot_index

        name_index = _skip_whitespace(text, dot_index + 1)
        method_name, name_end = _read_identifier(text, name_index)
        if not method_name:
            return _normalize_source_prefix(locator), dot_index

        if method_name in _TERMINAL_ACTION_METHODS:
            return _normalize_source_prefix(locator), dot_index

        if method_name not in _LOCATOR_CHAIN_METHODS and not method_name.startswith("getBy"):
            return _normalize_source_prefix(locator), dot_index

        name_end = _skip_whitespace(text, name_end)
        if name_end >= len(text) or text[name_end] != "(":
            return _normalize_source_prefix(locator), dot_index

        end = _find_matching_parenthesis(text, name_end)
        if end == -1:
            return _normalize_source_prefix(locator), len(text)

        locator += "." + method_name + text[name_end:end + 1]
        cursor = end + 1

    return _normalize_source_prefix(locator), cursor



def extract_raw_locator_chains(text: str) -> list[str]:
    """提取文本中所有 page locator chain，并保持首次出现顺序。"""
    locators: list[str] = []
    index = 0

    while index < len(text):
        char = text[index]

        if char in {"'", '"', '`'}:
            index = _consume_string(text, index)
            continue

        if char == "/" and index + 1 < len(text):
            if text[index + 1] == "/":
                index = _consume_line_comment(text, index)
                continue
            if text[index + 1] == "*":
                index = _consume_block_comment(text, index)
                continue

        locator, next_index = _consume_locator_chain(text, index)
        if locator:
            locators.append(locator)
            index = max(next_index, index + 1)
            continue

        index += 1

    return _dedupe_keep_order(locators)



def _split_top_level_args(args_text: str) -> list[str]:
    """按顶层逗号拆分参数，避免对象字面量内部逗号被误切分。"""
    parts: list[str] = []
    current: list[str] = []
    paren_depth = 0
    bracket_depth = 0
    brace_depth = 0
    index = 0

    while index < len(args_text):
        char = args_text[index]

        if char in {"'", '"', '`'}:
            end = _consume_string(args_text, index)
            current.append(args_text[index:end])
            index = end
            continue

        if char == "(":
            paren_depth += 1
        elif char == ")":
            paren_depth = max(paren_depth - 1, 0)
        elif char == "[":
            bracket_depth += 1
        elif char == "]":
            bracket_depth = max(bracket_depth - 1, 0)
        elif char == "{":
            brace_depth += 1
        elif char == "}":
            brace_depth = max(brace_depth - 1, 0)
        elif char == "," and paren_depth == 0 and bracket_depth == 0 and brace_depth == 0:
            parts.append("".join(current).strip())
            current = []
            index += 1
            continue

        current.append(char)
        index += 1

    tail = "".join(current).strip()
    if tail:
        parts.append(tail)
    return parts



def _unwrap_simple_literal(value: str) -> str:
    """尽量把简单字符串字面量还原为纯文本，便于写入 step_model。"""
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"', '`'}:
        quote = value[0]
        body = value[1:-1]
        body = body.replace(f"\\{quote}", quote)
        body = body.replace("\\n", "\n")
        body = body.replace("\\t", "\t")
        body = body.replace("\\r", "\r")
        body = body.replace("\\\\", "\\")
        return body
    return value



def _parse_first_arg(args_text: str) -> str:
    """提取动作调用的首个参数，兼容字符串与对象字面量。"""
    args = _split_top_level_args(args_text)
    if not args:
        return ""
    return _unwrap_simple_literal(args[0])



def _split_statements(script: str) -> list[str]:
    """按分号切分脚本语句，兼容录制脚本外层 test/use 代码块。"""
    statements: list[str] = []
    current: list[str] = []
    index = 0
    paren_depth = 0
    bracket_depth = 0
    brace_depth = 0

    while index < len(script):
        char = script[index]

        if char in {"'", '"', '`'}:
            end = _consume_string(script, index)
            current.append(script[index:end])
            index = end
            continue

        if char == "/" and index + 1 < len(script):
            if script[index + 1] == "/":
                end = _consume_line_comment(script, index)
                current.append(script[index:end])
                index = end
                continue
            if script[index + 1] == "*":
                end = _consume_block_comment(script, index)
                current.append(script[index:end])
                index = end
                continue

        if char == "(":
            paren_depth += 1
        elif char == ")":
            paren_depth = max(paren_depth - 1, 0)
        elif char == "[":
            bracket_depth += 1
        elif char == "]":
            bracket_depth = max(bracket_depth - 1, 0)
        elif char == "{":
            brace_depth += 1
        elif char == "}":
            brace_depth = max(brace_depth - 1, 0)
        # Playwright 录制脚本通常包裹在 test(...) => { ... } 代码块中。
        # 业务语句（await page.xxx(...)）末尾的分号出现时，括号和方括号一定是平衡的
        # （paren_depth == 1 表示仍在 test() 内，bracket_depth == 0）。
        # 注意：filter({ hasText: '...' }) 等内联对象会让 brace_depth 短暂升到 2 以上，
        # 但在语句末尾 ; 处内联对象的花括号已经闭合，因此不能用 brace_depth 作为切分条件。
        elif char == ";" and paren_depth <= 1 and bracket_depth == 0:
            current.append(char)
            statement = "".join(current).strip()
            if statement:
                statements.append(statement)
            current = []
            index += 1
            continue

        current.append(char)
        index += 1

    tail = "".join(current).strip()
    if tail:
        statements.append(tail)

    return statements



def _extract_first_locator_action(statement: str) -> tuple[Optional[str], str, str]:
    """提取语句中的首个 locator 动作及其参数。"""
    index = 0
    while index < len(statement):
        char = statement[index]

        if char in {"'", '"', '`'}:
            index = _consume_string(statement, index)
            continue

        if char == "/" and index + 1 < len(statement):
            if statement[index + 1] == "/":
                index = _consume_line_comment(statement, index)
                continue
            if statement[index + 1] == "*":
                index = _consume_block_comment(statement, index)
                continue

        locator, next_index = _consume_locator_chain(statement, index)
        if locator:
            action_dot = _skip_whitespace(statement, next_index)
            if action_dot < len(statement) and statement[action_dot] == ".":
                name_index = _skip_whitespace(statement, action_dot + 1)
                action_name, action_end = _read_identifier(statement, name_index)
                if action_name in _TERMINAL_ACTION_METHODS:
                    action_end = _skip_whitespace(statement, action_end)
                    if action_end < len(statement) and statement[action_end] == "(":
                        args_end = _find_matching_parenthesis(statement, action_end)
                        if args_end != -1:
                            return locator, action_name, statement[action_end + 1:args_end]
                    return locator, action_name, ""
            return locator, "", ""

        index += 1

    return None, "", ""



def _strip_page_prefix(locator: str) -> str:
    """为了兼容现有 step_model 结构，输出时去掉 page. 前缀。"""
    locator = _normalize_source_prefix(locator)
    if locator.startswith("page."):
        return locator[len("page."):]
    return locator


def _ensure_page_prefix(locator: str) -> str:
    """为 locator 补齐 page. 前缀，便于做统一归一化比较。"""
    candidate = (locator or "").strip()
    if not candidate:
        return ""
    if candidate.startswith("page.") or candidate.startswith("this.page."):
        return candidate
    return f"page.{candidate.lstrip('.')}"


def _normalize_step_locator(locator: str) -> str:
    """标准化 step_model 中的 locator，避免 page 前缀差异影响比较结果。"""
    candidate = _ensure_page_prefix(locator)
    if not candidate:
        return ""
    return normalize_locator_expression(candidate)


def _locator_looks_like_button_trigger(locator: str) -> bool:
    """判断定位器是否更像按钮型 opener。"""
    normalized = _normalize_step_locator(locator)
    if not normalized:
        return False
    return bool(re.search(r"getByRole\((['\"])button\1", normalized))


def _locator_looks_like_form_control(locator: str) -> bool:
    """判断定位器是否更像弹窗或表单内部控件。"""
    normalized = _normalize_step_locator(locator)
    if not normalized:
        return False

    patterns = (
        r"locator\((['\"])textarea\1\)",
        r"locator\((['\"])(?:input|select)\1\)",
        r"getByRole\((['\"])(?:textbox|combobox|spinbutton|checkbox|radio|switch)\1",
        r"getByLabel\(",
        r"getByPlaceholder\(",
        r"getByDisplayValue\(",
    )
    return any(re.search(pattern, normalized) for pattern in patterns)


def _find_next_meaningful_step(parsed_steps: list[dict], start_index: int) -> Optional[dict]:
    """向后查找下一个真正参与业务判断的步骤，跳过等待和断言候选。"""
    for index in range(start_index, len(parsed_steps)):
        step = parsed_steps[index]["step"]
        if step.get("action_type") in {"WAIT", "ASSERT_CANDIDATE"}:
            continue
        return parsed_steps[index]
    return None


def _should_collapse_duplicate_opener_click(
    previous_step: dict,
    current_step: dict,
    next_meaningful_step: Optional[dict],
) -> bool:
    """
    判断当前点击是否属于“冗余 opener 重复点击”。

    只有当两次连续点击同一按钮型 opener，且后续步骤已经进入表单/弹窗内部交互时，
    才允许在生成阶段折叠掉第二次点击，避免把录制噪声直接带进产物脚本。
    """
    if previous_step.get("action_type") != "CLICK" or current_step.get("action_type") != "CLICK":
        return False

    previous_locator = _normalize_step_locator(previous_step.get("locator", ""))
    current_locator = _normalize_step_locator(current_step.get("locator", ""))
    if not previous_locator or previous_locator != current_locator:
        return False

    if not _locator_looks_like_button_trigger(current_step.get("locator", "")):
        return False

    if not next_meaningful_step:
        return False

    next_action_type = (next_meaningful_step.get("action_type") or "").upper()
    if next_action_type in {"INPUT", "SELECT", "KEY_PRESS"}:
        return True

    if next_action_type == "CLICK":
        next_locator = next_meaningful_step.get("locator", "")
        if _normalize_step_locator(next_locator) == current_locator:
            return False
        return _locator_looks_like_form_control(next_locator)

    return False


def _build_generation_projection(parsed_steps: list[dict]) -> tuple[list[dict], list[dict]]:
    """基于原始步骤构建生成阶段使用的步骤序列和归一化说明。"""
    kept_steps: list[dict] = []
    normalization_items: list[dict] = []

    for index, parsed_step in enumerate(parsed_steps):
        current_step = parsed_step["step"]
        previous_step = kept_steps[-1]["step"] if kept_steps else None
        next_meaningful = _find_next_meaningful_step(parsed_steps, index + 1)

        if previous_step and _should_collapse_duplicate_opener_click(
            previous_step=previous_step,
            current_step=current_step,
            next_meaningful_step=next_meaningful["step"] if next_meaningful else None,
        ):
            normalization_items.append({
                "type": "collapse_duplicate_opener_click",
                "kept_raw_step_no": previous_step.get("step_no"),
                "removed_raw_step_no": current_step.get("step_no"),
                "locator": current_step.get("locator", ""),
                "next_action_type": next_meaningful["step"].get("action_type") if next_meaningful else "",
                "next_locator": next_meaningful["step"].get("locator", "") if next_meaningful else "",
                "reason": "连续两次点击同一 opener，且后续已进入表单或弹窗内部交互，生成阶段仅保留第一次点击。",
                "removed_statement_index": parsed_step["statement_index"],
            })
            continue

        kept_steps.append(parsed_step)

    generation_steps: list[dict] = []
    for generation_step_no, parsed_step in enumerate(kept_steps, start=1):
        raw_step = parsed_step["step"]
        generation_steps.append({
            "step_no": generation_step_no,
            "raw_step_no": raw_step.get("step_no"),
            "action_type": raw_step.get("action_type", ""),
            "locator": raw_step.get("locator", ""),
            "input_value": raw_step.get("input_value", ""),
            "page_url": raw_step.get("page_url", ""),
        })

    return generation_steps, normalization_items


def _build_generation_script_from_statements(
    raw_script: str,
    statements: list[str],
    normalization_items: list[dict],
) -> str:
    """按归一化结果裁剪录制稿，生成供代码生成阶段使用的录制脚本。"""
    removed_statement_indexes = {
        item.get("removed_statement_index")
        for item in normalization_items
        if item.get("removed_statement_index") is not None
    }
    if not removed_statement_indexes:
        return raw_script.strip()

    kept_statements = [
        statement
        for index, statement in enumerate(statements)
        if index not in removed_statement_indexes
    ]
    return "\n".join(kept_statements).strip()


def build_generation_script_from_recording(raw_script: str, step_model: Optional[dict] = None) -> str:
    """对外暴露生成阶段使用的归一化录制稿，供不同生成链路复用。"""
    if step_model and step_model.get("generation_script"):
        return step_model["generation_script"]

    built_step_model = step_model or build_step_model_from_recording(raw_script)
    generation_script = built_step_model.get("generation_script")
    if generation_script:
        return generation_script
    return (raw_script or "").strip()



def build_step_model_from_recording(raw_script: str) -> dict:
    """从录制脚本中构建结构化 step_model，并完整保留链式定位器。"""
    if not raw_script or not raw_script.strip():
        return {
            "steps": [],
            "total_steps": 0,
            "generation_steps": [],
            "generation_total_steps": 0,
            "normalization_items": [],
            "generation_script": "",
        }

    statements = _split_statements(raw_script)
    parsed_steps: list[dict] = []
    step_no = 0

    for statement_index, statement in enumerate(statements):
        stripped = statement.strip()
        if not stripped or stripped.startswith("import "):
            continue

        goto_match = re.search(r"page\.goto\((?P<args>[\s\S]+?)\)\s*;?$", stripped)
        if goto_match:
            step_no += 1
            parsed_steps.append({
                "statement_index": statement_index,
                "step": {
                    "step_no": step_no,
                    "action_type": "NAVIGATE",
                    "locator": "",
                    "input_value": "",
                    "page_url": _parse_first_arg(goto_match.group("args")),
                },
            })
            continue

        wait_match = re.search(r"page\.waitFor(?:URL|LoadState)\((?P<args>[\s\S]+?)\)\s*;?$", stripped)
        if wait_match:
            step_no += 1
            parsed_steps.append({
                "statement_index": statement_index,
                "step": {
                    "step_no": step_no,
                    "action_type": "WAIT",
                    "locator": "",
                    "input_value": _parse_first_arg(wait_match.group("args")),
                    "page_url": "",
                },
            })
            continue

        locator, action_name, args_text = _extract_first_locator_action(stripped)
        if locator and action_name:
            action_type = {
                "click": "CLICK",
                "dblclick": "CLICK",
                "hover": "CLICK",
                "tap": "CLICK",
                "check": "CLICK",
                "uncheck": "CLICK",
                "focus": "CLICK",
                "blur": "CLICK",
                "fill": "INPUT",
                "type": "INPUT",
                "clear": "INPUT",
                "press": "KEY_PRESS",
                "selectOption": "SELECT",
                "waitFor": "WAIT",
            }.get(action_name)

            if action_type:
                step_no += 1
                parsed_steps.append({
                    "statement_index": statement_index,
                    "step": {
                        "step_no": step_no,
                        "action_type": action_type,
                        "locator": _strip_page_prefix(locator),
                        "input_value": _parse_first_arg(args_text),
                        "page_url": "",
                    },
                })
                continue

        if "expect(" in stripped:
            expect_target = ""
            locators = extract_raw_locator_chains(stripped)
            if locators:
                expect_target = _strip_page_prefix(locators[0])
            else:
                match = re.search(r"expect\((?P<target>[\s\S]+?)\)", stripped)
                if match:
                    expect_target = match.group("target").strip()

            step_no += 1
            parsed_steps.append({
                "statement_index": statement_index,
                "step": {
                    "step_no": step_no,
                    "action_type": "ASSERT_CANDIDATE",
                    "locator": expect_target,
                    "input_value": "",
                    "page_url": "",
                },
            })

    steps = [parsed_step["step"] for parsed_step in parsed_steps]
    generation_steps, normalization_items = _build_generation_projection(parsed_steps)
    generation_script = _build_generation_script_from_statements(
        raw_script=raw_script,
        statements=statements,
        normalization_items=normalization_items,
    )

    return {
        "steps": steps,
        "total_steps": len(steps),
        "generation_steps": generation_steps,
        "generation_total_steps": len(generation_steps),
        "normalization_items": [
            {
                key: value
                for key, value in item.items()
                if key != "removed_statement_index"
            }
            for item in normalization_items
        ],
        "generation_script": generation_script,
    }



def _append_validation_issue(
    manual_review_items: list[str],
    invalid_locators: list[dict],
    source: str,
    locator: str,
) -> None:
    """统一记录定位器校验问题，方便在 V1 管线中回传人工审核项。"""
    issue = f"{source} 出现未在 raw_script 中出现的定位器: {locator}"
    manual_review_items.append(issue)
    invalid_locators.append({"source": source, "locator": locator})



def _validate_locator_collection(
    source: str,
    locators: list[str],
    raw_locator_set: set[str],
    manual_review_items: list[str],
    invalid_locators: list[dict],
) -> None:
    """校验一组定位器是否全部来自原始录制脚本。"""
    for locator in locators:
        normalized = normalize_locator_expression(locator)
        if normalized and normalized not in raw_locator_set:
            _append_validation_issue(manual_review_items, invalid_locators, source, locator)



def _validate_definition_field(
    source: str,
    definition: str,
    raw_locator_set: set[str],
    manual_review_items: list[str],
    invalid_locators: list[dict],
) -> None:
    """校验 new_locators.definition，允许 page. 与 this.page. 的结构差异。"""
    if not definition or not definition.strip():
        return

    locators = extract_raw_locator_chains(definition)
    if locators:
        _validate_locator_collection(source, locators, raw_locator_set, manual_review_items, invalid_locators)
        return

    candidate = definition.strip()
    if not candidate.startswith("page.") and not candidate.startswith("this.page."):
        candidate = f"page.{candidate.lstrip('.')}"

    normalized = normalize_locator_expression(candidate)
    if normalized and normalized not in raw_locator_set:
        _append_validation_issue(manual_review_items, invalid_locators, source, definition.strip())


def _is_regex_literal_start(text: str, index: int) -> bool:
    """粗略判断当前位置是否为正则字面量起点，避免归一化时破坏 `/.../` 内容。"""
    if text[index] != "/" or index + 1 >= len(text) or text[index + 1] in {"/", "*"}:
        return False

    cursor = index - 1
    while cursor >= 0 and text[cursor].isspace():
        cursor -= 1

    if cursor < 0:
        return True

    return text[cursor] in {"(", ",", "=", ":", "!", "?"}


def _consume_regex_literal(text: str, index: int) -> int:
    """跳过正则字面量，避免把正则中的空白与括号误判为源码结构。"""
    cursor = index + 1
    in_charset = False

    while cursor < len(text):
        char = text[cursor]
        if char == "\\":
            cursor += 2
            continue
        if char == "[" and not in_charset:
            in_charset = True
            cursor += 1
            continue
        if char == "]" and in_charset:
            in_charset = False
            cursor += 1
            continue
        if char == "/" and not in_charset:
            cursor += 1
            while cursor < len(text) and text[cursor].isalpha():
                cursor += 1
            return cursor
        cursor += 1

    return len(text)


def normalize_url_check_expression(expression: str) -> str:
    """标准化 URL 等待/断言表达式，统一 page / this.page 前缀并移除无意义空白差异。"""
    expression = re.sub(r"\bthis\.page\b", "page", expression.strip()).rstrip(";")
    if not expression:
        return ""

    result: list[str] = []
    index = 0
    while index < len(expression):
        char = expression[index]
        if char in {"'", '"', "`"}:
            end = _consume_string(expression, index)
            result.append(expression[index:end])
            index = end
            continue
        if char == "/" and _is_regex_literal_start(expression, index):
            end = _consume_regex_literal(expression, index)
            result.append(expression[index:end])
            index = end
            continue
        if char.isspace():
            index += 1
            continue
        result.append(char)
        index += 1
    return "".join(result)


def _consume_wait_for_url_call(text: str, index: int) -> tuple[Optional[str], int]:
    """从 page.waitForURL / this.page.waitForURL 起点消费完整调用表达式。"""
    if text.startswith("this.page.waitForURL", index):
        prefix = "this.page.waitForURL"
    elif text.startswith("page.waitForURL", index):
        prefix = "page.waitForURL"
    else:
        return None, index + 1

    if not _is_start_boundary(text, index):
        return None, index + 1

    cursor = index + len(prefix)
    cursor = _skip_whitespace(text, cursor)
    if cursor >= len(text) or text[cursor] != "(":
        return None, index + 1

    end = _find_matching_parenthesis(text, cursor)
    if end == -1:
        return None, len(text)

    expression = re.sub(r"\bthis\.page\b", "page", text[index:end + 1].strip())
    return expression, end + 1


def _consume_to_have_url_assertion(text: str, index: int) -> tuple[Optional[str], int]:
    """从 expect(page).toHaveURL / expect(this.page).not.toHaveURL 中提取完整断言表达式。"""
    if not text.startswith("expect(", index):
        return None, index + 1

    if not _is_start_boundary(text, index):
        return None, index + 1

    open_paren = index + len("expect")
    end = _find_matching_parenthesis(text, open_paren)
    if end == -1:
        return None, len(text)

    target = text[open_paren + 1:end].strip()
    normalized_target = re.sub(r"\bthis\.page\b", "page", target)
    if normalized_target != "page":
        return None, end + 1

    cursor = _skip_whitespace(text, end + 1)
    if cursor >= len(text) or text[cursor] != ".":
        return None, end + 1

    qualifier = ""
    name_index = _skip_whitespace(text, cursor + 1)
    method_name, name_end = _read_identifier(text, name_index)
    if method_name == "not":
        qualifier = ".not"
        cursor = _skip_whitespace(text, name_end)
        if cursor >= len(text) or text[cursor] != ".":
            return None, end + 1
        name_index = _skip_whitespace(text, cursor + 1)
        method_name, name_end = _read_identifier(text, name_index)

    if method_name != "toHaveURL":
        return None, end + 1

    name_end = _skip_whitespace(text, name_end)
    if name_end >= len(text) or text[name_end] != "(":
        return None, end + 1

    args_end = _find_matching_parenthesis(text, name_end)
    if args_end == -1:
        return None, len(text)

    expression = f"expect(page){qualifier}.toHaveURL{text[name_end:args_end + 1]}"
    return expression, args_end + 1


def extract_url_wait_assertions(text: str) -> list[str]:
    """提取文本中的 URL 等待/断言表达式，用于拦截凭空发明的固定路由判断。"""
    results: list[str] = []
    index = 0

    while index < len(text):
        char = text[index]

        if char in {"'", '"', "`"}:
            index = _consume_string(text, index)
            continue

        if char == "/" and index + 1 < len(text):
            if text[index + 1] == "/":
                index = _consume_line_comment(text, index)
                continue
            if text[index + 1] == "*":
                index = _consume_block_comment(text, index)
                continue

        expression, next_index = _consume_wait_for_url_call(text, index)
        if expression:
            results.append(expression)
            index = max(next_index, index + 1)
            continue

        expression, next_index = _consume_to_have_url_assertion(text, index)
        if expression:
            results.append(expression)
            index = max(next_index, index + 1)
            continue

        index += 1

    return _dedupe_keep_order(results)


def _append_url_check_issue(
    manual_review_items: list[str],
    invalid_url_checks: list[dict],
    source: str,
    expression: str,
) -> None:
    """统一记录 URL 等待/断言校验问题。"""
    issue = f"{source} 出现未在 raw_script 中出现的 URL 等待/断言: {expression}"
    manual_review_items.append(issue)
    invalid_url_checks.append({"source": source, "expression": expression})


def _validate_url_check_collection(
    source: str,
    expressions: list[str],
    raw_url_check_set: set[str],
    manual_review_items: list[str],
    invalid_url_checks: list[dict],
) -> None:
    """校验一组 URL 等待/断言是否全部来自原始录制脚本。"""
    for expression in expressions:
        normalized = normalize_url_check_expression(expression)
        if normalized and normalized not in raw_url_check_set:
            _append_url_check_issue(manual_review_items, invalid_url_checks, source, expression)


def validate_v1_url_semantics_preservation(raw_script: str, llm_result: dict) -> dict:
    """校验 V1 输出是否凭空发明了 raw_script 中不存在的 URL 等待/断言。"""
    raw_url_checks = extract_url_wait_assertions(raw_script)
    raw_url_check_set = {normalize_url_check_expression(expression) for expression in raw_url_checks}

    manual_review_items: list[str] = []
    invalid_url_checks: list[dict] = []

    spec_file = llm_result.get("spec_file") or {}
    if spec_file.get("content"):
        source = f"spec_file({spec_file.get('path', 'unknown')})"
        _validate_url_check_collection(
            source,
            extract_url_wait_assertions(spec_file["content"]),
            raw_url_check_set,
            manual_review_items,
            invalid_url_checks,
        )

    for page_create in llm_result.get("page_creates", []):
        source = f"page_create({page_create.get('path', page_create.get('class_name', 'unknown'))})"
        _validate_url_check_collection(
            source,
            extract_url_wait_assertions(page_create.get("content", "")),
            raw_url_check_set,
            manual_review_items,
            invalid_url_checks,
        )

    for page_update in llm_result.get("page_updates", []):
        page_name = page_update.get("page_name", "unknown")

        for action_item in page_update.get("new_actions", []):
            source = f"page_update({page_name}).new_actions[{action_item.get('name', 'unknown')}]"
            _validate_url_check_collection(
                source,
                extract_url_wait_assertions(action_item.get("content", "")),
                raw_url_check_set,
                manual_review_items,
                invalid_url_checks,
            )

        for action_item in page_update.get("extend_actions", []):
            source = f"page_update({page_name}).extend_actions[{action_item.get('name', 'unknown')}]"
            _validate_url_check_collection(
                source,
                extract_url_wait_assertions(action_item.get("content", "")),
                raw_url_check_set,
                manual_review_items,
                invalid_url_checks,
            )

    manual_review_items = _dedupe_keep_order(manual_review_items)
    return {
        "raw_url_checks": raw_url_checks,
        "raw_url_check_count": len(raw_url_checks),
        "invalid_url_checks": invalid_url_checks,
        "manual_review_items": manual_review_items,
    }


def _count_locator_chain_segments(locator: str) -> int:
    """统计定位器链上的选择器段数，用于识别复杂原始定位器。"""
    normalized = normalize_locator_expression(locator)
    if not normalized:
        return 0

    return len(re.findall(r"\.(?:locator|frameLocator|getBy[A-Za-z]+|filter|first|last|nth)\b", normalized))


def _is_critical_raw_locator(locator: str) -> bool:
    """判断定位器是否属于必须显式保留的复杂原始定位器。"""
    normalized = normalize_locator_expression(locator)
    if not normalized:
        return False

    if _count_locator_chain_segments(locator) >= 2:
        return True

    return any(token in normalized for token in (".filter(", ".nth(", ".first()", ".last()"))


def extract_critical_raw_locator_chains(text: str) -> list[str]:
    """提取 raw_script 中需要显式保留的复杂定位器链。"""
    return [locator for locator in extract_raw_locator_chains(text) if _is_critical_raw_locator(locator)]


def _collect_llm_output_locators(llm_result: dict) -> list[str]:
    """汇总 LLM 输出中显式出现的定位器表达式。"""
    locators: list[str] = []

    spec_file = llm_result.get("spec_file") or {}
    if spec_file.get("content"):
        locators.extend(extract_raw_locator_chains(spec_file["content"]))

    for page_create in llm_result.get("page_creates", []):
        locators.extend(extract_raw_locator_chains(page_create.get("content", "")))

    for page_update in llm_result.get("page_updates", []):
        for locator_item in page_update.get("new_locators", []):
            definition = locator_item.get("definition", "")
            extracted = extract_raw_locator_chains(definition)
            if extracted:
                locators.extend(extracted)
                continue

            candidate = definition.strip()
            if candidate:
                if not candidate.startswith("page.") and not candidate.startswith("this.page."):
                    candidate = f"page.{candidate.lstrip('.')}"
                locators.append(candidate)

        for action_item in page_update.get("new_actions", []):
            locators.extend(extract_raw_locator_chains(action_item.get("content", "")))

        for action_item in page_update.get("extend_actions", []):
            locators.extend(extract_raw_locator_chains(action_item.get("content", "")))

    return _dedupe_keep_order(locators)


def validate_v1_critical_locator_coverage(raw_script: str, llm_result: dict) -> dict:
    """校验复杂原始定位器是否在 V1 输出中被显式保留，而不是被泛化成索引 helper。"""
    critical_raw_locators = extract_critical_raw_locator_chains(raw_script)
    output_locator_set = {
        normalize_locator_expression(locator)
        for locator in _collect_llm_output_locators(llm_result)
        if normalize_locator_expression(locator)
    }

    missing_critical_locators = [
        locator
        for locator in critical_raw_locators
        if normalize_locator_expression(locator) not in output_locator_set
    ]

    manual_review_items = [
        f"LLM 输出未显式保留 raw_script 中的复杂原始定位器，可能被错误泛化为索引或通用 helper: {locator}"
        for locator in missing_critical_locators
    ]

    return {
        "critical_raw_locators": critical_raw_locators,
        "critical_raw_locator_count": len(critical_raw_locators),
        "missing_critical_locators": missing_critical_locators,
        "manual_review_items": _dedupe_keep_order(manual_review_items),
    }


def validate_v1_locator_preservation(raw_script: str, llm_result: dict) -> dict:
    """校验 V1 输出是否严格保留了 raw_script 中的原始定位器。"""
    raw_locators = extract_raw_locator_chains(raw_script)
    raw_locator_set = {normalize_locator_expression(locator) for locator in raw_locators}

    manual_review_items: list[str] = []
    invalid_locators: list[dict] = []

    spec_file = llm_result.get("spec_file") or {}
    if spec_file.get("content"):
        source = f"spec_file({spec_file.get('path', 'unknown')})"
        _validate_locator_collection(
            source,
            extract_raw_locator_chains(spec_file["content"]),
            raw_locator_set,
            manual_review_items,
            invalid_locators,
        )

    for page_create in llm_result.get("page_creates", []):
        source = f"page_create({page_create.get('path', page_create.get('class_name', 'unknown'))})"
        _validate_locator_collection(
            source,
            extract_raw_locator_chains(page_create.get("content", "")),
            raw_locator_set,
            manual_review_items,
            invalid_locators,
        )

    for page_update in llm_result.get("page_updates", []):
        page_name = page_update.get("page_name", "unknown")

        for locator_item in page_update.get("new_locators", []):
            source = f"page_update({page_name}).new_locators[{locator_item.get('name', 'unknown')}]"
            _validate_definition_field(
                source,
                locator_item.get("definition", ""),
                raw_locator_set,
                manual_review_items,
                invalid_locators,
            )

        for action_item in page_update.get("new_actions", []):
            source = f"page_update({page_name}).new_actions[{action_item.get('name', 'unknown')}]"
            _validate_locator_collection(
                source,
                extract_raw_locator_chains(action_item.get("content", "")),
                raw_locator_set,
                manual_review_items,
                invalid_locators,
            )

        for action_item in page_update.get("extend_actions", []):
            source = f"page_update({page_name}).extend_actions[{action_item.get('name', 'unknown')}]"
            _validate_locator_collection(
                source,
                extract_raw_locator_chains(action_item.get("content", "")),
                raw_locator_set,
                manual_review_items,
                invalid_locators,
            )

    manual_review_items = _dedupe_keep_order(manual_review_items)
    return {
        "raw_locators": raw_locators,
        "raw_locator_count": len(raw_locators),
        "invalid_locators": invalid_locators,
        "manual_review_items": manual_review_items,
    }
