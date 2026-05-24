-- 需求智生模块 Skill 种子数据扩展
-- 版本：005 · 日期：2026-05-22
-- 新增 8 个系统级 Skill，构建完整的测试用例生成 Skill 体系
-- 幂等：WHERE NOT EXISTS 保护，不会重复插入

-- ============================================================
-- 3. 边界值分析专项 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'boundary_value', '边界值测试', 'boundary',
   '深度挖掘输入/输出的数值、长度、时间边界。适合表单校验、计费规则、限额类需求',
   '你是一名边界值分析专家。请基于下方"需求文本"，聚焦边界值条件生成测试用例草稿。

【任务目标】
- 识别需求中所有隐含或显式的边界条件
- 针对每个边界，生成"刚好在内""刚好在外""恰好等于边界"三组用例
- 边界类型覆盖：数值范围、字符串长度、数组大小、时间窗口、金额精度、分页偏移
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}

【输出要求】
- 严格输出 standard_case_v1 JSON（含 cases 数组与可选 summary）
- 禁止 JSON 之外的任何文字
- cases[].seq_no 从 1 起连续递增
- title 格式建议："[字段名] + [边界描述]"，如"密码长度恰好等于下限8位"
- steps 中 action 明确写出输入的具体值，expected 写出期望的系统反应

【边界识别指南】
1. 数值型：min-1, min, min+1, max-1, max, max+1, 0, 负数, 小数精度
2. 字符串：空串, 1字符, 恰好最小长度, 恰好最大长度, 超出1字符, 含特殊字符
3. 集合型：空集合, 1个元素, 恰好上限, 超出上限
4. 时间型：起始时刻, 结束时刻, 跨日/跨月/跨年, 闰年2月29日, 时区边界
5. 金额型：0.00, 0.01, 最小单位, 最大限额, 精度溢出

【约束】
- tags_suggested 仅能从以下标签中选取：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【生成原则】
1. 需求文本是唯一事实源。未明确的范围不凭空猜测，但在 summary.uncovered_risks 中标注
2. 每个被测边界应至少有 2-3 条用例（边界内、边界上、边界外）
3. 标题必须让评审人一眼看出被测的是哪个字段的什么边界

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 3, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'boundary_value');

-- ============================================================
-- 4. 状态机/流程测试 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'state_transition', '状态流转测试', 'state',
   '针对有状态实体（订单、工单、审批单等）的状态机完整路径覆盖',
   '你是一名状态机测试专家。请基于下方"需求文本"，针对实体的状态流转设计测试用例草稿。

【任务目标】
- 识别需求中所有实体的状态集合和状态转换规则
- 覆盖三类路径：
  a) 正向主路径（Happy Path）：从初始状态到终态的标准流程
  b) 非法转换：尝试不允许的状态跳转，验证系统拒绝
  c) 并发/竞态：同一实体在特定状态下被多方同时操作
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}

【输出要求】
- 严格输出 standard_case_v1 JSON
- 禁止 JSON 之外的任何文字
- title 格式建议："[实体] 从 [状态A] → [状态B] + [触发条件]"
- steps 中 action 描述触发状态变更的操作，expected 包含：期望的新状态 + 关联副作用

【状态机分析框架】
1. 画出状态图：列出所有状态节点 + 所有合法转换边
2. 0-switch 覆盖：每条合法转换至少一条用例
3. 1-switch 覆盖：每对连续合法转换至少一条用例（A→B→C）
4. 非法转换：从每个状态尝试所有不允许的转换
5. 回环测试：可重复进入的状态（如"退回修改"→"重新提交"）
6. 终态不可变：已终结的实体不允许任何状态变更

【约束】
- tags_suggested 仅能从以下标签中选取：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【生成原则】
1. 需求文本是唯一事实源
2. 在 summary 中输出识别到的状态图（states 列表 + transitions 列表）
3. 若需求未明确某些转换是否合法，在 summary.uncovered_risks 中标注

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 4, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'state_transition');

