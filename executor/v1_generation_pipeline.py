"""
v1_generation_pipeline.py — V1 多文件生成管线

职责：
  1. 接收 refactor 请求 + project_scope
  2. 初始化项目工作区（幂等）
  3. 加载 page-registry，构建 LLM 上下文
  4. 组装 V1 System Prompt + User Prompt
  5. 调用 LLM 获取结构化 JSON 输出
  6. 解析输出：写入新页面、应用增量更新、更新 registry
  7. 重建 base.fixture.ts
  8. 返回结构化结果给 Go 后端
"""
import json
import logging
import os
from typing import Optional

from project_workspace import ProjectScope, resolve_project_scope, ensure_project_ready, compute_file_hash
from page_registry import PageRegistry
from fixture_builder import write_base_fixture
from raw_locator_guard import (
    validate_v1_critical_locator_coverage,
    validate_v1_locator_preservation,
    validate_v1_url_semantics_preservation,
)

logger = logging.getLogger(__name__)

# V1 prompt 模板路径
_PROMPTS_DIR = os.path.join(os.path.dirname(__file__), "pw_projects", "templates", "prompts")
_V1_PROMPT_TEMPLATE = os.path.join(_PROMPTS_DIR, "playwright-refactor-v1.md")


def run_v1_pipeline(
    scenario_desc: str,
    start_url: str,
    raw_script: str,
    step_model_json: Optional[dict],
    account_ref: Optional[str],
    project_scope_dict: dict,
) -> dict:
    """
    V1 多文件生成管线入口

    Args:
        scenario_desc: 场景描述
        start_url: 起始 URL
        raw_script: 原始录制脚本
        step_model_json: 步骤模型
        account_ref: 测试账号
        project_scope_dict: ProjectScope 字典

    Returns:
        结构化生成结果（兼容 Go 后端的 ExecutorGenerateResponse 格式）
    """
    logger.info("[v1-pipeline] 开始 V1 多文件生成管线")

    # 1. 解析 ProjectScope
    scope = resolve_project_scope(
        project_id=project_scope_dict["project_id"],
        project_key=project_scope_dict["project_key"],
        project_name=project_scope_dict.get("project_name", ""),
    )

    # 2. 确保项目工作区就绪
    ensure_project_ready(scope)
    logger.info(f"[v1-pipeline] 工作区就绪: {scope.workspace_root}")

    # 3. 加载 page-registry
    registry = PageRegistry(scope.registry_file)

    # 4. 构建 LLM 上下文
    registry_context = registry.build_llm_context()

    # 5. 组装 V1 prompt
    system_prompt = _build_v1_system_prompt(
        scope=scope,
        registry_context=registry_context,
        raw_script=raw_script,
        step_model_json=step_model_json,
        scenario_desc=scenario_desc,
    )

    user_prompt = _build_v1_user_prompt(
        scenario_desc=scenario_desc,
        start_url=start_url,
        raw_script=raw_script,
        step_model_json=step_model_json,
        account_ref=account_ref,
    )

    # 6. 调用 LLM
    llm_result = _call_v1_llm(system_prompt, user_prompt)

    if not llm_result:
        return {
            "success": False,
            "script_content": "",
            "generation_summary": "V1 生成失败：LLM 返回为空",
            "error_message": "V1 LLM 调用失败",
        }

    locator_guard_result = validate_v1_locator_preservation(raw_script=raw_script, llm_result=llm_result)
    url_guard_result = validate_v1_url_semantics_preservation(raw_script=raw_script, llm_result=llm_result)
    critical_locator_coverage_result = validate_v1_critical_locator_coverage(
        raw_script=raw_script,
        llm_result=llm_result,
    )

    if locator_guard_result.get("manual_review_items"):
        llm_result["manual_review_items"] = _dedupe_items(
            llm_result.get("manual_review_items", []) + locator_guard_result["manual_review_items"]
        )
        llm_result["risk_hints"] = _dedupe_items(
            llm_result.get("risk_hints", []) + [
                f"检测到 {len(locator_guard_result.get('invalid_locators', []))} 处原始定位器保留风险，已标记人工审核"
            ]
        )
        llm_result["locator_guard_result"] = locator_guard_result
        logger.warning(
            "[v1-pipeline] 原始定位器守卫发现 %s 个问题，已转人工审核",
            len(locator_guard_result["manual_review_items"]),
        )
    if url_guard_result.get("manual_review_items"):
        llm_result["manual_review_items"] = _dedupe_items(
            llm_result.get("manual_review_items", []) + url_guard_result["manual_review_items"]
        )
        llm_result["risk_hints"] = _dedupe_items(
            llm_result.get("risk_hints", []) + [
                f"检测到 {len(url_guard_result.get('invalid_url_checks', []))} 处 URL 等待/断言保留风险，已标记人工审核"
            ]
        )
        llm_result["url_guard_result"] = url_guard_result
        logger.warning(
            "[v1-pipeline] URL 语义守卫发现 %s 个问题，已转人工审核",
            len(url_guard_result["manual_review_items"]),
        )
    if critical_locator_coverage_result.get("manual_review_items"):
        llm_result["manual_review_items"] = _dedupe_items(
            llm_result.get("manual_review_items", []) + critical_locator_coverage_result["manual_review_items"]
        )
        llm_result["risk_hints"] = _dedupe_items(
            llm_result.get("risk_hints", []) + [
                f"检测到 {len(critical_locator_coverage_result.get('missing_critical_locators', []))} 处复杂原始定位器未被显式保留，已标记人工审核"
            ]
        )
        llm_result["critical_locator_coverage_result"] = critical_locator_coverage_result
        logger.warning(
            "[v1-pipeline] 复杂原始定位器覆盖校验发现 %s 个问题，已转人工审核",
            len(critical_locator_coverage_result["manual_review_items"]),
        )
    if locator_guard_result.get("invalid_locators"):
        # 严格模式下，只要发现 LLM 输出改写了录制稿里的原始定位器，就直接拦截，
        # 避免把不可信的 Page Object / spec 落盘到项目工作区。
        return {
            "success": False,
            "script_content": "",
            "generation_summary": "V1 生成失败：原始定位器守卫拦截了被改写的定位器输出",
            "error_message": "检测到 LLM 输出未严格保留录制脚本中的原始定位器，已阻止写入工作区",
            "risk_hints": llm_result.get("risk_hints", []),
            "manual_review_items": llm_result.get("manual_review_items", []),
            "locator_guard_result": locator_guard_result,
        }
    if url_guard_result.get("invalid_url_checks"):
        # 同样地，如果 LLM 凭空发明了 raw_script 中没有出现过的固定 URL ready 断言，
        # 说明它把业务路由假设硬编码进了生成结果，也必须直接拦截。
        return {
            "success": False,
            "script_content": "",
            "generation_summary": "V1 生成失败：URL 语义守卫拦截了凭空发明的页面 URL 等待/断言",
            "error_message": "检测到 LLM 输出加入了录制脚本中不存在的固定 URL 等待或断言，已阻止写入工作区",
            "risk_hints": llm_result.get("risk_hints", []),
            "manual_review_items": llm_result.get("manual_review_items", []),
            "locator_guard_result": locator_guard_result,
            "url_guard_result": url_guard_result,
        }

    # 7. 处理 LLM 输出
    result = _process_v1_output(scope, registry, llm_result)

    # 8. 重建 base.fixture.ts
    try:
        _, fixture_hash = write_base_fixture(registry, scope.workspace_root)
        result["base_fixture_hash"] = fixture_hash
    except Exception as e:
        logger.error(f"[v1-pipeline] 重建 base.fixture 失败: {e}")
        result.setdefault("manual_review_items", []).append(
            f"base.fixture.ts 重建失败: {e}"
        )

    # 9. 构建兼容返回
    return _build_response(scope, registry, result, llm_result)


