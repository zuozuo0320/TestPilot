-- 需求智生模块增量 SQL 迁移
-- 版本：006 · 日期：2026-05-31
-- 目的：从需求智生模块移除"接口测试(api_testcase)"与"端到端业务流程测试(e2e_user_journey)"两个内置 Skill。
--   原因：需求智生模块是"上传需求文档 → 提取用例"的场景。
--     - api_testcase 的提示词以"接口描述/契约文档"为输入，与需求文档输入错配；
--     - e2e_user_journey 的覆盖可由其他 Skill 的用例组合满足，无需单列。
--   仅清理系统内置记录（project_id=0 且 is_system=1），不影响任何项目私有 Skill。
--   历史生成任务已保存 skill_snapshot 快照，删除内置记录不影响其展示。

-- 删除系统内置 api_testcase
DELETE FROM `ai_skills` WHERE `project_id` = 0 AND `is_system` = 1 AND `skill_key` = 'api_testcase';

-- 删除系统内置 e2e_user_journey
DELETE FROM `ai_skills` WHERE `project_id` = 0 AND `is_system` = 1 AND `skill_key` = 'e2e_user_journey';
