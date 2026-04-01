# TestPilot Playwright 多项目工程化架构方案（V1）

> 参考：`foradar_ui_cypress/E2E_TEST_REFACTOR_PLAN.md` V2.0 架构
> 核心目标：将录制得到的 flat Playwright 脚本，升级为可维护、可复用、可项目级执行的 Playwright 工程代码。
> 运行定位：平台内运行是主路径，同时保留项目代码脱离平台直接执行的能力。
> 2026-04-01 实现对齐说明：当前代码实现已进入本文档描述的多项目 V1 主路径；下方“已落地实现补充”用于同步当前程序已完成的关键约束与兼容修复。

### 已确认的设计决策

| 决策项 | 结论 |
|--------|------|
| **项目边界** | `project_id / project_key` 是第一层边界，生成、验证、导出都必须先定位项目 |
| **工作区策略** | 每个项目独立工作区、独立 registry、独立 fixture、独立 auth_states，不允许多项目共用一个 `pw_workspace/` |
| **POM 粒度** | 按业务模块划分（一个菜单模块一个 POM，如 `TaskManagePage`） |
| **Shared 策略** | V1 固定白名单：`NavigationPage / DialogPage / ToastPage / LoginPage`；共享模板可复用，但共享代码按项目隔离 |
| **Fixture 策略** | `base.fixture.ts` 由程序根据当前项目的 registry 全量重建，LLM 不直接编辑 fixture |
| **更新策略** | `create` 允许新建完整文件，`update` 仅允许非破坏性增量更新 |
| **代码规范** | 所有生成类和 action 方法必须补充中文注释，且必须经过程序校验后才允许落盘 |
| **向后兼容** | 仅针对新录制用例做工程化升级，已有 flat 脚本保持原样不迁移 |

### 已落地实现补充（2026-04-01）

当前代码已经落地并需要在文档中明确的实现点如下：

- 已引入项目级共享默认注册表，shared 白名单默认项由程序标准化注入，避免 legacy 项目 registry 缺项导致 fixture 或 import 生成不完整
- `PageRegistry` 已作为 LLM 上下文的统一来源，当前最小 schema 已覆盖：文件路径、页面标识、已有 actions、已有 locators、shared 依赖
- `raw_locator_guard.py` 已落地三类守卫：
  - 原始 locator 保留校验
  - 复杂原始 locator 显式覆盖校验
  - URL 等待/断言语义保留校验
- `v1_generation_pipeline.py` 已在写盘前接入上述守卫；命中违规时直接阻止落盘，转为 `manual_review`
- `fixture_builder.py` 已按 registry 重建 `base.fixture.ts`，并对缺失物理文件做兼容跳过，避免 legacy 项目直接崩溃
- `project_workspace.py` 与 `validation_runner.py` 已支持 legacy 项目工作区补齐与缺失支持文件回填，验证前会自动同步必要模板
- `NavigationPage` 已采用“侧边菜单容器内优先寻找可见节点”的公共导航策略，避免误点内容区同名文本
- `ast_merger_bridge.py` 已固定使用 `UTF-8` 调用 Node/ts-morph 合并器，避免 Windows 环境下中文 locator、中文注释被合并成乱码
- 当前平台级验证主路径仍是“只跑指定失败 spec”，而不是全量 Playwright suite

---

## 一、核心设计目标

将当前「AI 输出扁平化 `.spec.ts` 单文件」的模式，升级为「按项目隔离、按工程组织、可维护的 Playwright 多文件代码资产」。

| 维度 | 当前状态 | 目标状态 |
|------|----------|----------|
| **项目隔离** | 默认单项目工作区 | 每个项目独立工作区、独立配置、独立 registry |
| **脚本结构** | 单文件 flat `test.step` | `Pages / Fixtures / Spec / Registry` 分层 |
| **选择器管理** | 散落在脚本中 | 收敛到 `pages/*.ts` Page Object |
| **登录/前置** | 每条脚本重复 | 抽取为项目级 `fixtures/auth.fixture.ts` |
| **环境配置** | 易硬编码 URL / 数据 | 项目级 `playwright.config.ts` + `.env` 外部化 |
| **生成方式** | AI 输出 1 个文件 | AI 输出多文件结果（spec + create + update + registry） |
| **更新方式** | 直接覆盖或人工手改 | 程序执行增量合并、registry 更新、fixture 重建 |
| **运行方式** | 平台内单次验证 | 平台内项目级执行，并保留标准 Playwright 独立执行能力 |
| **维护成本** | 改一个选择器动 N 条脚本 | 改一个 Page Object 全局生效 |

---

## 二、多项目目录架构

```text
pw_projects/
├── templates/                              # 平台维护的模板资产，不直接作为项目运行代码
│   ├── shared/
│   │   ├── NavigationPage.template.ts
│   │   ├── DialogPage.template.ts
│   │   ├── ToastPage.template.ts
│   │   └── LoginPage.template.ts
│   ├── fixtures/
│   │   └── auth.fixture.template.ts
│   └── prompts/
│       └── playwright-refactor-v1.md
│
├── projects/
│   ├── foradar/
│   │   ├── project.json                    # 项目元数据
│   │   ├── playwright.config.ts           # 项目级 Playwright 配置
│   │   ├── .env.example                   # 项目环境变量模板
│   │   ├── auth_states/
│   │   │   └── default.json
│   │   ├── fixtures/
│   │   │   ├── base.fixture.ts            # 由程序重建
│   │   │   └── auth.fixture.ts            # 固定认证包装层
│   │   ├── pages/
│   │   │   ├── shared/
│   │   │   │   ├── NavigationPage.ts
│   │   │   │   ├── DialogPage.ts
│   │   │   │   └── ToastPage.ts
│   │   │   ├── LoginPage.ts
│   │   │   ├── TaskManagePage.ts
│   │   │   └── AssetDiscoveryPage.ts
│   │   ├── tests/
│   │   │   ├── task/
│   │   │   └── asset/
│   │   ├── utils/
│   │   └── registry/
│   │       └── page-registry.json
│   └── testpilot/
│       ├── project.json
│       ├── playwright.config.ts
│       ├── fixtures/
│       ├── pages/
│       ├── tests/
│       ├── utils/
│       └── registry/
│
└── exports/                                # 项目级导出产物，可选
    └── <project_key>/
```

### 设计原则

- 模板共享，代码隔离
- 项目内代码独立运行，项目间不共享运行时代码文件
- 所有生成路径都必须限制在 `projects/<project_key>/` 之下
- 多项目可以同名页面、同名 fixture 映射，但作用域仅限各自项目内部

---

## 三、各层职责与项目边界

### 3.1 ProjectScope 与项目工作区

> 多项目版本的第一原则不是“先生成代码”，而是“先确定当前项目作用域”。

每一次录制、生成、验证、导出都必须先解析 `ProjectScope`。建议最小结构如下：

```json
{
  "project_id": 1001,
  "project_key": "foradar",
  "project_name": "Foradar UI",
  "workspace_root": "pw_projects/projects/foradar",
  "registry_file": "registry/page-registry.json",
  "env_file": ".env",
  "auth_state_dir": "auth_states",
  "base_url_env": "BASE_URL",
  "auth_strategy": "storage_state"
}
```

#### ProjectScope 规则

- 所有任务记录必须绑定 `project_id`
- 所有脚本版本必须绑定 `project_id`
- 所有验证任务必须绑定 `project_id`
- 所有生成文件路径都必须相对当前项目根目录
- 不允许跨项目读写页面文件、fixture、registry、auth_state