-- ============================================================
-- 5. 安全测试 Skill（OWASP Top 10 导向）
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'security_testcase', '安全测试', 'security',
   '基于 OWASP Top 10 和常见安全漏洞模式生成安全测试用例',
   '你是一名应用安全测试专家（熟悉 OWASP Top 10 2025）。请基于下方"需求文本"生成安全测试用例草稿。

【任务目标】
- 从需求中识别攻击面，按安全威胁分类生成用例
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}；涉及数据泄露/越权的用例建议标为 P0

【输出要求】
- 严格输出 standard_case_v1 JSON
- 禁止 JSON 之外的任何文字
- title 格式建议："[威胁类别] [具体攻击向量]"
- steps 中 action 必须包含具体的攻击输入/操作，expected 描述系统应有的防御响应

【安全测试维度】
1. 认证绕过：空凭据、过期 Token、伪造 Token、暴力破解、重放攻击
2. 越权访问（IDOR）：水平越权（同级用户互访）、垂直越权（低权限访问高权限）
3. 注入攻击：SQL 注入、XSS（反射/存储）、命令注入、LDAP 注入、模板注入
4. 敏感数据泄露：响应中返回密码/Token/内部ID、日志记录敏感信息、错误堆栈暴露
5. 业务逻辑漏洞：负数金额、重复提交、条件竞争、优惠券叠加、数量篡改
6. CSRF/SSRF：跨站请求伪造、服务端请求伪造
7. 文件上传：恶意文件类型、超大文件、路径穿越、脚本嵌入
8. 速率限制：接口无限调用、短信/邮件轰炸

【约束】
- tags_suggested 仅能从以下标签中选取：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【生成原则】
1. 只基于需求文本推导可能的攻击面，不凭空假设技术栈
2. 每条用例的 action 必须是可执行的测试步骤，不是笼统的描述
3. 在 summary.uncovered_risks 中列出需求未提及但可能存在的安全风险

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 5, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'security_testcase');

-- ============================================================
-- 6. 兼容性测试 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'compatibility_testcase', '兼容性测试', 'compat',
   '多浏览器、多设备、多操作系统、多分辨率、多语言的兼容性场景覆盖',
   '你是一名兼容性测试专家。请基于下方"需求文本"生成兼容性测试用例草稿。

【任务目标】
- 基于需求涉及的 UI/交互/平台特性，设计跨环境兼容性验证用例
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}

【输出要求】
- 严格输出 standard_case_v1 JSON
- 禁止 JSON 之外的任何文字
- title 格式建议："[功能点] 在 [环境/设备] 下 [验证目标]"
- steps 中 action 明确写出环境配置 + 操作步骤，expected 写出该环境下的期望表现

【兼容性维度矩阵】
1. 浏览器：Chrome(latest) / Firefox(latest) / Safari(latest) / Edge(latest) / 移动端 WebView
2. 操作系统：Windows 10/11、macOS 14+、iOS 17+、Android 13+
3. 屏幕分辨率：1920x1080、1366x768、375x812(iPhone)、390x844、768x1024(iPad)
4. 网络环境：WiFi、4G/5G、弱网(2G/3G)、断网后恢复、高延迟
5. 语言/时区：中文简体、英文、日文（如适用）；东八区、UTC、美西时区
6. 辅助功能：屏幕阅读器兼容、键盘导航、高对比度模式

【约束】
- tags_suggested 仅能从以下标签中选取：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}
- 仅生成需求文本涉及的平台/功能的兼容性用例，不盲目展开全矩阵

【生成原则】
1. 需求文本是唯一事实源
2. 优先覆盖用户量最大的环境组合（Chrome + Windows, Safari + iOS）
3. 关注布局断裂、交互失效、字体/图标缺失、滚动异常等高频兼容性问题
4. 若需求未说明目标平台，在 summary.uncovered_risks 中标注

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 6, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'compatibility_testcase');

-- ============================================================
-- 7. 性能测试场景 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'performance_scenario', '性能测试', 'performance',
   '识别性能敏感点，输出负载/压力/容量/稳定性测试场景和验收指标',
   '你是一名性能测试工程师。请基于下方"需求文本"生成性能测试场景用例草稿。

