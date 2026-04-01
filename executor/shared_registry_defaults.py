"""
shared_registry_defaults.py - 内置 Shared Page 标准定义

职责：
1. 统一维护平台内置 Shared Page 的 registry 标准定义。
2. 供项目初始化与已有项目的 registry 归一化共同复用，避免多处写死后再次漂移。
3. 明确标记已经废弃的内置 shared locator / action，便于做兼容迁移。
"""
from __future__ import annotations

import copy


_BUILTIN_SHARED_REGISTRY = {
    "NavigationPage": {
        "kind": "shared",
        "file": "pages/shared/NavigationPage.ts",
        "fixture_name": "navigationPage",
        "shared_dependencies": [],
        "locators": {},
        "actions": {
            "goToMenu": {
                "summary": "通过菜单名称导航到指定页面",
                "params_signature": "(menuName: string)",
                "uses_locators": [],
                "update_mode": "non_breaking_only",
            },
            "goToMenuPath": {
                "summary": "通过多级菜单路径导航",
                "params_signature": "(menuPath: string[])",
                "uses_locators": [],
                "update_mode": "non_breaking_only",
            },
            "expectMenuActive": {
                "summary": "断言侧边栏菜单处于激活状态",
                "params_signature": "(menuText: string)",
                "uses_locators": [],
                "update_mode": "non_breaking_only",
            },
            "expectPageContainsText": {
                "summary": "断言页面包含指定文本",
                "params_signature": "(text: string)",
                "uses_locators": [],
                "update_mode": "non_breaking_only",
            },
        },
        "page_update_mode": "append_only",
    },
    "DialogPage": {
        "kind": "shared",
        "file": "pages/shared/DialogPage.ts",
        "fixture_name": "dialogPage",
        "shared_dependencies": [],
        "locators": {
            "overlay": {"summary": "弹窗遮罩层"},
            "dialog": {"summary": "弹窗容器"},
            "dialogTitle": {"summary": "弹窗标题"},
            "confirmButton": {"summary": "确认按钮"},
            "cancelButton": {"summary": "取消按钮"},
            "closeButton": {"summary": "关闭按钮"},
        },
        "actions": {
            "confirm": {
                "summary": "点击确认按钮关闭弹窗",
                "params_signature": "()",
                "uses_locators": ["confirmButton"],
                "update_mode": "non_breaking_only",
            },
            "cancel": {
                "summary": "点击取消按钮关闭弹窗",
                "params_signature": "()",
                "uses_locators": ["cancelButton"],
                "update_mode": "non_breaking_only",
            },
            "close": {
                "summary": "点击右上角关闭按钮",
                "params_signature": "()",
                "uses_locators": ["closeButton"],
                "update_mode": "non_breaking_only",
            },
            "expectVisible": {
                "summary": "断言弹窗可见",
                "params_signature": "()",
                "uses_locators": ["dialog"],
                "update_mode": "non_breaking_only",
            },
            "expectHidden": {
                "summary": "断言弹窗已关闭",
                "params_signature": "()",
                "uses_locators": ["dialog"],
                "update_mode": "non_breaking_only",
            },
            "expectTitle": {
                "summary": "断言弹窗标题文本",
                "params_signature": "(title: string)",
                "uses_locators": ["dialogTitle"],
                "update_mode": "non_breaking_only",
            },
        },
        "page_update_mode": "append_only",
    },
    "ToastPage": {
        "kind": "shared",
        "file": "pages/shared/ToastPage.ts",
        "fixture_name": "toastPage",
        "shared_dependencies": [],
        "locators": {
            "successToast": {"summary": "成功提示消息"},
            "errorToast": {"summary": "错误提示消息"},
            "warningToast": {"summary": "警告提示消息"},
            "infoToast": {"summary": "信息提示消息"},
        },
        "actions": {
            "expectSuccess": {
                "summary": "断言出现成功提示",
                "params_signature": "(options: { text?: string } = {})",
                "uses_locators": ["successToast"],
                "update_mode": "non_breaking_only",
            },
            "expectError": {
                "summary": "断言出现错误提示",
                "params_signature": "(message: string)",
                "uses_locators": ["errorToast"],
                "update_mode": "non_breaking_only",
            },
            "expectWarning": {
                "summary": "断言出现警告提示",
                "params_signature": "(message: string)",
                "uses_locators": ["warningToast"],
                "update_mode": "non_breaking_only",
            },
            "expectInfo": {
                "summary": "断言出现信息提示",
                "params_signature": "(message: string)",
                "uses_locators": ["infoToast"],
                "update_mode": "non_breaking_only",
            },
            "waitForToastDismiss": {
                "summary": "等待所有提示消息消失",
                "params_signature": "()",
                "uses_locators": ["successToast", "errorToast", "warningToast", "infoToast"],
                "update_mode": "non_breaking_only",
            },
        },
        "page_update_mode": "append_only",
    },
    "LoginPage": {
        "kind": "shared",
        "file": "pages/LoginPage.ts",
        "fixture_name": "loginPage",
        "shared_dependencies": [],
        "locators": {
            "usernameInput": {"summary": "用户名输入框"},
            "passwordInput": {"summary": "密码输入框"},
            "captchaInput": {"summary": "验证码输入框"},
            "loginButton": {"summary": "登录按钮"},
            "errorMessage": {"summary": "登录错误提示"},
        },
        "actions": {
            "login": {
                "summary": "执行登录操作",
                "params_signature": "{ username: string, password: string, captcha?: string }",
                "uses_locators": ["usernameInput", "passwordInput", "captchaInput", "loginButton"],
                "update_mode": "non_breaking_only",
            },
            "expectLoginSuccess": {
                "summary": "断言登录成功",
                "params_signature": "(expectedUrlPattern?: string | RegExp)",
                "uses_locators": ["loginButton"],
                "update_mode": "non_breaking_only",
            },
            "expectLoginFailed": {
                "summary": "断言登录失败",
                "params_signature": "(message?: string)",
                "uses_locators": ["errorMessage"],
                "update_mode": "non_breaking_only",
            },
        },
        "page_update_mode": "append_only",
    },
}

_BUILTIN_SHARED_DEPRECATIONS = {
    "NavigationPage": {
        "locators": {"sidebarNav", "breadcrumb"},
        "actions": {"expectBreadcrumbContains"},
    },
}


def build_builtin_shared_registry() -> dict:
    """返回内置 Shared Page 标准定义的深拷贝。"""
    return copy.deepcopy(_BUILTIN_SHARED_REGISTRY)


def build_builtin_shared_deprecations() -> dict:
    """返回内置 Shared Page 已废弃字段定义的深拷贝。"""
    return copy.deepcopy(_BUILTIN_SHARED_DEPRECATIONS)