### 3.2 Page Object 层

> Playwright 推荐将 Locators 和 Actions 合并在 Page Object 中，这里采用 Class 化组织，便于 AI 增量更新和人工维护。

```typescript
// pages/TaskManagePage.ts
import { type Locator, type Page } from '@playwright/test';

/**
 * 任务管理页面对象，负责封装任务管理模块内的核心交互。
 */
export class TaskManagePage {
  readonly page: Page;
  readonly taskManageMenu: Locator;
  readonly assetDiscoveryMenu: Locator;
  readonly createTaskButton: Locator;
  readonly targetTextarea: Locator;
  readonly confirmButton: Locator;

  constructor(page: Page) {
    this.page = page;
    // 定位器优先级遵循 V1 规范，优先 testid / role，缺失时再退化。
    this.taskManageMenu = page.getByRole('menuitem', { name: '任务管理' });
    this.assetDiscoveryMenu = page.getByRole('menuitem', { name: '资产探知' });
    this.createTaskButton = page.getByRole('button', { name: '新建任务' });
    this.targetTextarea = page.getByRole('textbox', { name: '目标输入' });
    this.confirmButton = page.getByRole('button', { name: '确定' });
  }

  /**
   * 导航到资产探知页面。
   */
  async navigateToAssetDiscovery(): Promise<void> {
    await this.taskManageMenu.click();
    await this.assetDiscoveryMenu.click();
    await this.page.waitForLoadState('networkidle');
  }

  /**
   * 创建扫描任务。
   */
  async createTask(options: { targetIp: string }): Promise<void> {
    await this.createTaskButton.click();
    await this.targetTextarea.fill(options.targetIp);
    await this.confirmButton.click();
  }
}
```

### 3.3 Fixture 层

```typescript
// fixtures/base.fixture.ts
import { test as base } from '@playwright/test';
import { TaskManagePage } from '../pages/TaskManagePage';
import { NavigationPage } from '../pages/shared/NavigationPage';
import { ToastPage } from '../pages/shared/ToastPage';

export const test = base.extend<{
  taskManagePage: TaskManagePage;
  navigationPage: NavigationPage;
  toastPage: ToastPage;
}>({
  taskManagePage: async ({ page }, use) => {
    await use(new TaskManagePage(page));
  },
  navigationPage: async ({ page }, use) => {
    await use(new NavigationPage(page));
  },
  toastPage: async ({ page }, use) => {
    await use(new ToastPage(page));
  },
});

export { expect } from '@playwright/test';
```

```typescript
// fixtures/auth.fixture.ts
import path from 'path';
import { test as base } from './base.fixture';

const AUTH_STATE_FILE = path.resolve(__dirname, '../auth_states/default.json');

export const test = base.extend({
  storageState: AUTH_STATE_FILE,
});

export { expect } from '@playwright/test';
```

### 3.4 测试用例层

```typescript
// tests/task/create-task.spec.ts
import { test } from '../../fixtures/auth.fixture';

test.describe('资产探知任务管理', () => {
  test('创建下发任务', async ({ taskManagePage, toastPage }) => {
    await test.step('进入资产探知页面', async () => {
      await taskManagePage.navigateToAssetDiscovery();
    });

    await test.step('创建新任务', async () => {
      const targetIp = process.env.FORADAR_TARGET_IP;
      if (!targetIp) {
        throw new Error('FORADAR_TARGET_IP 未配置');
      }

      await taskManagePage.createTask({ targetIp });
    });

    await test.step('校验创建结果', async () => {
      await toastPage.expectSuccess('操作成功');
    });
  });
});
```

### 3.5 项目级注册表（AI 专用）

```json
// registry/page-registry.json
{
  "version": 3,
  "project": {
    "project_id": 1001,
    "project_key": "foradar",
    "project_name": "Foradar UI",
    "workspace_root": "pw_projects/projects/foradar",
    "registry_file": "registry/page-registry.json",
    "auth_state_dir": "auth_states",
    "base_url_env": "BASE_URL",
    "auth_strategy": "storage_state"
  },
  "pages": {
    "TaskManagePage": {
      "kind": "business",
      "file": "pages/TaskManagePage.ts",
      "fixture_name": "taskManagePage",
      "module_key": "task-manage",
      "page_identity": {
        "active_menu": ["任务管理"],
        "breadcrumb": ["工作台/任务管理"],
        "page_title": ["任务管理"],
        "root_container": ["[data-testid='task-manage-page']"],
        "url_patterns": ["/workbench", "/task-manage"]
      },
      "shared_dependencies": ["NavigationPage", "DialogPage", "ToastPage"],
      "locators": {
        "taskManageMenu": {
          "summary": "任务管理菜单"
        },
        "createTaskButton": {
          "summary": "新建任务按钮"
        },
        "targetTextarea": {
          "summary": "目标输入框"
        }
      },
      "actions": {
        "navigateToAssetDiscovery": {
          "summary": "导航到资产探知页面",
          "params_signature": "{}",
          "uses_locators": ["taskManageMenu"],
          "update_mode": "non_breaking_only"
        },
        "createTask": {
          "summary": "创建扫描任务",
          "params_signature": "{ targetIp: string, name?: string }",
          "uses_locators": ["createTaskButton", "targetTextarea"],
          "update_mode": "non_breaking_only"
        }
      },
      "page_update_mode": "append_only"
    }
  },
  "shared": {
    "NavigationPage": {
      "kind": "shared",
      "file": "pages/shared/NavigationPage.ts",
      "fixture_name": "navigationPage",
      "shared_dependencies": [],
      "locators": {},
      "actions": {},
      "page_update_mode": "append_only"
    },
    "DialogPage": {
      "kind": "shared",
      "file": "pages/shared/DialogPage.ts",
      "fixture_name": "dialogPage",
      "shared_dependencies": [],
      "locators": {},
      "actions": {},
      "page_update_mode": "append_only"
    },
    "ToastPage": {
      "kind": "shared",
      "file": "pages/shared/ToastPage.ts",
      "fixture_name": "toastPage",
      "shared_dependencies": [],
      "locators": {
        "successToast": {
          "summary": "成功提示"
        }
      },
      "actions": {
        "expectSuccess": {
          "summary": "校验成功提示",
          "params_signature": "(message: string)",
          "uses_locators": ["successToast"],
          "update_mode": "non_breaking_only"
        }
      },
      "page_update_mode": "append_only"
    },
    "LoginPage": {
      "kind": "shared",
      "file": "pages/LoginPage.ts",
      "fixture_name": "loginPage",
      "shared_dependencies": [],
      "locators": {
        "usernameInput": {
          "summary": "用户名输入框"
        },
        "passwordInput": {
          "summary": "密码输入框"
        },
        "captchaInput": {
          "summary": "验证码输入框"
        },
        "loginButton": {
          "summary": "登录按钮"
        }
      },
      "actions": {
        "login": {
          "summary": "执行登录",
          "params_signature": "{ username: string, password: string, captcha?: string }",
          "uses_locators": ["usernameInput", "passwordInput", "captchaInput", "loginButton"],
          "update_mode": "non_breaking_only"
        }
      },
      "page_update_mode": "append_only"
    }
  }
}
```

> `page-registry.json` 的职责不是保存源码副本，而是作为“当前项目的生成决策索引”。系统通过它做页面判定、复用决策、shared 依赖分析和 fixture 生成。

---