def _build_v1_system_prompt(
    scope: ProjectScope,
    registry_context: dict,
    raw_script: str,
    step_model_json: Optional[dict],
    scenario_desc: str,
) -> str:
    """组装 V1 system prompt"""
    template = _load_prompt_template()
    if not template:
        logger.error("[v1-pipeline] V1 prompt 模板加载失败")
        return ""

    # 替换占位符
    prompt = template
    prompt = prompt.replace("{{PROJECT_SCOPE}}", json.dumps(scope.to_dict(), ensure_ascii=False, indent=2))
    prompt = prompt.replace("{{PAGE_REGISTRY}}", json.dumps(registry_context, ensure_ascii=False, indent=2))
    prompt = prompt.replace("{{RAW_SCRIPT}}", raw_script)
    prompt = prompt.replace("{{STEP_MODEL}}", json.dumps(step_model_json or {}, ensure_ascii=False, indent=2))
    prompt = prompt.replace("{{SCENARIO_DESC}}", scenario_desc)

    return prompt


def _build_v1_user_prompt(
    scenario_desc: str,
    start_url: str,
    raw_script: str,
    step_model_json: Optional[dict],
    account_ref: Optional[str],
) -> str:
    """构建 user prompt"""
    parts = [
        "请根据以上规则，将原始录制脚本重构为 V1 工程化输出。",
        "严格约束：LLM 只允许做 POM 结构化组织，不允许修改、简化、替换任何原始定位器。",
        "如果需要新增 locator 定义，必须直接复用 raw_script 中已出现过的完整 locator chain。",
        "所有新增类、方法和关键逻辑都必须补充中文注释或中文 JSDoc。",
        f"\n场景描述: {scenario_desc}",
        f"起始 URL: {start_url}",
    ]
    if account_ref:
        parts.append(f"测试账号参考: {account_ref}")

    return "\n".join(parts)


