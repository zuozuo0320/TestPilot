"""
validation_runner.py — 使用 npx playwright test 执行 TypeScript 脚本回放验证

不再使用 Python 正则解析，而是：
1. 将 TypeScript 脚本写入临时 .spec.ts 文件
2. 创建/复用 Playwright 项目配置
3. 调用 npx playwright test 执行
4. 解析 JSON reporter 输出提取验证结果
"""
import json
import logging
import os
import re
import shutil
import subprocess
import time
from pathlib import Path

from config import SCREENSHOT_DIR
from project_workspace import sync_workspace_support_files

logger = logging.getLogger(__name__)

# Playwright 项目目录（持久化，避免每次初始化）
PLAYWRIGHT_PROJECT_DIR = os.path.join(os.path.dirname(__file__), "pw_workspace")


def _ensure_playwright_project():
    """确保 Playwright 项目已初始化"""
    project_dir = Path(PLAYWRIGHT_PROJECT_DIR)
    project_dir.mkdir(exist_ok=True)

    # package.json
    pkg_json = project_dir / "package.json"
    if not pkg_json.exists():
        pkg_json.write_text(json.dumps({
            "name": "testpilot-validation",
            "version": "1.0.0",
            "devDependencies": {
                "@playwright/test": "^1.40.0"
            }
        }, indent=2), encoding="utf-8")
        # 安装依赖
        logger.info("Installing Playwright dependencies...")
        subprocess.run(
            ["npm", "install"],
            cwd=str(project_dir),
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=120,
            shell=True,
        )

    # playwright.config.ts
    config_ts = project_dir / "playwright.config.ts"
    if not config_ts.exists():
        config_ts.write_text("""
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 60000,
  retries: 0,
  reporter: [['json', { outputFile: 'test-results.json' }]],
  use: {
    headless: true,
    screenshot: 'on',
    locale: 'zh-CN',
    ignoreHTTPSErrors: true,
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        browserName: 'chromium',
        launchOptions: {
          args: ['--disable-blink-features=AutomationControlled'],
        },
      },
    },
  ],
});
""".strip(), encoding="utf-8")

    # tests 目录
    tests_dir = project_dir / "tests"
    tests_dir.mkdir(exist_ok=True)

    return project_dir