### 3.6 页面判定规则（V1）

> 页面边界由系统规则优先判定，LLM 不负责自由拆分页面，只在当前项目、已确定的页面上下文中补充 locator、action、中文注释和断言建议。

#### 3.6.1 页面对象分类

- `BusinessPage`：业务页面对象，按业务模块划分，一个业务模块对应一个 Page Object
- `SharedPage`：公共页面对象，跨多个业务模块稳定复用，但代码文件按项目落地

V1 阶段固定公共页面对象范围如下：

- `NavigationPage`
- `DialogPage`
- `ToastPage`
- `LoginPage`

> 说明：`LoginPage` 虽然属于固定公共页面对象，但物理文件可以继续放在 `pages/LoginPage.ts`，不强制放入 `pages/shared/` 目录。

#### 3.6.2 项目边界约束

- 页面判定只在当前项目的 registry 中进行
- 不允许跨项目复用已有 Page Object 源码
- 即使不同项目页面名相同，也只能在各自项目内独立维护
- `module_key` 的唯一性只要求在当前项目内成立

#### 3.6.3 业务页面划分规则

- 页面按“业务模块”划分，不按一次界面变化、一次弹窗出现或一次局部交互划分
- 同一业务模块内的列表、搜索、筛选、分页、表单填写、业务弹窗、业务抽屉、页内 Tab，默认归同一个 `BusinessPage`
- V1 阶段不允许 AI 自由创建新的 `SharedPage`，除固定公共页面对象外，其余对象一律优先归当前业务页面

#### 3.6.4 页面复用判定优先级

系统在录制步骤解析阶段先提取 `PageContext`，至少包含以下信息：

- `project_key`
- `active_menu`
- `breadcrumb`
- `page_title`
- `root_container`
- `url_pattern`
- `dialog_title`
- `tab_or_window_id`

复用已有 `BusinessPage` 时，按以下优先级判定：

1. `active_menu / breadcrumb / 页面主标题`
2. `root_container`
3. `url_pattern`

推荐评分权重如下：

- `active_menu / breadcrumb` 命中：40 分
- `page_title` 命中：25 分
- `root_container` 命中：20 分
- `url_pattern` 命中：15 分

总分 `>= 70` 时复用已有页面，总分 `< 70` 时才进入新页面创建判断。

#### 3.6.4.1 任务工作台就绪判定补充规则

针对“同一业务模块下存在多个任务工作台入口”的页面，页面归属仍然按同一个 `BusinessPage` 处理，但页面就绪判定必须遵循以下规则：

- 禁止把工作台就绪条件硬编码为某一个固定 URL，例如只接受 `/assetsScan`、`/unitIndex` 或 `/domainTask`
- 入口页 ready 优先使用录制脚本中真实出现的入口控件，例如 `查看任务`
- 工作台 ready 优先使用工作台稳定可见的业务控件，例如 `新建任务`
- 表单 ready 优先使用录制脚本中真实操作过的首屏表单控件，例如 `textarea`、`请输入任务名称`、`请输入企业名称进行搜索`
- 只有录制脚本里明确出现了 `waitForURL` 或 URL 断言时，才允许保留 URL 级等待；否则默认使用业务控件判定
- 如果点击某个复杂入口 locator 后，系统直接进入了不同的业务状态页且录制稿中的工作台控件不再存在，应明确标记为“业务数据态漂移 / 需重录”，而不是继续猜测替代 locator

#### 3.6.5 何时复用已有页面

满足以下条件时，必须复用现有 `BusinessPage`：

- 当前项目作用域一致
- 当前业务标识与已有页面一致
- 页面主区域未发生明显变化
- 只是页内操作变化，如搜索、切换 Tab、分页、打开业务弹窗、提交表单

#### 3.6.6 何时创建新页面

仅在以下场景创建新的 `BusinessPage`：

- 进入新的业务模块，菜单、面包屑或页面主标题发生明显变化
- 打开独立详情页或新标签页，且主区域标识与现有页面明显不同
- 当前项目的 `page-registry.json` 中找不到可复用页面

#### 3.6.7 弹窗与提示归属规则

- 业务弹窗、业务抽屉不单独创建页面对象，默认归所属 `BusinessPage`
- 通用确认弹窗归 `DialogPage`
- 通用消息提示归 `ToastPage`
- 公共导航跳转归 `NavigationPage`

#### 3.6.8 LLM 职责边界

页面判定完成后，LLM 只负责以下内容：

- 在指定页面对象中补充 locator
- 在指定页面对象中新增或补充 action 方法
- 生成方法级中文注释和必要断言建议

LLM 不负责以下内容：

- 自由决定页面边界
- 自由创建新的 `SharedPage`
- 跨项目复用或修改其他项目的页面文件
- 因一次弹窗或一次局部交互将现有业务页面拆碎

### 3.7 Create / Update 规则（V1）

> V1 阶段采用“增量更新优先，整文件重写禁用”的策略。`create` 允许生成完整新文件，`update` 只允许做非破坏性增量更新。

#### 3.7.1 核心原则

- `create`：允许在当前项目内生成完整新页面文件
- `update`：只允许对当前项目已有页面做非破坏性增量更新
- 默认不允许 AI 整文件重写已有 `Page Object`
- 默认不允许 AI 删除、重命名、替换已有 `locator` 和 `action`
- 任何可能影响旧用例行为的修改，一律标记为 `manual_review`，不得自动落盘

#### 3.7.2 支持的操作类型

V1 阶段仅支持以下 5 类操作：

- `reuse`
- `create_page`
- `append_locator`
- `append_action`
- `extend_action_non_breaking`

#### 3.7.3 Create 触发条件

满足以下条件时，执行 `create_page`：

- 当前项目作用域已确定
- 根据页面判定规则确认这是新的 `BusinessPage`
- 当前项目的 `page-registry.json` 中找不到可复用页面
- 目标文件不存在

#### 3.7.4 Update 判定顺序

对已有页面执行更新时，必须按以下顺序判断：

1. 页面存在，且 action 已存在并足以表达当前业务步骤 -> `reuse`
2. 页面存在，action 不存在，但已有 locator 可复用 -> `append_action`
3. 页面存在，action 不存在，locator 也不完整 -> `append_locator` + `append_action`
4. 页面存在，action 语义相同但能力不足 -> `extend_action_non_breaking`
5. 若更新会破坏旧方法语义、超出当前项目作用域或影响旧用例 -> `manual_review`

#### 3.7.5 允许的非破坏性更新

`extend_action_non_breaking` 仅允许以下行为：

- 给方法参数对象增加可选字段
- 给方法内部补充缺失步骤
- 给方法补充更稳定的等待、断言和异常保护
- 给页面补充新的 locator
- 给文件补充缺失的 `import`

#### 3.7.6 禁止的自动更新

V1 阶段自动更新明确禁止以下行为：

- 修改类名
- 修改文件名
- 修改已有方法名
- 修改已有参数的业务语义
- 删除已有 locator
- 删除已有 action
- 将一个已有方法改造成另一种业务行为
- 对已有页面执行整文件覆盖
- 跨项目写入或更新页面文件

#### 3.7.7 LLM 输出边界

- 新建页面时，LLM 可以输出完整文件内容
- 更新已有页面时，LLM 不应输出“整文件重写结果”
- 更新已有页面时，LLM 只能输出结构化增量结果，由程序负责合并

建议的增量输出结构如下：