def _load_prompt_template() -> Optional[str]:
    """加载 V1 prompt 模板文件"""
    if not os.path.exists(_V1_PROMPT_TEMPLATE):
        logger.error(f"[v1-pipeline] 模板文件不存在: {_V1_PROMPT_TEMPLATE}")
        return None
    with open(_V1_PROMPT_TEMPLATE, "r", encoding="utf-8") as f:
        return f.read()


def _call_v1_llm(system_prompt: str, user_prompt: str) -> Optional[dict]:
    """调用 LLM 并解析 JSON 输出"""
    import re
    import time
    from config import OPENAI_BASE_URL, OPENAI_API_KEY, OPENAI_MODEL
    from openai import OpenAI

    client = OpenAI(
        base_url=OPENAI_BASE_URL,
        api_key=OPENAI_API_KEY,
    )

    max_retries = 3
    last_error = None
    content = ""

    for attempt in range(max_retries):
        try:
            response = client.chat.completions.create(
                model=OPENAI_MODEL,
                messages=[
                    {"role": "system", "content": system_prompt},
                    {"role": "user", "content": user_prompt},
                ],
                temperature=0.1,
                max_tokens=16384,
            )

            content = response.choices[0].message.content.strip()

            # 尝试解析 JSON
            result = json.loads(content)
            logger.info(f"[v1-pipeline] LLM 返回解析成功, 包含 {len(result.get('page_creates', []))} 个新页面")
            return result

        except json.JSONDecodeError as e:
            last_error = e
            logger.warning(f"[v1-pipeline] LLM 返回非法 JSON (attempt {attempt + 1}): {e}")
            # 尝试提取 JSON
            try:
                json_match = re.search(r'\{[\s\S]*\}', content)
                if json_match:
                    result = json.loads(json_match.group())
                    return result
            except Exception:
                pass

        except Exception as e:
            last_error = e
            logger.warning(f"[v1-pipeline] LLM 调用失败 (attempt {attempt + 1}): {e}")

        if attempt < max_retries - 1:
            time.sleep(2 ** attempt)

    logger.error(f"[v1-pipeline] LLM 调用最终失败: {last_error}")
    return None