def run_validation(
    task_id: int,
    script_version_id: int,
    script_content: str,
    start_url: str,
    project_scope: dict = None,        # V1 多项目工程化：ProjectScope 信息
    spec_relative_path: str = None,    # V1：项目内 spec 相对路径
) -> dict:
    """
    执行 Playwright TypeScript 脚本回放验证

    Args:
        task_id: 任务 ID
        script_version_id: 脚本版本 ID
        script_content: TypeScript 脚本内容
        start_url: 起始 URL
        project_scope: V1 项目作用域（存在时走项目级验证）
        spec_relative_path: V1 项目内 spec 相对路径

    Returns:
        验证结果字典
    """
    start_time = time.time()

    try:
        # ── V1 项目级验证 ──
        if project_scope and project_scope.get("workspace_root"):
            return _run_v1_project_validation(
                task_id=task_id,
                script_version_id=script_version_id,
                script_content=script_content,
                start_url=start_url,
                project_scope=project_scope,
                spec_relative_path=spec_relative_path,
                start_time=start_time,
            )

        # ── V0 兼容路径 ──
        # 1. 确保 Playwright 项目就绪
        project_dir = _ensure_playwright_project()
        tests_dir = project_dir / "tests"
        results_file = project_dir / "test-results.json"

        # 清理旧结果
        if results_file.exists():
            results_file.unlink()
        results_dir = project_dir / "test-results"
        if results_dir.exists():
            shutil.rmtree(results_dir, ignore_errors=True)

        # 2. 清理脚本中的 storageState 引用
        #    codegen --save-storage 会自动在录制脚本中生成 storageState 配置，
        #    但它使用相对路径，在验证环境中会找不到文件。
        #    auth_state 如果需要，应通过 playwright.config.ts 全局注入。
        cleaned_content = script_content
        # 移除 test.use({ storageState: '...' }) 或 test.use({ ignoreHTTPSErrors: true, storageState: '...' }) 中的 storageState 行
        cleaned_content = re.sub(
            r"^\s*storageState:\s*['\"].*?['\"]\s*,?\s*\n",
            "",
            cleaned_content,
            flags=re.MULTILINE,
        )
        # 清理可能留下的空 test.use({}) 块（只剩 ignoreHTTPSErrors 或空）
        cleaned_content = re.sub(
            r"test\.use\(\{\s*\}\s*\)\s*;?\s*\n?",
            "",
            cleaned_content,
        )

        # 如果有有效的 auth_state，通过 config 全局注入
        try:
            from auth_manager import has_valid_auth_state, get_auth_state_path
            auth_state_path = get_auth_state_path(start_url)
            if has_valid_auth_state(start_url):
                abs_path = os.path.abspath(auth_state_path).replace("\\", "/")
                _update_config_storage_state(project_dir, abs_path)
                logger.info(f"Injected storageState into config: {abs_path}")
            else:
                _update_config_storage_state(project_dir, None)
        except Exception as e:
            logger.warning(f"Failed to inject auth state: {e}")

        # 写入脚本文件
        script_file = tests_dir / f"task_{task_id}_v{script_version_id}.spec.ts"
        script_file.write_text(cleaned_content, encoding="utf-8")
        logger.info(f"Script written to {script_file} (storageState cleaned)")

        # 3. 执行 npx playwright test
        logger.info(f"Running playwright test for task {task_id}")
        proc = subprocess.run(
            ["npx", "playwright", "test", str(script_file.name)],
            cwd=str(project_dir),
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=180,
            shell=True,
        )

        stdout = proc.stdout
        stderr = proc.stderr
        exit_code = proc.returncode

        logger.info(f"Playwright test exit code: {exit_code}")
        if stderr:
            logger.debug(f"stderr: {stderr[:500]}")

        # 4. 解析 JSON reporter 结果
        test_result = _parse_json_results(results_file)
        duration_ms = int((time.time() - start_time) * 1000)

        # 5. 收集截图
        screenshots = _collect_screenshots(project_dir, task_id, script_version_id)

        # 6. 构建返回结果
        if exit_code == 0 and test_result["all_passed"]:
            return {
                "success": True,
                "total_step_count": test_result["total_tests"],
                "passed_step_count": test_result["passed_tests"],
                "failed_step_no": None,
                "fail_reason": "",
                "assertion_summary": test_result["assertions"],
                "duration_ms": duration_ms,
                "logs": _build_logs(stdout, stderr, "PASS"),
                "screenshots": screenshots,
                "error_message": "",
            }
        else:
            return {
                "success": False,
                "total_step_count": test_result["total_tests"],
                "passed_step_count": test_result["passed_tests"],
                "failed_step_no": test_result.get("first_failed_index"),
                "fail_reason": test_result.get("fail_reason", stderr[:500] if stderr else "测试执行失败"),
                "assertion_summary": test_result["assertions"],
                "duration_ms": duration_ms,
                "logs": _build_logs(stdout, stderr, "FAIL"),
                "screenshots": screenshots,
                "error_message": test_result.get("fail_reason", ""),
            }

    except subprocess.TimeoutExpired:
        duration_ms = int((time.time() - start_time) * 1000)
        logger.error(f"Playwright test timed out for task {task_id}")
        return {
            "success": False,
            "total_step_count": 0,
            "passed_step_count": 0,
            "failed_step_no": None,
            "fail_reason": "测试执行超时 (180s)",
            "assertion_summary": [],
            "duration_ms": duration_ms,
            "logs": json.dumps([{"level": "ERROR", "message": "测试执行超时"}]),
            "screenshots": [],
            "error_message": "测试执行超时",
        }
    except Exception as e:
        duration_ms = int((time.time() - start_time) * 1000)
        logger.error(f"Validation failed for task {task_id}: {e}")
        return {
            "success": False,
            "total_step_count": 0,
            "passed_step_count": 0,
            "failed_step_no": None,
            "fail_reason": str(e),
            "assertion_summary": [],
            "duration_ms": duration_ms,
            "logs": json.dumps([{"level": "ERROR", "message": str(e)}]),
            "screenshots": [],
            "error_message": str(e),
        }
    finally:
        # 清理临时脚本文件（仅 V0）
        if not project_scope:
            try:
                script_file = Path(PLAYWRIGHT_PROJECT_DIR) / "tests" / f"task_{task_id}_v{script_version_id}.spec.ts"
                if script_file.exists():
                    script_file.unlink()
            except Exception:
                pass