```json
{
  "page_name": "TaskManagePage",
  "operation": "append_action",
  "new_locators": [
    {
      "name": "taskTypeSelect",
      "definition": "page.getByRole('combobox', { name: '任务类型' })"
    }
  ],
  "new_actions": [
    {
      "name": "selectTaskType",
      "content": "..."
    }
  ],
  "extend_actions": [
    {
      "name": "createTask",
      "change_type": "add_optional_params",
      "content": "..."
    }
  ]
}
```

#### 3.7.8 程序合并原则

- 程序负责将 `new_locators`、`new_actions`、`extend_actions` 合并到当前项目的目标文件
- 不将整文件合并权交给 LLM
- `base.fixture.ts`、注册表和页面文件的最终写入结果，以程序生成和程序合并为准

### 3.8 Shared 抽取规则（V1）

> V1 阶段对 shared 抽取采用保守策略，使用固定白名单，禁止 AI 自由创建新的共享组件页面对象。共享模板可复用，但共享代码仍按项目落地。

#### 3.8.1 Shared 的定义

只有同时满足以下条件的 UI，才允许抽取为 `SharedPage`：

- 跨多个业务模块稳定复用
- 交互语义一致
- 定位方式相对稳定
- 方法命名可以保持通用，不依赖具体业务术语

#### 3.8.2 V1 固定白名单

V1 阶段仅允许以下共享页面对象存在：

- `NavigationPage`
- `DialogPage`
- `ToastPage`
- `LoginPage`

#### 3.8.3 多项目下的 Shared 原则

- 共享的是“模板规则”，不是跨项目直接共用源码文件
- 每个项目都生成自己的 `NavigationPage / DialogPage / ToastPage / LoginPage`
- 项目 A 的 shared 文件不能被项目 B 直接 import

#### 3.8.4 默认不抽取为 Shared 的对象

以下对象即使在多个页面中出现，V1 阶段也默认保留在各自 `BusinessPage` 内，不自动抽为共享对象：

- 业务表单弹窗
- 业务抽屉
- 业务筛选区
- 业务表格
- 业务分页区
- 业务详情面板
- 业务专属 Tab 区
- 某个模块独有的工具栏或操作区

#### 3.8.5 Shared 方法语义约束

共享页面对象的方法必须保持通用语义，不得携带业务特定含义。

推荐写法：

- `NavigationPage.goToMenu('任务管理')`
- `DialogPage.confirm()`
- `DialogPage.cancel()`
- `ToastPage.expectSuccess('操作成功')`

禁止写法：

- `DialogPage.confirmDeleteTask()`
- `NavigationPage.goToAiScriptTaskPageAndOpenCreateDialog()`
- `ToastPage.expectTaskCreateSuccess()`

#### 3.8.6 Shared 更新边界

对固定白名单中的 `SharedPage`，仅允许以下增量更新：

- 补充新的通用 locator
- 补充新的通用 action
- 补充中文方法注释和必要断言
- 补充稳定性增强，如等待、通用可见性校验

不允许以下更新：

- 将业务逻辑写入 shared
- 将业务字段、业务按钮、业务流程塞入 `SharedPage`
- 将 shared 改造成“半通用半业务”的混合对象
- 为某个单独模块定制 shared 专属方法

#### 3.8.7 候选 Shared 的处理方式

如果 AI 识别到疑似可复用组件，可以输出 `candidate_shared` 提示，但不得自动创建。例如：

```json
{
  "candidate_shared": [
    {
      "name": "FilterPanel",
      "reason": "在多个业务模块中出现，结构相似，但当前未进入共享白名单"
    }
  ]
}
```

### 3.9 Registry 最小 Schema（V1）

> `registry` 的职责是支撑系统进行页面归属判定、更新策略判定、shared 依赖分析和 fixture 自动生成，不是展示型清单，也不是源码存储区。

#### 3.9.1 顶层结构

V1 阶段 `page-registry.json` 最少包含以下顶层字段：

- `version`
- `project`
- `pages`
- `shared`

#### 3.9.2 project 最小字段

每个项目 registry 顶层最少包含：

- `project_id`
- `project_key`
- `project_name`
- `workspace_root`
- `registry_file`
- `auth_state_dir`
- `base_url_env`
- `auth_strategy`

#### 3.9.3 BusinessPage 最小字段

每个业务页面对象最少包含以下字段：

- `kind`
- `file`
- `fixture_name`
- `module_key`
- `page_identity`
- `shared_dependencies`
- `locators`
- `actions`
- `page_update_mode`

#### 3.9.4 page_identity 最小字段

`page_identity` 最少包含以下字段：

- `active_menu`
- `breadcrumb`
- `page_title`
- `root_container`
- `url_patterns`

#### 3.9.5 locators / actions 的存储形式

V1 阶段不再使用纯字符串数组存储 `locators` 和 `actions`，统一改为对象结构。

`locators` 最少包含：

- `summary`

`actions` 最少包含：

- `summary`
- `params_signature`
- `uses_locators`
- `update_mode`

#### 3.9.6 SharedPage 最小字段

每个共享页面对象与 `BusinessPage` 尽量使用同一套结构，最少包含：

- `kind`
- `file`
- `fixture_name`
- `shared_dependencies`
- `locators`
- `actions`
- `page_update_mode`

#### 3.9.7 更新策略落地规则

`registry` 中的以下字段用于承接 Create / Update 规则：

- `page_update_mode`
- `actions.*.update_mode`

V1 阶段约束如下：

- `page_update_mode` 固定为 `append_only`
- `actions.*.update_mode` 固定为 `non_breaking_only`

#### 3.9.8 强烈推荐字段

虽然不作为 V1 最小必填字段，但强烈建议后续补充以下字段：

- `file_hash`
- `last_generated_at`
- `last_generated_by`

其中 `file_hash` 可用于判断目标文件是否已被人工改动。一旦实际文件 hash 与 registry 记录不一致，系统应优先转入 `manual_review`，避免误覆盖人工代码。

### 3.10 Fixture 更新方式（V1）

> Fixture 采用“程序生成 + 分层固定”的方式实现。V1 阶段禁止 LLM 直接编辑 fixture 文件，避免将全局基础设施变成不稳定生成物。

#### 3.10.1 核心原则

- `base.fixture.ts` 不交给 LLM 编辑
- `auth.fixture.ts` 不交给 LLM 编辑
- fixture 的唯一数据来源是当前项目的 `page-registry.json`
- 每个项目独立维护自己的 fixture 文件

#### 3.10.2 V1 固定文件职责

V1 阶段 fixture 文件职责固定如下：

- `fixtures/base.fixture.ts`：负责注入当前项目所有 `BusinessPage` 和 `SharedPage`
- `fixtures/auth.fixture.ts`：负责在 `base.fixture.ts` 之上注入登录态、`storageState` 和认证包装逻辑

#### 3.10.3 base.fixture.ts 生成方式

`base.fixture.ts` 由程序根据当前项目的 `registry.pages + registry.shared` 全量生成或重建，生成内容固定包括：

- 页面对象 import 列表
- `test.extend<...>` 的类型声明
- 每个页面对象的实例注入逻辑
- `export { expect }`

#### 3.10.4 auth.fixture.ts 维护方式

`auth.fixture.ts` 保持稳定模板，由人工和程序共同维护，但不允许 LLM 自由生成业务逻辑。其职责包括：

- 基于 `base.fixture.ts` 再次扩展 `test`
- 注入 `storageState`
- 读取项目级 `.env` 或运行时认证配置
- 在无有效登录态时执行兜底认证逻辑