def _process_v1_output(scope: ProjectScope, registry: PageRegistry, llm_result: dict) -> dict:
    """
    处理 LLM JSON 输出：
    1. 写入新创建的页面文件
    2. 应用页面增量更新（append_locator / append_action / extend_action）
    3. 更新 page-registry
    4. 返回处理结果
    """
    result = {
        "files_created": [],
        "files_updated": [],
        "manual_review_items": _dedupe_items(llm_result.get("manual_review_items", [])),
        "risk_hints": _dedupe_items(llm_result.get("risk_hints", [])),
        "generation_summary": llm_result.get("generation_summary", ""),
    }

    # ── 处理 page_creates ──
    for page_create in llm_result.get("page_creates", []):
        path = page_create.get("path", "")
        content = page_create.get("content", "")
        class_name = page_create.get("class_name", "")

        if not path or not content:
            result["manual_review_items"].append(f"page_create 缺少 path 或 content: {class_name}")
            continue

        # 路径安全校验
        if not scope.validate_path(path):
            result["manual_review_items"].append(f"路径逃逸被阻止: {path}")
            continue

        full_path = os.path.join(scope.workspace_root, path)
        os.makedirs(os.path.dirname(full_path), exist_ok=True)

        # ── A+B 防护：文件已存在时禁止覆盖 ──
        if os.path.exists(full_path):
            logger.warning(
                f"[v1-pipeline] 页面文件已存在，跳过 create_page 覆盖: {path} ({class_name})。"
                f"LLM 应使用 page_updates (append_locator/append_action) 进行增量更新。"
            )
            result["manual_review_items"].append(
                f"页面 {class_name} 已存在（{path}），create_page 被拦截，未覆盖。"
                f"新增的 locator/action 需通过 page_updates 增量合并。"
            )
            # 将 LLM create_page 中可能包含的新方法提取为 page_updates 候选
            # 这样 AST merger 可以增量追加，而不是整文件覆盖
            result.setdefault("skipped_creates", []).append({
                "path": path,
                "class_name": class_name,
                "content": content,
            })
            continue

        # 文件不存在时正常创建
        with open(full_path, "w", encoding="utf-8") as f:
            f.write(content)

        result["files_created"].append({
            "relative_path": path,
            "file_type": "page",
            "class_name": class_name,
            "content_hash": compute_file_hash(content),
            "content": content,
        })
        logger.info(f"[v1-pipeline] 创建页面: {path}")

    # ── 处理 spec_file ──
    spec_file = llm_result.get("spec_file")
    if spec_file and spec_file.get("path") and spec_file.get("content"):
        spec_path = spec_file["path"]
        spec_content = spec_file["content"]

        if scope.validate_path(spec_path):
            full_path = os.path.join(scope.workspace_root, spec_path)
            os.makedirs(os.path.dirname(full_path), exist_ok=True)
            with open(full_path, "w", encoding="utf-8") as f:
                f.write(spec_content)

            result["files_created"].append({
                "relative_path": spec_path,
                "file_type": "spec",
                "content_hash": compute_file_hash(spec_content),
                "content": spec_content,
            })
            result["spec_file"] = spec_file
            logger.info(f"[v1-pipeline] 创建 spec: {spec_path}")
        else:
            result["manual_review_items"].append(f"spec 路径逃逸被阻止: {spec_path}")

    # ── 处理 page_updates（AST 增量合并）──
    page_updates = llm_result.get("page_updates", [])
    merge_eligible = []
    for page_update in page_updates:
        page_name = page_update.get("page_name", "")
        operation = page_update.get("operation", "")

        if not page_name:
            continue

        # 检查是否需要 manual_review
        if operation == "manual_review":
            result["manual_review_items"].append(
                f"页面 {page_name} 需要人工审核: {page_update.get('reason', '未指定原因')}"
            )
            continue

        merge_eligible.append(page_update)

    # 调用 AST 合并器
    if merge_eligible:
        try:
            from ast_merger_bridge import apply_page_updates
            merge_result = apply_page_updates(
                workspace_root=scope.workspace_root,
                page_updates=merge_eligible,
                registry_data=registry.snapshot(),
            )

            if merge_result.get("merged_files"):
                for mf in merge_result["merged_files"]:
                    result["files_updated"].append({
                        "relative_path": mf["file_path"],
                        "file_type": "page",
                        "operations": mf["operations_applied"],
                        "content_hash": mf["content_hash"],
                        "content": mf["content"],
                    })

            if merge_result.get("errors"):
                result["manual_review_items"].extend(merge_result["errors"])

            if merge_result.get("warnings"):
                result["risk_hints"].extend(merge_result["warnings"])

            logger.info(f"[v1-pipeline] AST 合并完成: {len(merge_result.get('merged_files', []))} 文件")

        except Exception as e:
            logger.error(f"[v1-pipeline] AST 合并器调用失败: {e}", exc_info=True)
            # 降级：将所有 update 标记为 manual_review
            for pu in merge_eligible:
                result["manual_review_items"].append(
                    f"AST 合并失败，请手动处理: {pu.get('page_name')} ({pu.get('operation')})"
                )
            result["files_updated"] = [
                {"page_name": pu.get("page_name"), "operation": pu.get("operation"), "update_data": pu}
                for pu in merge_eligible
            ]

    # ── 更新 registry ──
    registry_updates = llm_result.get("registry_updates", {})
    if registry_updates:
        registry.batch_update(registry_updates)
        logger.info(f"[v1-pipeline] Registry 更新: {len(registry_updates)} 页面")

    result["manual_review_items"] = _dedupe_items(result.get("manual_review_items", []))
    result["risk_hints"] = _dedupe_items(result.get("risk_hints", []))
    return result