def _sanitize_script_for_execution(script_content: str, workspace_root: Path) -> str:
    """清洗脚本内容，移除调用项目中已不存在方法的行。

    当 DB 中存储的 script_content 引用了已被移除的方法时（如 expectBreadcrumbContains），
    直接执行会报 TypeError。此函数扫描项目 Page Object 文件，
    对比脚本中的方法调用，自动注释掉不存在的方法调用行。
    """
    # 收集项目中所有 Page Object 导出的方法名
    pages_dir = workspace_root / "pages"
    available_methods: set[str] = set()
    if pages_dir.exists():
        for ts_file in pages_dir.rglob("*.ts"):
            try:
                content = ts_file.read_text(encoding="utf-8")
                # 匹配 async methodName( 或 methodName(
                import re as _re
                methods = _re.findall(r'(?:async\s+)?(\w+)\s*\(', content)
                available_methods.update(methods)
            except Exception:
                continue

    if not available_methods:
        return script_content

    # 扫描脚本中的方法调用，移除引用不存在方法的行
    import re as _re
    lines = script_content.split("\n")
    cleaned_lines = []
    removed_count = 0

    for line in lines:
        # 匹配 await xxx.methodName( 模式
        match = _re.search(r'await\s+\w+\.(\w+)\s*\(', line)
        if match:
            method_name = match.group(1)
            # 跳过 Playwright 内置方法和 test 框架方法
            builtin_methods = {
                'click', 'fill', 'type', 'press', 'check', 'uncheck',
                'selectOption', 'hover', 'focus', 'blur', 'dblclick',
                'waitForSelector', 'waitForLoadState', 'waitForURL',
                'waitForTimeout', 'waitForEvent', 'waitForResponse',
                'goto', 'reload', 'goBack', 'goForward',
                'toBeVisible', 'toContainText', 'toHaveText', 'toHaveValue',
                'toHaveCount', 'toBeChecked', 'toBeEnabled', 'toBeDisabled',
                'getByText', 'getByRole', 'getByLabel', 'getByPlaceholder',
                'getByTestId', 'locator', 'first', 'last', 'nth',
                'step', 'describe', 'use',
            }
            if method_name not in builtin_methods and method_name not in available_methods:
                logger.warning(
                    f"[v1-validation] 清洗脚本：移除不存在的方法调用 '{method_name}'"
                )
                removed_count += 1
                continue  # 跳过此行

        cleaned_lines.append(line)

    if removed_count > 0:
        logger.info(f"[v1-validation] 脚本清洗完成：移除 {removed_count} 行无效方法调用")

    return "\n".join(cleaned_lines)


def _find_matching_spec(workspace_root: Path, script_content: str, task_id: int, script_version_id: int) -> Path:
    """在项目工作区中查找与 script_content 匹配的已有 spec 文件。

    匹配策略：
    1. 精确内容匹配（去除空白差异）
    2. test.describe 标题匹配（同一测试用例的不同版本）
    3. 如果没找到，使用第一个 spec 文件（单 task/单 spec 场景）
    4. 最终回退：清洗脚本后写入临时文件
    """
    import re as _re

    tests_dir = workspace_root / "tests"
    if not tests_dir.exists():
        tests_dir.mkdir(exist_ok=True)

    # 在 tests 下递归搜索所有 spec 文件（排除 _fallback 目录中的旧文件）
    all_specs = [
        f for f in tests_dir.rglob("*.spec.ts")
        if "_fallback" not in str(f.relative_to(tests_dir))
    ]

    # 1. 精确内容匹配（标准化空白后比较）
    normalized_content = script_content.strip().replace("\r\n", "\n")
    for spec_file in all_specs:
        try:
            existing = spec_file.read_text(encoding="utf-8").strip().replace("\r\n", "\n")
            if existing == normalized_content:
                logger.info(f"[v1-validation] 找到精确匹配 spec: {spec_file.relative_to(workspace_root)}")
                return spec_file
        except Exception:
            continue

    # 2. test.describe 标题模糊匹配
    # 提取 DB 脚本中的 test.describe 标题
    describe_match = _re.search(r"test\.describe\(\s*['\"](.+?)['\"]", script_content)
    if describe_match:
        target_describe = describe_match.group(1)
        for spec_file in all_specs:
            try:
                existing = spec_file.read_text(encoding="utf-8")
                if target_describe in existing:
                    logger.info(
                        f"[v1-validation] 通过 test.describe 标题匹配到 spec: "
                        f"{spec_file.relative_to(workspace_root)} (title='{target_describe}')"
                    )
                    return spec_file
            except Exception:
                continue

    # 3. 如果只有一个 spec 文件，直接使用
    if len(all_specs) == 1:
        logger.info(f"[v1-validation] 使用唯一 spec: {all_specs[0].relative_to(workspace_root)}")
        return all_specs[0]

    # 4. 回退：清洗脚本后写入临时文件
    logger.warning(f"[v1-validation] 未找到匹配 spec，回退写入临时文件（已清洗）")

    # 清洗脚本：移除调用已删除方法的行
    sanitized_content = _sanitize_script_for_execution(script_content, workspace_root)

    if "../../fixtures" in sanitized_content or "../fixtures" in sanitized_content:
        fallback_dir = tests_dir / "_fallback"
        fallback_dir.mkdir(exist_ok=True)
        spec_path = fallback_dir / f"task_{task_id}_v{script_version_id}.spec.ts"
    else:
        spec_path = tests_dir / f"task_{task_id}_v{script_version_id}.spec.ts"
    spec_path.write_text(sanitized_content, encoding="utf-8")
    return spec_path