#### 3.10.5 生成与导入规则

V1 阶段建议的用例导入规则如下：

- 普通业务用例默认导入当前项目的 `auth.fixture.ts`
- 登录类、认证类、无登录态校验类用例导入当前项目的 `base.fixture.ts`

#### 3.10.6 fixture_name 规则

- `registry` 中的 `fixture_name` 是项目内唯一准入名称
- `fixture_name` 必须在当前项目内全局唯一
- 同名冲突时，程序应直接失败并转入 `manual_review`

#### 3.10.7 禁止项

V1 阶段明确禁止以下行为：

- 按 spec 动态生成临时 fixture
- 允许 LLM 新增任意 fixture 文件
- 允许 LLM 直接修改 `base.fixture.ts`
- 允许 LLM 直接修改 `auth.fixture.ts`
- 在 fixture 中写入业务页面流程逻辑
- 允许跨项目共用同一份 fixture 文件

#### 3.10.8 生成失败保护

在以下场景中，程序不得生成 fixture，必须报错并转入 `manual_review`：

- `registry` 中声明的页面文件不存在
- `registry` 中声明的类名与目标文件实际类名不一致
- `fixture_name` 冲突
- `registry` 与页面文件导入关系无法解析
- 目标路径超出当前项目工作区

### 3.11 代码生成硬约束（V1）

> 代码生成硬约束必须同时落到 Prompt、模板和程序校验三层。V1 阶段不接受“能跑就行”的生成结果，不满足硬约束的代码不得落盘。

#### 3.11.1 技术栈硬约束

- 只允许生成 `TypeScript`
- 只允许使用 `@playwright/test`
- 只允许生成符合当前工程分层的文件：
  - `pages/*.ts`
  - `pages/shared/*.ts`
  - `tests/**/*.spec.ts`
- 不允许 AI 自由新增新的基础设施文件，例如：
  - `playwright.config.ts`
  - `fixtures/base.fixture.ts`
  - `fixtures/auth.fixture.ts`

#### 3.11.2 分层职责硬约束

- `spec` 层只写业务意图，不允许出现：
  - `page.locator(...)`
  - `page.getBy...(...)`
  - 原始 CSS / XPath 选择器
- `Page Object` 层负责：
  - locator 定义
  - 业务 action 封装
  - 必要断言
- `SharedPage` 只允许通用语义，不允许承载业务流程
- `fixture` 文件不允许由 LLM 直接编辑

#### 3.11.3 多项目路径硬约束

- 所有生成路径都必须相对当前项目根目录
- 不允许生成 `../` 逃逸路径
- 不允许 import 其他项目下的文件
- 不允许跨项目复用现成源码

#### 3.11.4 文件结构硬约束

`pages/*.ts` 必须满足固定结构：

1. import 区
2. 类职责中文注释
3. class 定义
4. `readonly page: Page`
5. `readonly xxx: Locator`
6. constructor 中只做 locator 初始化
7. action 方法区

`tests/**/*.spec.ts` 必须满足固定结构：

1. 从 fixture 导入 `test, expect`
2. `test.describe(...)`
3. `test(...)`
4. `test.step(...)`
5. 不出现底层定位细节

#### 3.11.5 命名硬约束

- 文件名必须与类名一致，例如 `TaskManagePage.ts -> TaskManagePage`
- `fixture_name` 必须与 registry 一致，例如 `TaskManagePage -> taskManagePage`
- action 方法名必须采用“动词 + 业务对象”的语义形式，例如：
  - `createTask`
  - `navigateToAssetDiscovery`
  - `deleteTask`
- 禁止生成无语义方法名，例如：
  - `handleClick`
  - `doAction`
  - `processData`
  - `step1`

#### 3.11.6 定位器硬约束

定位器优先级固定如下：

1. `getByTestId`
2. `getByRole`
3. `getByLabel`
4. `getByPlaceholder`
5. `getByText`
6. `locator(css)`

默认禁止生成以下脆弱定位：

- `nth-child`
- 过长 CSS 链
- 纯 index 定位
- 无范围约束的纯文本模糊匹配
- 无解释的 `.first()` / `.last()` 兜底

#### 3.11.7 等待与断言硬约束

- 默认禁止生成 `waitForTimeout`
- 优先使用：
  - Playwright auto-wait
  - `expect(...)`
  - `waitForURL`
  - `waitForLoadState`
  - `waitForResponse`
  - locator 级等待
- 关键业务动作后必须有结果校验，例如：
  - 成功提示
  - URL 变化
  - 列表状态变化
  - 表单提交结果

#### 3.11.8 参数与方法签名硬约束

- action 方法统一优先使用 `options object`
- 后续扩展字段只能新增可选参数
- 不允许生成多个难维护的位置参数
- 方法返回值必须明确：
  - 大多数 action 返回 `Promise<void>`
  - 如需返回业务结果，必须显式声明返回类型

#### 3.11.9 环境与敏感信息硬约束

禁止硬编码以下内容：

- URL
- 用户名
- 密码
- token
- 验证码
- IP
- 环境标识
- 项目根路径

必须通过以下方式注入：

- `process.env`
- `options`
- 测试数据层
- auth fixture / auth state

#### 3.11.10 中文注释硬约束

中文注释为强制要求，不是建议项。

- 每个生成的类必须有中文职责注释
- 每个 action 方法必须有中文方法注释
- 方法内部如存在关键业务逻辑、分支、降级定位、特殊等待，必须补充中文逻辑注释
- 注释必须解释“为什么这么做”或“这段逻辑保证了什么”

#### 3.11.11 明确禁止生成的内容

- `test.only`
- `test.skip`
- `test.fixme`
- `page.pause()`
- `debugger`
- `console.log` 调试残留
- 默认使用 `force: true`
- 空的 `catch`
- 吞异常
- 整文件重写已有 POM
- 在 spec 中直接写 locator
- 在 shared 中写业务方法
- 跨项目 import 页面或 fixture

#### 3.11.12 程序校验硬约束

生成结果最少必须通过以下校验，否则拒绝落盘：

- JSON Schema 校验
- TypeScript 语法校验
- import 路径存在性校验
- 文件名与类名一致性校验
- registry 与页面文件一致性校验
- `fixture_name` 一致性校验
- 中文注释存在性校验
- 目标路径是否位于当前项目根目录
- 禁止词扫描，例如：
  - `waitForTimeout`
  - `test.only`
  - `page.pause`
  - `debugger`

---

## 四、AI 自动生成流程（多项目版）

### 4.1 当前流程 vs 目标流程

```text
【当前】
录制原始脚本 → AI 重构 → 输出 1 个 flat .spec.ts → 写入全局 tests/

【目标】
录制原始脚本
  → 解析 ProjectScope
  → 解析 PageContext
  → AI 输出结构化 JSON
  → 程序创建新页面 / 合并增量更新
  → 更新当前项目 page-registry.json
  → 重建当前项目 base.fixture.ts
  → 在当前项目工作区执行指定 spec 验证
```

### 4.2 AI 重构输入与输出契约

**输入增强**：在调用 LLM 时，除了原始录制脚本和步骤模型，还需要注入：

1. **ProjectScope**（当前项目元数据）
2. **当前项目的 Page Object 注册表**（`page-registry.json`）
3. **PageContext 信息**（页面归属判定结果）
4. **固定架构规则**（文档 `3.6 ~ 3.11`）
5. **POM / Spec 模板片段**（保证输出结构一致）
6. **当前项目环境配置上下文**（baseURL、已知环境变量）