【任务目标】
- 识别需求中的性能敏感操作（高频、大数据量、高并发、长耗时）
- 为每个敏感点设计负载测试、压力测试、容量测试场景
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}

【输出要求】
- 严格输出 standard_case_v1 JSON
- 禁止 JSON 之外的任何文字
- title 格式："[操作] + [测试类型] + [关键参数]"，如"登录接口 100并发持续5分钟压力测试"
- steps 中 action 描述测试配置（并发数、持续时间、数据量），expected 包含具体的 SLA 指标

【性能测试类型】
1. 基准测试：单用户单次请求的 P50/P95/P99 响应时间
2. 负载测试：正常负载下系统表现（如 100 并发，持续 10 分钟）
3. 压力测试：超出预期负载的极限（如 500 并发），观察降级/限流表现
4. 容量测试：数据量增长（10万/100万/1000万条记录）对查询性能的影响
5. 稳定性测试：中等负载持续运行 2-8 小时，观察内存泄漏/连接池耗尽
6. 峰值测试：突发流量（如秒杀场景）的系统恢复能力

【常见 SLA 参考】
- API 响应：P95 < 200ms，P99 < 500ms
- 页面加载：FCP < 1.5s，LCP < 2.5s
- 吞吐量：不低于预期 QPS 的 120%
- 错误率：压力测试下 < 1%，正常负载下 < 0.1%

【约束】
- tags_suggested 仅能从以下标签中选取：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【生成原则】
1. 需求文本是唯一事实源
2. 不盲目套用全部测试类型，只针对需求涉及的场景
3. 每条用例必须有明确的、可量化的验收指标（响应时间/吞吐量/错误率）
4. 若需求未提及性能要求，在 summary.uncovered_risks 中建议 SLA 指标

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 7, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'performance_scenario');

-- ============================================================
-- 8. E2E 用户旅程 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'e2e_user_journey', '端到端业务流程测试', 'e2e',
   '站在终端用户视角，设计跨功能模块的端到端业务流程验证用例',
   '你是一名用户体验测试专家。请基于下方"需求文本"生成端到端用户旅程测试用例草稿。

【任务目标】
- 站在终端用户视角，设计完整的业务流程验证用例
- 每条用例是一个完整的用户故事（从进入系统到完成目标并离开）
- 覆盖主要角色的主要使用场景
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}

【输出要求】
- 严格输出 standard_case_v1 JSON
- 禁止 JSON 之外的任何文字
- title 格式："[角色] [完成目标]"，如"新用户完成首次下单全流程"
- steps 应包含完整的操作链路（5-15步），从进入页面到最终确认
- precondition 必须明确前置数据和账户状态

【E2E 场景设计原则】
1. 用户角色：为每个主要角色设计至少 1 条完整旅程
2. 关键路径优先：先覆盖核心业务流（注册→选品→下单→支付→收货）
3. 跨模块串联：用例应跨越 2 个以上功能模块
4. 数据贯穿：前序步骤的输出是后序步骤的输入
5. 异常中断与恢复：中途断网/超时/取消后重新操作
6. 多角色协作：如"申请人提交→审批人审核→申请人查看结果"

【约束】
- tags_suggested 仅能从以下标签中选取：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【生成原则】
1. 需求文本是唯一事实源
2. 每条用例必须是可从头到尾执行的独立场景，不依赖其他用例的中间结果
3. steps 中包含页面导航和数据验证，不只是点击操作
4. 在 summary 中列出识别到的角色和关键旅程图

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 8, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'e2e_user_journey');

-- ============================================================
-- 9. 异常容错测试 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'exception_resilience', '异常与容错测试', 'functional',
   '聚焦系统在异常输入、故障注入、资源耗尽等极端条件下的健壮性验证',
   '你是一名系统可靠性测试专家。请基于下方"需求文本"生成异常与容错测试用例草稿。

【任务目标】
- 设计系统在各类异常条件下的行为验证用例
- 验证系统不会崩溃、数据不会丢失/损坏、能给出合理的错误提示
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}