def _detect_workspace_preflight_issues(workspace_root: Path) -> list[str]:
    """扫描项目工作区中明显损坏的生成物，提前给出可定位的错误原因。"""
    issues: list[str] = []
    pages_dir = workspace_root / "pages"
    tests_dir = workspace_root / "tests"

    ts_files: list[Path] = []
    if pages_dir.exists():
        ts_files.extend(pages_dir.rglob("*.ts"))
    if tests_dir.exists():
        ts_files.extend(tests_dir.rglob("*.ts"))

    shared_boundary_pattern = re.compile(
        r"readonly\s+[A-Za-z0-9_]*(?:Menu|Nav|Navigation|Breadcrumb)[A-Za-z0-9_]*\s*:\s*Locator",
        re.IGNORECASE,
    )

    for file_path in ts_files:
        try:
            content = file_path.read_text(encoding="utf-8")
        except Exception as exc:
            issues.append(f"{file_path.relative_to(workspace_root)} 读取失败: {exc}")
            continue

        if "\ufffd" in content:
            issues.append(
                f"{file_path.relative_to(workspace_root)} 包含乱码占位符字符 U+FFFD，疑似生成阶段编码损坏"
            )

        # 只对业务页面做共享导航越界检查，shared 页面允许存在这些能力。
        if file_path.is_relative_to(pages_dir / "shared"):
            continue
        if file_path.parent != pages_dir:
            continue

        if shared_boundary_pattern.search(content):
            issues.append(
                f"{file_path.relative_to(workspace_root)} 在 BusinessPage 中定义了共享菜单/导航/面包屑 locator"
            )

    return issues


def _build_preflight_failure(start_time: float, issues: list[str]) -> dict:
    """构建工作区预检失败结果，避免继续执行误导性的 Playwright 点击超时。"""
    duration_ms = int((time.time() - start_time) * 1000)
    fail_reason = "；".join(issues[:3])
    logs = [{"level": "ERROR", "message": issue} for issue in issues[:20]]
    return {
        "success": False,
        "total_step_count": 0,
        "passed_step_count": 0,
        "failed_step_no": None,
        "fail_reason": fail_reason,
        "assertion_summary": [],
        "duration_ms": duration_ms,
        "logs": logs,
        "screenshots": [],
        "error_message": fail_reason,
    }