**输出格式**：LLM 输出路径必须为“相对于当前项目根目录”的路径，不允许携带其他项目路径。

```json
{
  "spec_file": {
    "path": "tests/task/create-task.spec.ts",
    "content": "import { test, expect } from '../../fixtures/auth.fixture';\n..."
  },
  "page_creates": [
    {
      "path": "pages/TaskManagePage.ts",
      "class_name": "TaskManagePage",
      "content": "import { type Page, type Locator, expect } from '@playwright/test';\n..."
    }
  ],
  "page_updates": [
    {
      "page_name": "TaskManagePage",
      "operation": "append_action",
      "new_locators": [
        {
          "name": "taskTypeSelect",
          "definition": "page.getByRole('combobox', { name: '任务类型' })",
          "summary": "任务类型选择框"
        }
      ],
      "new_actions": [
        {
          "name": "selectTaskType",
          "content": "..."
        }
      ],
      "extend_actions": [
        {
          "name": "createTask",
          "change_type": "add_optional_params",
          "content": "..."
        }
      ]
    }
  ],
  "registry_updates": {
    "TaskManagePage": {
      "kind": "business",
      "file": "pages/TaskManagePage.ts",
      "fixture_name": "taskManagePage",
      "module_key": "task-manage",
      "page_identity": {
        "active_menu": ["任务管理"],
        "breadcrumb": ["工作台/任务管理"],
        "page_title": ["任务管理"],
        "root_container": ["[data-testid='task-manage-page']"],
        "url_patterns": ["/workbench"]
      },
      "shared_dependencies": ["NavigationPage", "ToastPage"],
      "locators": {
        "taskManageMenu": {
          "summary": "任务管理菜单"
        },
        "createTaskButton": {
          "summary": "新建任务按钮"
        }
      },
      "actions": {
        "navigateToAssetDiscovery": {
          "summary": "导航到资产探知页面",
          "params_signature": "{}",
          "uses_locators": ["taskManageMenu"],
          "update_mode": "non_breaking_only"
        },
        "createTask": {
          "summary": "创建扫描任务",
          "params_signature": "{ targetIp: string }",
          "uses_locators": ["createTaskButton"],
          "update_mode": "non_breaking_only"
        }
      },
      "page_update_mode": "append_only"
    }
  },
  "manual_review_items": [
    "若检测到目标文件已被人工修改且 file_hash 不一致，则不得自动覆盖"
  ],
  "risk_hints": ["..."],
  "generation_summary": "..."
}
```

### 4.3 后端处理流程（script_generator.py 改造）

```python
def refactor_recorded_script_v2(
    project_scope: dict,
    scenario_desc: str,
    start_url: str,
    raw_script: str,
    step_model_json: dict | None = None,
) -> dict:
    """
    V2 多项目重构流程：
    1. 解析 project_scope，定位当前项目工作区
    2. 读取当前项目的 page-registry.json
    3. 解析 raw_script，提取 step_model_json 和 page_context
    4. 注入文档 3.6 ~ 3.11 的规则、ProjectScope、registry 和模板上下文构建 Prompt
    5. 调用 LLM，要求输出 {spec_file, page_creates, page_updates, registry_updates, manual_review_items}
    6. 先做 JSON Schema 校验、代码硬约束校验和路径作用域校验
    7. 程序创建新页面文件，并对当前项目已有页面执行增量合并
    8. 更新当前项目 page-registry.json
    9. 根据 registry 全量重建当前项目 base.fixture.ts
    10. 返回生成产物、风险提示和 manual_review 信息给前端展示
    """
```

### 4.4 AI Prompt 核心约束

```text
你是 Playwright 工程化重构器。
必须严格遵守文档 3.6 ~ 3.11 的规则。

关键限制：
- 页面边界由系统判定，不要自由拆分页面
- Shared 仅允许 NavigationPage / DialogPage / ToastPage / LoginPage
- 更新已有页面时只允许输出增量结果，不允许整文件重写
- 不允许输出 fixture 修改
- spec 中不得出现 locator
- 所有类和 action 方法必须带中文注释
- 所有输出路径都必须相对于当前项目根目录
- 不允许跨项目复用或修改其他项目代码

输出格式：
输出 JSON，包含 spec_file、page_creates、page_updates、registry_updates、manual_review_items
```

---

## 五、Validation Runner 适配（多项目版）

### 5.1 从单文件执行到项目级执行

当前 `validation_runner.py` 只执行单个 `.spec.ts`，但工程化后 spec 会 import fixtures 和 pages，需要完整项目上下文。

**改造要点**：

- `pw_projects/projects/<project_key>/` 是执行根目录
- 验证时必须先通过 `project_id / project_key` 解析目标项目工作区
- spec 文件按 `tests/{module}/` 组织
- `playwright.config.ts`、`.env`、`auth_states/` 都使用项目级文件
- 验证时只执行指定 spec 文件，但必须以完整项目上下文执行
- `base.fixture.ts` 和 `page-registry.json` 的最终结果必须在验证前已经落盘
- 不允许跨项目执行或跨项目引用页面文件

### 5.2 建议的验证器接口

```python
def run_validation(
    project_scope: dict,
    task_id: int,
    script_version_id: int,
    spec_relative_path: str,
) -> dict:
    """
    1. 根据 project_scope 定位项目工作区
    2. 校验 spec_relative_path 处于当前项目 tests/ 下
    3. 加载当前项目的 playwright.config.ts、.env、auth_states
    4. 执行 npx playwright test <spec_relative_path>
    5. 回传当前项目范围内的结果、截图和日志
    """
```

### 5.3 playwright.config.ts 工程化

```typescript
import { defineConfig } from '@playwright/test';
import dotenv from 'dotenv';

dotenv.config();

export default defineConfig({
  testDir: './tests',
  timeout: 60000,
  retries: 0,
  reporter: [['json', { outputFile: 'test-results.json' }]],
  use: {
    // BASE_URL 必须通过当前项目的环境变量注入，不在代码中硬编码
    baseURL: process.env.BASE_URL,
    headless: true,
    screenshot: 'on',
    locale: 'zh-CN',
    ignoreHTTPSErrors: true,
    trace: 'retain-on-failure',
    storageState: process.env.AUTH_STATE_PATH || undefined,
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
```

---

## 六、接口与数据契约（实施级）

### 6.1 通用字段约定

所有生成、验证、导出、人工确认相关接口都必须带上以下公共字段：

- `project_id`
- `project_key`
- `task_id`
- `version_id`
- `operator_id`
- `request_id`

约束如下：

- `project_id` 为真实主键，是服务端最终判定项目作用域的依据
- `project_key` 用于可读性和路径映射，服务端需校验其与 `project_id` 是否一致
- `request_id` 用于幂等和日志追踪
- 不允许存在“只有 task_id，没有 project_id”的写接口

### 6.2 生成接口契约

生成接口应以“当前项目 + 当前录制 + 当前版本”为最小作用域。

建议的请求结构如下：

```json
{
  "project_id": 1001,
  "project_key": "foradar",
  "task_id": 20001,
  "version_id": 30001,
  "scenario_desc": "创建资产探知任务",
  "start_url": "${BASE_URL}/workbench",
  "raw_script": "...",
  "step_model_json": {},
  "account_ref": "default-admin",
  "request_id": "req-20260331-0001"
}
```

建议的响应结构如下：

