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
import subprocess
import tempfile
import time
from pathlib import Path

from config import SCREENSHOT_DIR

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
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
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
) -> dict:
    """
    执行 Playwright TypeScript 脚本回放验证

    Args:
        task_id: 任务 ID
        script_version_id: 脚本版本 ID
        script_content: TypeScript 脚本内容
        start_url: 起始 URL

    Returns:
        验证结果字典
    """
    start_time = time.time()

    try:
        # 1. 确保 Playwright 项目就绪
        project_dir = _ensure_playwright_project()
        tests_dir = project_dir / "tests"
        results_file = project_dir / "test-results.json"

        # 清理旧结果
        if results_file.exists():
            results_file.unlink()

        # 2. 写入脚本文件
        script_file = tests_dir / f"task_{task_id}_v{script_version_id}.spec.ts"
        script_file.write_text(script_content, encoding="utf-8")
        logger.info(f"Script written to {script_file}")

        # 3. 执行 npx playwright test
        logger.info(f"Running playwright test for task {task_id}")
        proc = subprocess.run(
            ["npx", "playwright", "test", str(script_file.name)],
            cwd=str(project_dir),
            capture_output=True,
            text=True,
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
        # 清理临时脚本文件
        try:
            script_file = Path(PLAYWRIGHT_PROJECT_DIR) / "tests" / f"task_{task_id}_v{script_version_id}.spec.ts"
            if script_file.exists():
                script_file.unlink()
        except Exception:
            pass


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

        for suite in suites:
            for spec in suite.get("specs", []):
                for test in spec.get("tests", []):
                    for result in test.get("results", []):
                        total += 1
                        status = result.get("status", "")

                        assertion = {
                            "name": spec.get("title", f"Test {total}"),
                            "result": status,
                            "message": None,
                        }

                        if status == "passed":
                            passed += 1
                        elif status in ("failed", "timedOut"):
                            if first_failed is None:
                                first_failed = total
                            errors = result.get("errors", [])
                            if errors:
                                fail_reason = errors[0].get("message", "")[:500]
                                assertion["message"] = fail_reason

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
            import shutil
            shutil.copy2(str(img_file), dest_path)
            screenshots.append({
                "file_name": dest_name,
                "url": f"/screenshots/{dest_name}",
                "caption": img_file.stem,
            })
        except Exception as e:
            logger.warning(f"Failed to copy screenshot {img_file}: {e}")

    return screenshots


def _build_logs(stdout: str, stderr: str, status: str) -> str:
    """构建日志 JSON"""
    logs = []
    if stdout:
        for line in stdout.strip().split("\n")[:50]:
            logs.append({"level": "INFO", "message": line.strip()})
    if stderr:
        for line in stderr.strip().split("\n")[:20]:
            logs.append({"level": "ERROR" if status == "FAIL" else "WARN", "message": line.strip()})
    return json.dumps(logs, ensure_ascii=False)