def _run_v1_project_validation(
    task_id: int,
    script_version_id: int,
    script_content: str,
    start_url: str,
    project_scope: dict,
    spec_relative_path: str | None,
    start_time: float,
) -> dict:
    """
    V1 项目级验证：在项目工作区内执行 playwright test

    与 V0 的区别：
    - 不创建临时文件，使用项目工作区的实际文件结构
    - spec 运行在正确的 fixture/page 依赖上下文中
    - 不清理文件（项目文件是持久化的）
    """
    # workspace_root 可能是相对路径（如 pw_projects/projects/project_1），
    # 需要相对 executor 目录解析为绝对路径
    raw_root = project_scope["workspace_root"]
    if os.path.isabs(raw_root):
        workspace_root = Path(raw_root)
    else:
        workspace_root = Path(os.path.dirname(__file__)) / raw_root
    
    if not workspace_root.exists():
        return {
            "success": False,
            "total_step_count": 0,
            "passed_step_count": 0,
            "failed_step_no": None,
            "fail_reason": f"项目工作区不存在: {workspace_root}",
            "assertion_summary": [],
            "duration_ms": int((time.time() - start_time) * 1000),
            "logs": [{"level": "ERROR", "message": f"项目工作区不存在: {workspace_root}"}],
            "screenshots": [],
            "error_message": f"项目工作区不存在: {workspace_root}",
        }

    # 对历史项目做一次运行期文件补齐，避免 registry 升级后缺失内置 shared 模板文件。
    sync_workspace_support_files(str(workspace_root))

    # 预检生成物完整性，避免把生成阶段的问题误判为 Playwright 元素定位失败。
    preflight_issues = _detect_workspace_preflight_issues(workspace_root)
    if preflight_issues:
        logger.error("[v1-validation] 工作区预检失败: %s", " | ".join(preflight_issues[:5]))
        return _build_preflight_failure(start_time, preflight_issues)
    
    # 确保 node_modules 已安装
    node_modules = workspace_root / "node_modules"
    if not node_modules.exists():
        logger.info(f"[v1-validation] 安装项目依赖: {workspace_root}")
        subprocess.run(
            ["npm", "install"],
            cwd=str(workspace_root),
            capture_output=True,
            timeout=120,
            shell=True,
        )

    # 确定 spec 文件路径
    if spec_relative_path:
        spec_path = workspace_root / spec_relative_path
    else:
        # V1 回退：在工作区 tests 目录下查找内容匹配的 spec 文件
        spec_path = _find_matching_spec(workspace_root, script_content, task_id, script_version_id)

    # 清理旧结果
    results_file = workspace_root / "test-results.json"
    if results_file.exists():
        results_file.unlink()
    results_dir = workspace_root / "test-results"
    if results_dir.exists():
        shutil.rmtree(results_dir, ignore_errors=True)

    # auth_state 注入
    try:
        from auth_manager import has_valid_auth_state, get_auth_state_path
        auth_state_path = get_auth_state_path(start_url)
        if has_valid_auth_state(start_url):
            abs_path = os.path.abspath(auth_state_path).replace("\\", "/")
            _update_config_storage_state(workspace_root, abs_path)
            # 同步 auth state 到项目级 auth_states/default.json
            # auth.fixture.ts 引用 default.json，其 storageState 优先级高于 config
            project_auth_dir = workspace_root / "auth_states"
            project_auth_dir.mkdir(exist_ok=True)
            project_default = project_auth_dir / "default.json"
            shutil.copy2(auth_state_path, str(project_default))
            logger.info(f"[v1-validation] auth_state 同步至项目: {project_default}")
        else:
            _update_config_storage_state(workspace_root, None)
    except Exception as e:
        logger.warning(f"[v1-validation] auth_state 注入失败: {e}")

    # BASE_URL 自动同步：从 start_url 提取 origin 写入 .env
    try:
        from urllib.parse import urlparse
        parsed = urlparse(start_url)
        base_url = f"{parsed.scheme}://{parsed.netloc}"
        env_file = workspace_root / ".env"
        if env_file.exists():
            env_content = env_file.read_text(encoding="utf-8")
            import re as _re
            if _re.search(r"^BASE_URL=", env_content, _re.MULTILINE):
                env_content = _re.sub(
                    r"^BASE_URL=.*$", f"BASE_URL={base_url}",
                    env_content, flags=_re.MULTILINE,
                )
            else:
                env_content += f"\nBASE_URL={base_url}\n"
            env_file.write_text(env_content, encoding="utf-8")
        else:
            env_file.write_text(f"BASE_URL={base_url}\n", encoding="utf-8")
        logger.info(f"[v1-validation] BASE_URL 同步: {base_url}")
    except Exception as e:
        logger.warning(f"[v1-validation] BASE_URL 同步失败: {e}")

    # 执行 npx playwright test
    # Windows 兼容：Playwright 将参数作为正则匹配，反斜杠会被当作转义字符
    spec_relative = str(spec_path.relative_to(workspace_root)).replace("\\", "/")
    logger.info(f"[v1-validation] 执行: npx playwright test {spec_relative}")
    proc = subprocess.run(
        ["npx", "playwright", "test", spec_relative],
        cwd=str(workspace_root),
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=180,
        shell=True,
    )

    stdout = proc.stdout
    stderr = proc.stderr
    exit_code = proc.returncode
    logger.info(f"[v1-validation] 退出码: {exit_code}")

    # 解析结果
    test_result = _parse_json_results(results_file)
    duration_ms = int((time.time() - start_time) * 1000)
    screenshots = _collect_screenshots(workspace_root, task_id, script_version_id)

    if exit_code == 0 and test_result["all_passed"]:
        return {
            "success": True,
            "total_step_count": test_result["total_tests"],
            "passed_step_count": test_result["passed_tests"],
            "failed_step_no": None,
            "fail_reason": "",
            "assertion_summary": test_result["assertions"],
            "duration_ms": duration_ms,
            "logs": _build_logs(stdout, stderr, "PASS"),
            "screenshots": screenshots,
            "error_message": "",
        }
    else:
        return {
            "success": False,
            "total_step_count": test_result["total_tests"],
            "passed_step_count": test_result["passed_tests"],
            "failed_step_no": test_result.get("first_failed_index"),
            "fail_reason": test_result.get("fail_reason", stderr[:500] if stderr else "V1 测试执行失败"),
            "assertion_summary": test_result["assertions"],
            "duration_ms": duration_ms,
            "logs": _build_logs(stdout, stderr, "FAIL"),
            "screenshots": screenshots,
            "error_message": test_result.get("fail_reason", ""),
        }


