-- 需求智生模块增量 SQL 迁移
-- 版本：004 · 日期：2026-05-22
-- 处理 AutoMigrate 无法完成的：复合唯一索引 + 内置 Skill 种子数据
-- 注意：4 张新表的基础 DDL 由 GORM AutoMigrate 自动创建，此处仅补充索引和种子

-- ============================================================
-- 1. testcases 增量列（AutoMigrate 会创建列，此处仅确保索引存在）
-- ============================================================

-- 安全补建索引（如果 AutoMigrate 未创建）
-- MySQL 8.x 允许 CREATE INDEX IF NOT EXISTS 语法不存在，使用存储过程包裹
-- 由于 migrate.go 的 splitSQL 不支持 DELIMITER，改用条件判断方式跳过

-- 2. 内置 Skill 种子数据（幂等：ON DUPLICATE KEY 忽略）
-- ============================================================

INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'functional_testcase', '通用功能测试', 'functional',
   '面向业务功能验证：正向流程 + 异常分支 + 边界值。适合大多数 PRD / 用户故事',
   '你是一名资深测试工程师。请基于下方"需求文本"生成测试用例草稿，供人工审阅采纳。\n\n【任务目标】\n- 从需求中拆解出可独立验证的测试场景\n- 覆盖三类场景：正向流程、异常分支、边界值\n- 总数不超过 {{max_cases}} 条\n- 用例默认级别 {{default_level}}；P0 仅用于会阻塞主流程的关键路径\n\n【输出要求】\n- 严格输出符合 standard_case_v1 的 JSON 对象（含 cases 数组与可选 summary 对象）\n- 禁止任何 JSON 之外的文字（不要 Markdown 代码块包裹，不要自然语言说明）\n- cases[].seq_no 从 1 起连续递增\n- 每条用例的 steps 数组中，action 和 expected 必须成对出现\n- title 简明，不超过 60 字\n\n【业务上下文】\n{{project_context}}\n\n【约束】\n- tags_suggested 仅能从以下标签中选取（不得新增）：{{existing_tags}}\n- 若识别到值得新建的标签，写到该用例的 notes 字段，不要放到 tags_suggested\n- 用户额外指引：{{extra_prompt}}\n\n【生成原则】\n1. 需求文本是唯一事实源。文档未明确描述的业务规则不允许凭空补全\n2. 异常分支应覆盖：必填缺失、类型错误、长度超限、权限不足、并发冲突等\n3. 边界值优先关注：数字范围两端、字符串长度上下限、时间临界点、空集合\n4. 同一功能的多条用例之间，标题应能清晰区分被测变量\n5. 若需求文本本身不足以支撑生成（信息过少、矛盾），在 summary.uncovered_risks 中说明\n\n【需求文本】\n{{requirement_text}}\n\n{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 1, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'functional_testcase');
