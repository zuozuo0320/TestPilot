# 测试管理平台-用例评审模块 OpenAPI v0.2 增量文档

> 版本：v0.2（更新于 2026-04-23）
> 基线：`TestPilot/docs/测试管理平台-用例评审模块OpenAPI接口文档-20260404.md`（v1.1）
> 定位：在 v1.1 基础上**只记录增量**，v1.1 已有的接口若未出现在本文档即保持不变
> 关联需求：`TestPilot/docs/测试管理平台-用例评审角色职责需求方案-20260423.md`

## 1. 新增枚举

### 1.1 `ai_gate_status`（`case_review_items.ai_gate_status`）

| 枚举值 | 说明 |
|---|---|
| `not_started` | 未开始（默认值） |
| `running` | 规则/LLM 运行中（Phase 1 持续时间可忽略） |
| `passed` | 通过 |
| `failed` | 未通过：存在 `critical` 或 `major` finding |
| `timeout` | 超时（Phase 2 接入 LLM 时使用） |
| `bypassed` | Moderator 手动放行 |

### 1.2 `review_role`（`case_review_item_reviewers.review_role`）

| 枚举值 | 说明 |
|---|---|
| `primary` | 主评人，一个评审项**唯一**，决策者 |
| `shadow` | 陪审，多人，仅建议不决策 |

### 1.3 `severity`（`case_review_defects.severity`）

| 枚举值 | 说明 |
|---|---|
| `critical` | 严重，阻断重提 |
| `major` | 主要，门禁失败 |
| `minor` | 提示 |

### 1.4 `defect_status`（`case_review_defects.status`）

| 枚举值 | 说明 |
|---|---|
| `open` | 待处理（默认） |
| `resolved` | Author 已处理 |
| `disputed` | Author 对缺陷提异议，需 Moderator 裁决 |

### 1.5 `defect_source`（`case_review_defects.source`）

| 枚举值 | 说明 |
|---|---|
| `primary_review` | 主评人驳回生成 |
| `ai_gate` | AI 门禁（规则引擎/LLM）失败生成 |

## 2. 新增错误码

| 错误码 | 常量 | 说明 |
|---|---|---|
| `120201` | `CodeReviewSelfReviewForbidden` | 禁止自审：主评人/陪审不能是用例作者（`allow_self_review` 未开启） |
| `120202` | `CodeReviewPrimaryRequired` | 必须指定唯一主评人 |
| `120203` | `CodeReviewPrimaryDuplicate` | 同一评审项存在多个 Primary |
| `120204` | `CodeReviewRoleInvalid` | `review_role` 枚举非法 |
| `120205` | `CodeReviewSeverityInvalid` | `severity` 枚举非法 |
| `120206` | `CodeReviewDefectNotFound` | Action Item 不存在 |
| `120207` | `CodeReviewDefectStatusInvalid` | Action Item 状态不允许该操作 |
| `120208` | `CodeReviewAIGateBlocked` | AI 门禁失败，请先处理阻断缺陷 |

## 3. 核心对象增量

### 3.1 ReviewSummary 新增字段

```json
{
  "moderator_id": 3,
  "moderator_name": "王五",
  "ai_enabled": true,
  "revision_round": 1,
  "default_primary_reviewer_id": 7
}
```

- `moderator_id`：计划仲裁人（默认等于创建者，用于异议裁决、门禁放行）
- `moderator_name`：仲裁人昵称（响应层回填，写入时忽略）
- `ai_enabled`：是否启用 AI 门禁（创建时默认 `true`）
- `revision_round`：当前最大修订轮次（跟随任一评审项增长）
- `default_primary_reviewer_id`：默认 Primary（用于批量指派，缺省取 `default_reviewer_ids[0]`）

### 3.2 ReviewItem 新增字段

```json
{
  "ai_gate_status": "failed",
  "ai_report_id": 0,
  "revision_round": 1
}
```

- `ai_gate_status`：见 §1.1
- `ai_report_id`：关联 `case_review_ai_reports.id`（Phase 2 启用，Phase 1 恒为 0）
- `revision_round`：当前修订轮次，Author 每次重提后 +1

### 3.3 ReviewItemReviewer 新增字段

```json
{
  "review_role": "primary"
}
```

### 3.4 CaseReviewDefect（v0.2 新增对象）

```json
{
  "id": 501,
  "review_id": 101,
  "review_item_id": 1001,
  "project_id": 1,
  "record_id": 9001,
  "source": "primary_review",
  "title": "步骤描述模糊",
  "severity": "major",
  "status": "open",
  "dispute_reason": "",
  "resolve_note": "",
  "created_by": 7,
  "created_at": "2026-04-23T10:00:00+08:00",
  "resolved_by": 0,
  "resolved_by_name": "",
  "resolved_at": null,
  "updated_at": "2026-04-23T10:00:00+08:00"
}
```

- `source=primary_review` 时 `record_id` 非零，指向主评人驳回记录
- `source=ai_gate` 时 `record_id=0`

### 3.5 ProjectSettings（v0.2 新增对象）

```json
{
  "allow_self_review": false
}
```

- `allow_self_review`：项目级是否允许自审；默认 `false`，开启后 Author 可同时作为 Primary/Shadow

## 4. 请求体增量

### 4.1 `POST /projects/{projectID}/case-reviews`

新增可选字段：

```json
{
  "default_primary_reviewer_id": 7,
  "moderator_id": 3,
  "ai_enabled": true,
  "auto_submit": false
}
```