def _dedupe_items(items: list[str]) -> list[str]:
    """
    对人工审核项和风险提示去重，避免同一条信息在多个阶段重复返回。
    """
    seen: set[str] = set()
    result: list[str] = []
    for item in items or []:
        if not item:
            continue
        normalized = item.strip()
        if not normalized or normalized in seen:
            continue
        seen.add(normalized)
        result.append(normalized)
    return result


def _build_response(
    scope: ProjectScope,
    registry: PageRegistry,
    process_result: dict,
    llm_result: dict,
) -> dict:
    """构建兼容 Go 后端的返回格式"""
    # 生成 script_content（向后兼容：取 spec 文件内容）
    spec_file = llm_result.get("spec_file", {})
    script_content = spec_file.get("content", "")

    # 判定是否需要 manual_review
    has_manual_review = len(process_result.get("manual_review_items", [])) > 0

    # V1 多文件结果（白名单过滤 LLM 输出，只传需要的字段）
    _PAGE_CREATE_KEYS = {"path", "class_name", "content"}
    _PAGE_UPDATE_KEYS = {"page_name", "operation", "new_locators", "new_actions", "extend_actions", "reason"}

    safe_page_creates = [
        {k: v for k, v in pc.items() if k in _PAGE_CREATE_KEYS}
        for pc in llm_result.get("page_creates", [])
    ]
    safe_page_updates = [
        {k: v for k, v in pu.items() if k in _PAGE_UPDATE_KEYS}
        for pu in llm_result.get("page_updates", [])
    ]

    return {
        "success": True,
        "script_content": script_content,  # 向后兼容
        "generation_summary": process_result.get("generation_summary", ""),
        "risk_hints": process_result.get("risk_hints", []),
        "assertion_suggestions": [],

        # V1 多文件结果
        "spec_file": spec_file,
        "page_creates": safe_page_creates,
        "page_updates": safe_page_updates,
        "registry_updates": llm_result.get("registry_updates"),
        "manual_review_items": process_result.get("manual_review_items", []),

        # V1 元数据
        "project_key_snapshot": scope.project_key,
        "workspace_root_snapshot": scope.workspace_root,
        "registry_snapshot": registry.snapshot(),
        "base_fixture_hash": process_result.get("base_fixture_hash", ""),
        "version_status": "MANUAL_REVIEW_REQUIRED" if has_manual_review else "GENERATED",
        "files_created": process_result.get("files_created", []),
        "files_updated": process_result.get("files_updated", []),
    }