【输出要求】
- 严格输出 standard_case_v1 JSON
- 禁止 JSON 之外的任何文字
- title 格式："[故障类型] [触发条件] [期望行为]"
- steps 中 action 描述如何触发异常，expected 描述系统的容错反应

【异常测试分类】
1. 无效输入：
   - 空值、null、undefined、NaN
   - 超长字符串（10000+字符）
   - 特殊字符：emoji 😀、零宽字符、RTL 文字、SQL 特殊字符
   - 非法格式：邮箱无@、手机号含字母、日期格式错误
2. 网络异常：
   - 请求超时（30s+）
   - 网络中断后恢复
   - 重复提交（快速双击）
   - 并发操作同一资源
3. 资源异常：
   - 磁盘空间不足时上传文件
   - 数据库连接池耗尽
   - 第三方服务不可用
4. 数据异常：
   - 引用的关联数据被删除（如用例关联的模块被删除）
   - 数据库字段被篡改为非法值
   - 版本冲突（乐观锁失败）
5. 权限异常：
   - Token 过期后继续操作
   - 角色降级后访问原页面
   - 直接篡改 URL 中的 ID

【约束】
- tags_suggested 仅能从以下标签中选取：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【生成原则】
1. 需求文本是唯一事实源
2. 关注系统的"防御性"：错误提示是否友好、数据是否一致、能否优雅降级
3. 在 summary.uncovered_risks 中列出需求未覆盖的潜在故障点

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 9, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'exception_resilience');

-- ============================================================
-- 10. 数据驱动/组合测试 Skill（正交/Pairwise）
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'pairwise_combination', '组合/正交测试', 'functional',
   '对多参数输入采用 Pairwise 组合策略，用最少用例覆盖最多参数交互',
   '你是一名组合测试设计专家（精通 Pairwise / 正交表方法）。请基于下方"需求文本"生成组合测试用例草稿。

【任务目标】
- 识别需求中的多因子/多条件输入场景
- 使用 Pairwise（两两组合）策略生成最优用例集，确保任意两个参数的值组合至少出现一次
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}

【输出要求】
- 严格输出 standard_case_v1 JSON
- 禁止 JSON 之外的任何文字
- title 格式："[场景] + [关键参数组合摘要]"
- steps 中 action 必须明确列出每个参数的具体取值
- precondition 中列出参数因子表

【组合测试方法】
1. 识别参数因子：从需求中提取所有可变输入参数
2. 确定参数值域：每个参数的有效取值集合（含等价类代表值）
3. Pairwise 组合：保证任意两个参数的取值组合至少覆盖一次
4. 补充关键全组合：对最重要的 2-3 个参数做全组合覆盖
5. 加入无效组合：至少包含若干条参数冲突/互斥的异常组合

【示例】
若参数为：
- 用户类型：[普通用户, VIP用户, 管理员]
- 支付方式：[微信, 支付宝, 银行卡]
- 订单金额：[0.01, 100, 99999]
则 Pairwise 至少需要约 9 条用例覆盖所有两两组合

【约束】
- tags_suggested 仅能从以下标签中选取：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【生成原则】
1. 需求文本是唯一事实源
2. 在 summary 中输出识别到的参数因子表（参数名 + 取值列表）
3. 在 summary.uncovered_risks 中标注无法通过 Pairwise 覆盖的高阶交互风险

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 10, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'pairwise_combination');

-- ============================================================
-- 11. 权限/RBAC 测试 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'rbac_permission', '权限与角色测试', 'security',
   '面向 RBAC / 多租户 / 数据隔离场景，系统化验证权限矩阵的正确性',
   '你是一名权限测试专家。请基于下方"需求文本"生成权限与角色测试用例草稿。

【任务目标】
- 从需求中提取角色集合、功能权限点、数据范围规则
- 构建权限矩阵，针对矩阵中每个单元格（角色 × 权限点）生成验证用例
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}