- 未传 `default_primary_reviewer_id` 时，服务端自动取 `default_reviewer_ids[0]`
- 未传 `moderator_id` 时，默认等于创建者
- 未传 `ai_enabled` 时，默认为 `true`

### 4.2 `POST /projects/{projectID}/case-reviews/{reviewID}/items/link`

`items[]` 新增可选字段：

```json
{
  "items": [
    {
      "testcase_id": 201,
      "primary_reviewer_id": 7,
      "shadow_reviewer_ids": [9, 11]
    }
  ],
  "auto_submit": false
}
```

- `primary_reviewer_id` 缺省时使用计划的 `default_primary_reviewer_id`
- `shadow_reviewer_ids` 缺省时使用 `default_reviewer_ids` 的其余成员
- 兼容字段 `reviewer_ids`（v0.1 语义）仍生效：首元素作 Primary，其余作 Shadow

## 5. 新增接口

### 5.1 规则引擎

#### `POST /projects/{projectID}/case-reviews/{reviewID}/items/{itemID}/ai-gate/rerun`

手动触发 Layer 1 规则引擎重跑；幂等（已 resolved/disputed 的 ai_gate 缺陷不会被清除）。

**响应**

```json
{
  "code": 0,
  "data": {
    "item_id": 1001,
    "ai_gate_status": "failed",
    "passed": false,
    "findings": [
      {
        "id": "RULE_STEPS_REQUIRED",
        "rule": "步骤必填",
        "message": "用例步骤为空",
        "severity": "critical",
        "field": "steps"
      }
    ],
    "critical_count": 1,
    "major_count": 0,
    "minor_count": 0,
    "defect_ids": [501],
    "run_at": "2026-04-23T10:00:00+08:00"
  }
}
```

**规则 ID 清单（Phase 1）**

| ID | severity | 字段 | 阻断 |
|---|---|---|---|
| `RULE_TITLE_REQUIRED` | `critical` | `title` | 是 |
| `RULE_TITLE_LEN_MAX` | `minor` | `title` | 否 |
| `RULE_PRECONDITION_REQUIRED` | `major` | `precondition` | 是 |
| `RULE_STEPS_REQUIRED` | `critical` | `steps` | 是 |
| `RULE_STEPS_MIN_LEN` | `major` | `steps` | 是 |
| `RULE_POSTCONDITION_REQUIRED` | `minor` | `postcondition` | 否 |
| `RULE_LEVEL_REQUIRED` | `major` | `level` | 是 |

`minor` 级别 finding 仅作提示，不产生 Action Item。

### 5.2 Action Items（评审缺陷）

#### `GET /projects/{projectID}/case-reviews/{reviewID}/defects`

计划级 Action Items 分页列表。

**Query 参数**

| 名称 | 类型 | 说明 |
|---|---|---|
| `source` | string | 过滤 `primary_review` / `ai_gate` |
| `status` | string | 过滤 `open` / `resolved` / `disputed` |
| `severity` | string | 过滤 `critical` / `major` / `minor` |
| `page` / `pageSize` | int | 分页（默认 1/20） |

响应：标准分页结构 + `data` 为 `CaseReviewDefect[]`。

#### `GET /projects/{projectID}/case-reviews/{reviewID}/items/{itemID}/defects`

单评审项下的 Action Items（按创建时间升序），返回 `{items: CaseReviewDefect[]}`。

#### `GET /projects/{projectID}/case-review-defects/{defectID}`

查询单条 Action Item。

#### `POST /projects/{projectID}/case-review-defects/{defectID}/resolve`

Author 标记为已解决。仅当 `status=open` 时允许；成功后 `status=resolved`。

请求体：

```json
{ "note": "已补充登录失败分支" }
```

#### `POST /projects/{projectID}/case-review-defects/{defectID}/dispute`

Author 对 Action Item 提异议。仅当 `status=open` 时允许；成功后 `status=disputed`。

请求体：

```json
{ "reason": "建议 Moderator 复核，该步骤已足够明确" }
```

`reason` 必填。

#### `POST /projects/{projectID}/case-review-defects/{defectID}/reopen`

Moderator 将 Action Item 重置为 `open`。当前 Phase 1 未限定角色，Phase 2 接入 RBAC 校验。

### 5.3 项目级 Settings

#### `GET /projects/{projectID}/settings`

返回 `ProjectSettings`。

#### `PUT /projects/{projectID}/settings`

请求体：

```json
{ "allow_self_review": true }
```

字段使用指针语义：`null`（未传）不修改，`false`/`true` 才是显式变更。响应返回完整 `ProjectSettings` 以便前端即时回显。

## 6. 行为变更

### 6.1 自审拦截

创建评审 / 关联用例时，若未开启 `allow_self_review`，则 Primary 或 Shadow 列表中包含用例作者（`testcase.created_by`）将返回：

```json
{
  "code": 120201,
  "message": "主评人不能是用例作者（allow_self_review 未开启）"
}
```

### 6.2 CopyReview 评审人角色保留

复制评审（`POST /case-reviews/{reviewID}/copy`）现在会保留原计划的 `review_role`。历史数据若 `review_role=""`，默认按 `primary` 写入。

### 6.3 规则引擎自动执行时机

Phase 1 不在创建评审项时自动触发规则引擎（避免事务耗时），完全依赖前端主动调用 §5.1 的 rerun 接口或通过 UI 手动执行。

Phase 2 会引入后台异步 worker 在 `link items` 后自动 enqueue 规则/LLM 任务。