```json
{
  "project_scope": {
    "project_id": 1001,
    "project_key": "foradar",
    "workspace_root": "pw_projects/projects/foradar"
  },
  "task_id": 20001,
  "version_id": 30001,
  "version_status": "GENERATED",
  "files": [
    {
      "file_type": "spec",
      "relative_path": "tests/task/create-task.spec.ts",
      "content_hash": "sha256:..."
    },
    {
      "file_type": "page",
      "relative_path": "pages/TaskManagePage.ts",
      "content_hash": "sha256:..."
    }
  ],
  "manual_review_items": [],
  "risk_hints": [],
  "generation_summary": "..."
}
```

生成接口必须满足：

- 所有返回路径均为当前项目内相对路径
- 返回结果必须显式标明 `version_status`
- 若进入人工审核，必须返回 `manual_review_items`

### 6.3 验证接口契约

验证接口必须在当前项目工作区内执行指定 spec，不允许只传脚本内容临时跑。

建议的请求结构如下：

```json
{
  "project_id": 1001,
  "project_key": "foradar",
  "task_id": 20001,
  "version_id": 30001,
  "spec_relative_path": "tests/task/create-task.spec.ts",
  "request_id": "req-20260331-0002"
}
```

建议的响应结构如下：

```json
{
  "project_id": 1001,
  "task_id": 20001,
  "version_id": 30001,
  "version_status": "VALIDATE_SUCCESS",
  "success": true,
  "duration_ms": 18234,
  "assertions": [
    {
      "name": "校验创建结果",
      "passed": true
    }
  ],
  "screenshots": [],
  "logs": []
}
```

### 6.4 前端展示契约

前端在多项目版本中必须显式展示项目维度信息，不得只展示任务维度。

详情页最少展示以下数据块：

- `project_name / project_key`
- `task_id / version_id`
- `version_status`
- 文件树视图
- 代码内容视图
- `manual_review_items`
- 验证结果、截图、日志

前端规则：

- 文件树使用项目内相对路径展示，不拼接系统绝对路径
- `manual_review_items` 必须作为独立卡片展示，不允许埋在日志里
- 同一任务存在多个版本时，必须允许切换版本查看生成产物

### 6.5 数据库存储契约

建议沿用现有任务/版本模型，并补齐多项目字段和文件明细表。

最少需要满足以下数据契约：

1. `ai_script_task`
- `project_id`
- `current_version_id`
- `current_status`
- `latest_request_id`

2. `ai_script_version`
- `project_id`
- `project_key_snapshot`
- `version_status`
- `generation_summary`
- `manual_review_status`
- `registry_snapshot_json`
- `workspace_root_snapshot`
- `base_fixture_hash`

3. `ai_script_file` 新表
- `id`
- `project_id`
- `task_id`
- `version_id`
- `file_type` (`spec/page/shared/fixture/registry`)
- `relative_path`
- `content`
- `content_hash`
- `source_kind` (`create/update/generated/rebuilt`)
- `manual_review_required`

4. `ai_script_operation_log`
- `project_id`
- `task_id`
- `version_id`
- `operation_type`
- `status_from`
- `status_to`
- `detail_json`
- `request_id`

数据库规则：

- 所有核心表都必须带 `project_id`
- 文件明细以 `relative_path + version_id` 唯一约束
- 状态回写必须以 `version` 为核心，不允许只更新 `task` 不更新 `version`

---

## 七、程序合并算法（实施级）

### 7.1 合并目标分类

V1 阶段不同类型文件采用不同策略：

- `page create`：创建新文件
- `page update`：对已有页面执行程序化增量合并
- `spec file`：默认创建新文件；若目标已存在且非当前版本生成，进入 `manual_review`
- `registry`：程序对象合并
- `base.fixture.ts`：程序全量重建
- `auth.fixture.ts`：不参与 AI 合并

### 7.2 推荐实现方式

建议采用 `AST-first` 策略，不使用正则或整文件字符串替换作为主路径。

推荐实现：

- 由 Python / Go 主流程生成 `merge_plan`
- 调用 Node 合并器（如 `ts-morph`）执行 TypeScript AST 级修改
- 合并完成后再做 TypeScript 校验和格式化

如果 AST 解析失败或命中不支持场景：

- 不降级到盲改文本
- 直接转入 `manual_review`

### 7.3 页面文件合并步骤

对 `Page Object` 的增量更新建议按以下顺序执行：

1. 解析目标文件 AST
2. 校验类名是否与 registry 一致
3. 合并 import
4. 合并 `readonly xxx: Locator` 声明
5. 合并 constructor 内的 locator 初始化
6. 追加新 action 方法
7. 对允许 `extend_action_non_breaking` 的方法执行受控扩展
8. 输出代码并重新做语法校验

约束如下：

- 不允许删除已有成员
- 不允许移动人工代码块位置
- 不允许重写整个 class

### 7.4 Spec 文件策略

V1 阶段不对现有 spec 做复杂合并。

策略如下：

- 若目标 spec 不存在，则直接创建
- 若目标 spec 已存在，但 `content_hash` 与当前版本快照一致，可覆盖当前版本生成文件
- 若目标 spec 已存在且疑似人工改动，进入 `manual_review`

原因：

- spec 的人工语义通常更强
- spec 合并收益低于风险

### 7.5 Registry 与 Fixture 合并步骤

1. 校验 `registry_updates` 是否满足 schema
2. 将页面、shared、action、locator 元数据合并到当前项目 registry
3. 更新 `file_hash / last_generated_at / last_generated_by`
4. 根据最新 registry 全量重建 `base.fixture.ts`
5. 重新校验 import 和 `fixture_name` 唯一性

### 7.6 失败回退策略

以下任一情况发生时，合并流程必须停止并转入 `manual_review`：

- AST 解析失败
- 目标类名不一致
- `file_hash` 冲突
- 目标路径越界
- 需要删除或重命名现有成员
- 需要整文件重写
- Windows 环境下增量合并后出现中文 locator 或中文注释乱码

---

## 八、任务状态机（实施级）

### 8.1 任务与版本双层状态

多项目版本建议采用“任务状态 + 版本状态”双层模型。

任务状态用于前端总览，版本状态用于执行细节。

#### 任务状态建议值

- `RECORDING`
- `PROCESSING`
- `MANUAL_REVIEW`
- `READY`
- `FAILED`

#### 版本状态建议值

- `RECORDED`
- `GENERATING`
- `GENERATE_FAILED`
- `MANUAL_REVIEW_REQUIRED`
- `GENERATED`
- `VALIDATING`
- `VALIDATE_SUCCESS`
- `VALIDATE_FAILED`
- `ARCHIVED`

### 8.2 状态流转规则

建议流转如下：

1. 录制完成后，版本状态进入 `RECORDED`
2. 开始 AI 重构时，版本状态进入 `GENERATING`，任务状态进入 `PROCESSING`
3. 若生成命中冲突或硬约束失败，版本状态进入 `MANUAL_REVIEW_REQUIRED`，任务状态进入 `MANUAL_REVIEW`
4. 若生成成功，版本状态进入 `GENERATED`
5. 开始验证时，版本状态进入 `VALIDATING`
6. 验证通过后，版本状态进入 `VALIDATE_SUCCESS`，任务状态进入 `READY`
7. 验证失败后，版本状态进入 `VALIDATE_FAILED`，任务状态进入 `FAILED` 或 `MANUAL_REVIEW`
8. 老版本被替换或冻结后，可进入 `ARCHIVED`