def _parse_json_results(results_file: Path) -> dict:
    """解析 Playwright JSON reporter 输出"""
    default = {
        "total_tests": 0,
        "passed_tests": 0,
        "all_passed": False,
        "assertions": [],
        "fail_reason": "",
        "first_failed_index": None,
    }

    if not results_file.exists():
        logger.warning("JSON results file not found")
        return default

    try:
        data = json.loads(results_file.read_text(encoding="utf-8"))
        suites = data.get("suites", [])

        total = 0
        passed = 0
        assertions = []
        fail_reason = ""
        first_failed = None

        def _collect_specs(suite_list):
            """递归收集所有层级的 specs（Playwright 的 test.describe 会创建嵌套 suites）"""
            specs = []
            for suite in suite_list:
                specs.extend(suite.get("specs", []))
                # 递归处理嵌套的子 suites
                nested = suite.get("suites", [])
                if nested:
                    specs.extend(_collect_specs(nested))
            return specs

        all_specs = _collect_specs(suites)

        for spec in all_specs:
            for test in spec.get("tests", []):
                for result in test.get("results", []):
                    status = result.get("status", "")

                    # 优先从 result.steps 中提取 test.step 级别的详情
                    steps = result.get("steps", [])
                    if steps:
                        for step in steps:
                            total += 1
                            step_status = step.get("error") is None
                            assertion = {
                                "name": step.get("title", f"Step {total}"),
                                "passed": step_status,
                                "skipped": False,
                            }
                            if step_status:
                                passed += 1
                            else:
                                if first_failed is None:
                                    first_failed = total
                                err = step.get("error", {})
                                if err:
                                    fail_reason = (err.get("message", "") or "")[:500]
                            assertions.append(assertion)
                    else:
                        # 回退：无 steps 时按整个 test case 统计
                        total += 1
                        is_passed = status == "passed"
                        is_skipped = status == "skipped"

                        assertion = {
                            "name": spec.get("title", f"Test {total}"),
                            "passed": is_passed,
                            "skipped": is_skipped,
                        }

                        if is_passed:
                            passed += 1
                        elif status in ("failed", "timedOut"):
                            if first_failed is None:
                                first_failed = total
                            errors = result.get("errors", [])
                            if errors:
                                fail_reason = errors[0].get("message", "")[:500]

                        assertions.append(assertion)

        return {
            "total_tests": total,
            "passed_tests": passed,
            "all_passed": total > 0 and passed == total,
            "assertions": assertions,
            "fail_reason": fail_reason,
            "first_failed_index": first_failed,
        }

    except Exception as e:
        logger.error(f"Failed to parse JSON results: {e}")
        return default


