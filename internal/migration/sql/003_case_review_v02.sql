-- 003_case_review_v02.sql
-- 用例评审 v0.2 Phase 1 数据回填迁移。
--
-- 说明：
--   所有新列和索引都通过 GORM AutoMigrate + model gorm tag 建立（MySQL 8 不支持
--   `ADD COLUMN IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS`，直接 ALTER 会
--   导致二次启动 "column already exists" 报错）。本脚本只保留 MySQL 兼容的
--   UPDATE 语句做数据回填，幂等安全。
--
-- 回填点：
--   1. case_reviews.moderator_id       —— 老数据默认把创建者当 Moderator。
--   2. case_review_item_reviewers.review_role —— 老数据统一标 primary。

-- 回填 Moderator：只在 moderator_id 未设置时生效，再跑也不会覆盖已显式指定的值
UPDATE case_reviews
   SET moderator_id = created_by
 WHERE moderator_id = 0
   AND created_by > 0;

-- 回填评审角色：历史数据视同 primary（Phase 1 之前所有评审人都按单/多人裁定）
UPDATE case_review_item_reviewers
   SET review_role = 'primary'
 WHERE review_role = ''
    OR review_role IS NULL;