### 8.3 人工审核触发条件

以下场景必须进入 `MANUAL_REVIEW_REQUIRED`：

- `file_hash` 冲突
- 路径越界
- 需要破坏性更新
- AST 合并失败
- `fixture_name` 冲突
- shared 抽象超出白名单
- 中文注释或硬约束校验未通过

### 8.4 人工审核后的动作

人工审核至少支持以下结果：

- `approve_and_retry`
- `edit_and_retry`
- `reject_version`
- `archive_version`

对应规则：

- `approve_and_retry`：允许在保留当前版本号或新版本号策略下重新生成
- `edit_and_retry`：人工修正后重新进入 `GENERATING` 或 `VALIDATING`
- `reject_version`：版本进入终态，不再自动推进
- `archive_version`：版本冻结，仅保留追溯信息

### 8.5 日志与可追溯性

每次状态变化都必须写入 `ai_script_operation_log`，至少记录：

- `project_id`
- `task_id`
- `version_id`
- `request_id`
- `status_from`
- `status_to`
- `detail_json`

---

## 九、同项目并发保护（实施级）

### 9.1 V1 并发策略

V1 采用保守并发策略：`同一项目的写操作串行化`。

含义如下：

- 不同项目之间允许并行生成和验证
- 同一项目内的生成 / 合并 / registry 更新 / fixture 重建必须串行执行
- 同一项目内的验证建议与写操作串行，避免读到半成品工作区

### 9.2 锁粒度建议

V1 建议使用项目级工作区锁：

- `lock_key = project:<project_id>`
- `lock_type = workspace_write`

可选增加：

- `lock_type = validate_run`

但若实现复杂度高，V1 可直接统一为一个项目级串行锁。

### 9.3 锁表建议

建议新增 `ai_script_workspace_lock` 表，最少字段如下：

- `project_id`
- `lock_key`
- `lock_type`
- `owner_task_id`
- `owner_version_id`
- `owner_request_id`
- `heartbeat_at`
- `expires_at`
- `status`

### 9.4 写操作执行顺序

同项目写操作建议按以下顺序执行：

1. 获取项目工作区锁
2. 读取当前项目 registry 最新快照
3. 校验 `file_hash`、路径作用域、状态合法性
4. 生成 `merge_plan`
5. 写入临时文件
6. 原子替换目标文件
7. 更新 registry
8. 重建 `base.fixture.ts`
9. 释放项目工作区锁

### 9.5 失效锁处理

- 锁必须带 TTL
- 长任务必须定期 heartbeat
- 服务异常退出后，可由守护任务清理过期锁
- 清理过期锁前必须确认关联任务和版本不再运行

### 9.6 幂等与乐观校验

即使拿到锁，写入前仍需做乐观校验：

- 当前 `version_status` 是否允许写
- 目标文件 `file_hash` 是否仍与读取时一致
- 当前项目 registry 版本号是否变化

若任一校验失败：

- 不继续写入
- 进入 `manual_review` 或重新排队

### 9.7 原子写入规则

- 页面文件、registry、fixture 都先写临时文件
- 通过校验后再原子替换目标文件
- 不允许边改边写正式文件
- 不允许多个协程同时改同一项目工作区

---
## 十、实施路线（多项目 V1）

### 阶段 1：项目工作区与元数据隔离

- [ ] 引入 `ProjectScope`，统一解析 `project_id / project_key`
- [ ] 创建 `pw_projects/projects/<project_key>/` 目录约定
- [ ] 初始化项目级 `project.json`
- [ ] 初始化项目级 `page-registry.json` V3 结构
- [ ] 创建项目级 `playwright.config.ts`、`.env.example`、`auth_states/`
- [ ] 创建固定 shared 白名单骨架：`NavigationPage`、`DialogPage`、`ToastPage`、`LoginPage`

### 阶段 2：多文件生成与程序合并

- [ ] 新增 `PageContext` 提取逻辑
- [ ] 新增 `refactor_recorded_script_v2(project_scope, ...)`
- [ ] LLM 输出 `{spec_file, page_creates, page_updates, registry_updates, manual_review_items}`
- [ ] 增加 JSON Schema 校验、路径作用域校验和代码硬约束校验
- [ ] 程序执行页面增量合并、更新项目 registry、重建项目 `base.fixture.ts`
- [ ] Go 后端适配多项目字段和 `manual_review` 返回结构
- [ ] 前端适配项目维度展示、多文件展示和人工 review 提示

### 阶段 3：多项目执行闭环

- [ ] 适配 `validation_runner.py`，在项目工作区执行指定 spec
- [ ] **场景 1**：项目 A 生成业务 POM + spec，并只更新项目 A 的 registry / fixture
- [ ] **场景 2**：项目 B 与项目 A 同名页面并存，但互不冲突
- [ ] **场景 3**：同项目复用已有 POM，只新增 spec 或执行非破坏性增量 update
- [ ] **场景 4**：`file_hash` 冲突、`fixture_name` 冲突、路径越界时进入 `manual_review`
- [ ] 向后兼容验证：旧的 flat 脚本仍可正常执行

---

## 十一、风险与应对

| 风险 | 影响 | 应对策略 |
|------|------|----------|
| 项目串写 | 项目 A 的生成结果误写到项目 B | `ProjectScope` 强制解析 + 路径作用域校验 |
| LLM 输出结构不稳定 | JSON 解析失败、字段缺失、路径错误 | JSON Schema 校验 + 程序硬约束校验 + fallback / manual_review |
| 页面归属误判 | 重复创建 POM 或错误复用已有页面 | `page_identity` + 评分规则 + 项目级 registry 比对 |
| 增量更新误伤人工代码 | 覆盖人工维护逻辑，导致历史用例回归 | `append_only` + `non_breaking_only` + `file_hash` 检测 + `manual_review` |
| Shared 抽象过度 | 业务逻辑污染 shared，后续难维护 | 固定 shared 白名单 + `candidate_shared` 仅提示不自动创建 |
| Fixture 漂移 | 注入关系错误，导致执行失败 | 项目级 `base.fixture.ts` 由 registry 全量重建，禁止 LLM 直接编辑 |
| 认证态串项目 | A 项目 auth_state 被 B 项目误用 | `auth_states/` 按项目隔离，验证器按项目装载 |
| 向后兼容失败 | 已有 flat 脚本回归受阻 | 保留 V1 单文件模式作为 fallback |

---

## 十二、成功标准

1. **多项目隔离生效**：项目 A 和项目 B 可以同时存在，拥有各自独立的 `pages / tests / fixtures / registry / auth_states`
2. **项目级生成生效**：录制任务必须先绑定 `project_id`，生成结果只能写入对应项目工作区
3. **同项目复用生效**：后续同项目同模块用例优先 `reuse`，缺失能力时只允许追加 locator / action 或执行非破坏性扩展
4. **跨项目不串写**：即使两个项目都存在 `TaskManagePage`，也不会发生 registry、fixture、源码覆盖冲突
5. **人工代码保护生效**：当检测到 `file_hash` 冲突、路径越界或需要破坏性更新时，系统进入 `manual_review`，不得覆盖人工代码
6. **硬约束校验生效**：生成代码必须通过 `3.11` 校验，确保 spec 无 locator、方法带中文注释、无 `waitForTimeout` / `test.only` 等禁项
7. **验证闭环生效**：Validation Runner 能在对应项目工作区执行指定 spec，旧 flat 脚本仍可继续运行