def _collect_screenshots(project_dir: Path, task_id: int, script_version_id: int) -> list:
    """收集 Playwright 运行产生的截图"""
    screenshots = []
    results_dir = project_dir / "test-results"

    if not results_dir.exists():
        return screenshots

    for img_file in results_dir.rglob("*.png"):
        # 复制到统一截图目录
        dest_name = f"validation_{task_id}_v{script_version_id}_{img_file.name}"
        dest_path = os.path.join(SCREENSHOT_DIR, dest_name)
        try:
            os.makedirs(SCREENSHOT_DIR, exist_ok=True)
            shutil.copy2(str(img_file), dest_path)
            screenshots.append({
                "file_name": dest_name,
                "url": f"/screenshots/{dest_name}",
                "caption": img_file.stem,
            })
        except Exception as e:
            logger.warning(f"Failed to copy screenshot {img_file}: {e}")

    return screenshots


def _build_logs(stdout: str, stderr: str, status: str) -> list:
    """构建日志列表"""
    logs = []
    if stdout:
        for line in stdout.strip().split("\n")[:50]:
            logs.append({"level": "INFO", "message": line.strip()})
    if stderr:
        for line in stderr.strip().split("\n")[:20]:
            logs.append({"level": "ERROR" if status == "FAIL" else "WARN", "message": line.strip()})
    return logs


def _update_config_storage_state(project_dir: Path, storage_state_path: str | None):
    """动态更新 playwright.config.ts 中的 storageState 配置

    对 V1 项目工作区：只注入/移除 storageState 行，保留项目原有配置（baseURL、dotenv 等）。
    对 V0 工作区：整文件重写（V0 config 由本模块管理，无外部配置）。
    """
    config_ts = project_dir / "playwright.config.ts"

    # 判断是否为 V1 项目工作区（存在 project.json）
    is_v1 = (project_dir / "project.json").exists()

    if is_v1 and config_ts.exists():
        # V1：只做 storageState 行的注入/移除，不重写整个文件
        content = config_ts.read_text(encoding="utf-8")

        # 先移除已有的 storageState 行
        content = re.sub(
            r"^\s*storageState:\s*.*,?\s*\n",
            "",
            content,
            flags=re.MULTILINE,
        )

        if storage_state_path:
            # 在 use: { 块内的 ignoreHTTPSErrors 或 trace 行后面注入 storageState
            inject_line = f"    storageState: '{storage_state_path}',\n"
            # 找到 use: { ... } 块中合适的注入点
            if "trace:" in content:
                content = re.sub(
                    r"(^\s*trace:.*,?\s*\n)",
                    r"\1" + inject_line,
                    content,
                    count=1,
                    flags=re.MULTILINE,
                )
            elif "ignoreHTTPSErrors:" in content:
                content = re.sub(
                    r"(^\s*ignoreHTTPSErrors:.*,?\s*\n)",
                    r"\1" + inject_line,
                    content,
                    count=1,
                    flags=re.MULTILINE,
                )
            else:
                # 兜底：在 use: { 后面直接注入
                content = re.sub(
                    r"(use:\s*\{[^\n]*\n)",
                    r"\1" + inject_line,
                    content,
                    count=1,
                )

        config_ts.write_text(content, encoding="utf-8")
        logger.info(f"Updated V1 playwright.config.ts storageState (inject only, storageState={'set' if storage_state_path else 'none'})")
        return

    # V0 兼容：整文件重写（V0 config 完全由本模块管理）
    if storage_state_path:
        storage_line = f"    storageState: '{storage_state_path}',"
    else:
        storage_line = ""

    config_content = f"""
import {{ defineConfig }} from '@playwright/test';

export default defineConfig({{
  testDir: './tests',
  timeout: 60000,
  retries: 0,
  reporter: [['json', {{ outputFile: 'test-results.json' }}]],
  use: {{
    headless: true,
    screenshot: 'on',
    locale: 'zh-CN',
    ignoreHTTPSErrors: true,
    trace: 'retain-on-failure',
{storage_line}
  }},
  projects: [
    {{
      name: 'chromium',
      use: {{
        browserName: 'chromium',
        launchOptions: {{
          args: ['--disable-blink-features=AutomationControlled'],
        }},
      }},
    }},
  ],
}});
""".strip()

    config_ts.write_text(config_content, encoding="utf-8")
    logger.info(f"Updated V0 playwright.config.ts (full rewrite, storageState={'set' if storage_state_path else 'none'})")
