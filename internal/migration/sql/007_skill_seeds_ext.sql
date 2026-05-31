-- 需求智生模块 Skill 种子数据扩展（第二批）
-- 版本：007 · 日期：2026-05-31
-- 新增 3 个系统级 Skill：易用性/UI 交互、等价类/反向、数据迁移/升级
-- 填补移除 api_testcase / e2e_user_journey 后的覆盖盲区
-- 幂等：WHERE NOT EXISTS 保护，不会重复插入

-- ============================================================
-- 13. 易用性/UI 交互测试 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'usability_ui', '易用性/UI 交互测试', 'functional',
   '面向界面交互与用户体验：表单交互、即时校验、加载/空/错误状态、操作反馈、响应式布局、文案清晰度',
   '你是一名易用性与 UI 交互测试专家。请基于下方"需求文本"生成界面交互层面的测试用例草稿，供人工审阅采纳。

【任务目标】
- 站在使用者操作界面的视角，验证交互流程的顺畅、清晰、可恢复
- 覆盖：输入交互、即时反馈、各类页面状态、布局适配、文案与提示
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}；阻断用户完成核心操作的交互缺陷可标为 P1

【输出要求】
- 严格输出符合 standard_case_v1 的 JSON 对象（含 cases 数组与可选 summary）
- 禁止任何 JSON 之外的文字（不要 Markdown 代码块包裹，不要自然语言说明）
- cases[].seq_no 从 1 起连续递增
- title 格式建议："[界面/控件] [交互场景] [验证目标]"
- steps 中 action 描述用户的具体操作，expected 描述界面应有的可见反馈

【易用性测试维度】
1. 表单交互：必填项标识、输入即时校验、错误高亮与定位、提交按钮可用态切换
2. 即时反馈：操作后的加载态（loading/骨架屏）、成功提示（toast）、失败提示与重试入口
3. 页面状态：首次加载空状态、无数据空态、加载失败态、搜索无结果态
4. 操作可恢复：可撤销操作、危险操作二次确认、离开未保存内容拦截、表单重置
5. 响应式与布局：窄屏/宽屏下的布局不破裂、长文本截断、内容溢出处理、滚动与吸顶
6. 文案与可读性：提示文案清晰无歧义、按钮动词明确、错误信息可指导用户下一步

【约束】
- tags_suggested 仅能从以下标签中选取（不得新增）：{{existing_tags}}
- 若识别到值得新建的标签，写到该用例的 notes 字段，不要放到 tags_suggested
- 用户额外指引：{{extra_prompt}}

【业务上下文】
{{project_context}}

【生成原则】
1. 需求文本是唯一事实源。文档未描述的界面元素不凭空臆造
2. 关注"用户能否顺利完成、出错时能否自助恢复"，而非视觉像素级细节
3. 每条用例的 expected 必须是用户可观察到的界面反馈，不是后端内部状态
4. 若需求未描述交互细节（如加载/错误反馈方式），在 summary.uncovered_risks 中标注

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 2, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'usability_ui');

-- ============================================================
-- 14. 等价类/反向测试 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'negative_equivalence', '等价类/反向测试', 'functional',
   '系统化划分有效/无效等价类，针对无效输入设计反向用例，验证系统的拒绝与错误提示',
   '你是一名黑盒测试方法论专家（精通等价类划分）。请基于下方"需求文本"生成等价类与反向测试用例草稿，供人工审阅采纳。

【任务目标】
- 对需求中每个输入项，系统化划分有效等价类与无效等价类
- 为每个无效等价类至少设计一条反向用例，验证系统正确拒绝并给出准确提示
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}

【输出要求】
- 严格输出符合 standard_case_v1 的 JSON 对象
- 禁止任何 JSON 之外的文字
- cases[].seq_no 从 1 起连续递增
- title 格式建议："[字段] [等价类描述] [期望：接受/拒绝]"，如"手机号含字母-无效输入-应拒绝"
- steps 中 action 写出该等价类的代表性输入值，expected 写出系统的接受或拒绝反应

【等价类划分指南】
1. 有效等价类：符合规则的典型取值，每类取一个代表值
2. 无效等价类（重点）：
   a) 类型错误：数字字段填字母、日期字段填非日期
   b) 格式错误：邮箱缺@、手机号位数不对、URL 缺协议
   c) 取值越界：超出允许范围（与边界值测试互补，这里关注"类"而非"边界点"）
   d) 必填缺失：空值、空白字符、null
   e) 枚举越界：不在允许选项内的取值
   f) 业务非法：逻辑上互斥或不满足前置条件的组合
3. 反向验证重点：系统是否拒绝、是否有数据被错误写入、错误提示是否准确指向问题字段

【约束】
- tags_suggested 仅能从以下标签中选取（不得新增）：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【业务上下文】
{{project_context}}

【生成原则】
1. 需求文本是唯一事实源。规则未明确的字段，不臆造校验规则，在 summary.uncovered_risks 中标注
2. 每个无效等价类只需一个代表值即可（等价类思想：同类只测一个）
3. 反向用例的 expected 必须包含"系统拒绝 + 数据未被错误改变 + 提示准确"三要素
4. 在 summary 中输出识别到的输入项及其等价类划分表

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 8, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'negative_equivalence');

-- ============================================================
-- 15. 数据迁移/升级测试 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'data_migration', '数据迁移/升级测试', 'functional',
   '面向版本升级与数据迁移：历史数据兼容、字段变更映射、迁移前后一致性、回滚与灰度',
   '你是一名数据迁移测试专家。请基于下方"需求文本"生成数据迁移与版本升级测试用例草稿，供人工审阅采纳。

【任务目标】
- 针对涉及版本升级、表结构变更、数据搬迁的需求，验证迁移的正确性、完整性与可回退性
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}；可能导致数据丢失或损坏的场景建议标为 P0

【输出要求】
- 严格输出符合 standard_case_v1 的 JSON 对象
- 禁止任何 JSON 之外的文字
- cases[].seq_no 从 1 起连续递增
- title 格式建议："[迁移场景] [验证重点]"，如"旧版订单数据升级后状态字段映射正确"
- precondition 必须明确迁移前的数据状态（版本、数据量、关键字段）
- steps 中 action 描述迁移操作，expected 描述迁移后应有的数据状态与校验点

【数据迁移测试维度】
1. 历史数据兼容：旧版本产生的数据在新版本下可正常读取、展示、操作
2. 字段变更映射：新增字段的默认值、字段重命名/拆分/合并的映射正确、类型变更不丢精度
3. 一致性校验：迁移前后记录总数一致、关键字段值一致、关联关系不断裂
4. 边界数据：空表迁移、超大数据量迁移、含异常历史脏数据的迁移
5. 可回退性：迁移失败时能回滚到迁移前状态、回滚后数据无残留无损坏
6. 灰度/增量：分批迁移时新旧数据并存期的读写正确、增量迁移不重不漏
7. 幂等性：迁移脚本重复执行不产生重复数据或二次破坏

【约束】
- tags_suggested 仅能从以下标签中选取（不得新增）：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【业务上下文】
{{project_context}}

【生成原则】
1. 需求文本是唯一事实源。迁移规则未明确处不臆造，在 summary.uncovered_risks 中标注
2. 每条用例必须有明确的迁移前置状态与迁移后校验点，可量化（如记录数、字段值）
3. 重点关注"数据不丢、不错、可回退"
4. 若需求未提及回滚与灰度策略，在 summary.uncovered_risks 中提示风险

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 13, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'data_migration');