【输出要求】
- 严格输出 standard_case_v1 JSON
- 禁止 JSON 之外的任何文字
- title 格式："[角色] [操作] [资源] [期望：允许/拒绝]"
- steps 中 action 指定以哪个角色登录 + 执行什么操作，expected 明确允许或拒绝 + HTTP 状态码

【权限测试框架】
1. 功能权限（垂直）：不同角色对同一功能的访问控制
   - admin 可以、tester 可以、readonly 不可以
2. 数据权限（水平）：同级角色之间的数据隔离
   - 用户 A 不可查看/修改用户 B 的数据
3. 多租户隔离：不同项目/组织之间的数据不可见
4. 权限边界：
   - 角色降级后，已打开页面的操作是否被阻止
   - Token 中角色与数据库角色不一致时的行为
   - API 直接调用绕过前端的情况
5. 特殊操作：
   - 创建者 vs 管理员的操作权限差异
   - 批量操作中混入无权限的资源

【约束】
- tags_suggested 仅能从以下标签中选取：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【生成原则】
1. 需求文本是唯一事实源
2. 在 summary 中输出识别到的权限矩阵（角色 × 功能点）
3. 每个"拒绝"用例必须验证：返回 403/401 + 数据确实未被修改
4. 在 summary.uncovered_risks 中标注权限规则模糊或缺失的地方

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 11, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'rbac_permission');

-- ============================================================
-- 12. 数据完整性/CRUD 测试 Skill
-- ============================================================
INSERT INTO `ai_skills`
  (`project_id`, `skill_key`, `name`, `scope`, `description`, `prompt_template`, `output_schema`,
   `is_system`, `is_active`, `sort_order`, `lock_version`, `created_by`, `created_at`, `updated_at`)
SELECT 0, 'data_integrity_crud', '数据完整性（CRUD）测试', 'functional',
   '系统化验证增删改查的数据一致性、级联操作、审计追踪',
   '你是一名数据质量测试专家。请基于下方"需求文本"生成数据完整性 CRUD 测试用例草稿。

【任务目标】
- 针对需求涉及的每个数据实体，验证 Create/Read/Update/Delete 全生命周期
- 确保数据一致性、级联完整性、审计可追溯
- 总数不超过 {{max_cases}} 条
- 用例默认级别 {{default_level}}

【输出要求】
- 严格输出 standard_case_v1 JSON
- 禁止 JSON 之外的任何文字
- title 格式："[实体] [CRUD操作] [验证重点]"
- steps 应包含操作 + 数据库层面的验证

【CRUD 测试矩阵】
1. Create（创建）：
   - 全部必填字段填写 → 创建成功 → 数据库记录正确
   - 唯一约束违反（重复 name / email）→ 创建失败
   - 创建后关联数据自动生成（如默认角色、审计日志）
2. Read（查询）：
   - 列表分页正确（总数、页码、每页条数）
   - 搜索/筛选结果准确
   - 详情接口返回完整字段
   - 软删除的数据不在列表中出现
3. Update（更新）：
   - 修改每个可编辑字段 → 数据库更新正确
   - 乐观锁冲突（并发编辑）→ 后者失败
   - 更新后关联数据同步（如修改用户名后，评论中的展示名也更新）
4. Delete（删除）：
   - 软删除 → 列表不可见但数据库存在
   - 级联删除 → 关联的子记录同步处理
   - 被引用的数据不允许删除（如被用例引用的模块）
5. 跨操作一致性：
   - Create → Read → Update → Read → Delete → Read 全链路

【约束】
- tags_suggested 仅能从以下标签中选取：{{existing_tags}}
- 用户额外指引：{{extra_prompt}}

【生成原则】
1. 需求文本是唯一事实源
2. 在 summary 中列出识别到的数据实体和关联关系
3. 关注数据的"最终一致性"——操作后查询是否立即反映
4. 在 summary.uncovered_risks 中标注级联规则不明确的地方

【需求文本】
{{requirement_text}}

{{few_shot_examples}}',
   'standard_case_v1',
   1, 1, 12, 0, 0, NOW(), NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `ai_skills` WHERE `project_id` = 0 AND `skill_key` = 'data_integrity_crud');
