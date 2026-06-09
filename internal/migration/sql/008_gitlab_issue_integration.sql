-- 008_gitlab_issue_integration.sql
-- GitLab Issue 需求源接入迁移。
--
-- 变更原因：
--   需求智生需要从 GitLab Issue 拉取需求文本、评论和来源链接，生成可追溯的需求文档。
--
-- 影响表：
--   1. project_integrations      —— 保存项目级 GitLab 地址、项目路径和加密 Token。
--   2. requirement_doc_sources   —— 保存 requirement_docs 与 GitLab Issue 的外部来源映射。
--
-- 回滚方案：
--   1. 下线前端 GitLab Issue 入口和后端相关路由。
--   2. 备份并执行 DROP TABLE requirement_doc_sources; DROP TABLE project_integrations;。
--   3. 已导入的 requirement_docs 会保留为普通 markdown 需求文档，不影响用例生成结果。
--
-- 数据量与锁表风险：
--   新增空表，无历史数据回填；首次执行只创建两张表和索引，锁表风险低。
--   现有生产 schema 仍由 AutoMigrate 先创建，本迁移用于记录可审计变更边界。

CREATE TABLE IF NOT EXISTS `project_integrations` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `project_id` bigint unsigned NOT NULL,
  `provider` varchar(30) NOT NULL,
  `base_url` varchar(500) NOT NULL DEFAULT '',
  `project_path` varchar(500) NOT NULL DEFAULT '',
  `encrypted_token` text,
  `token_mask` varchar(80) NOT NULL DEFAULT '',
  `enabled` boolean NOT NULL DEFAULT true,
  `created_by` bigint unsigned NOT NULL DEFAULT 0,
  `updated_by` bigint unsigned NOT NULL DEFAULT 0,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_project_provider` (`project_id`, `provider`),
  KEY `idx_pi_project` (`project_id`)
);

CREATE TABLE IF NOT EXISTS `requirement_doc_sources` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `project_id` bigint unsigned NOT NULL,
  `requirement_doc_id` bigint unsigned NOT NULL,
  `source_type` varchar(30) NOT NULL,
  `external_system` varchar(30) NOT NULL,
  `source_url` varchar(1000) NOT NULL DEFAULT '',
  `external_project_id` varchar(100) NOT NULL DEFAULT '',
  `external_project_path` varchar(500) NOT NULL DEFAULT '',
  `external_issue_iid` bigint NOT NULL DEFAULT 0,
  `external_key` varchar(128) NOT NULL,
  `version_no` bigint NOT NULL DEFAULT 1,
  `external_updated_at` datetime(3) DEFAULT NULL,
  `last_synced_at` datetime(3) DEFAULT NULL,
  `sync_status` varchar(20) NOT NULL DEFAULT 'synced',
  `sync_error` varchar(1000) NOT NULL DEFAULT '',
  `created_by` bigint unsigned NOT NULL DEFAULT 0,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_rds_external` (`project_id`, `source_type`, `external_key`, `version_no`),
  KEY `idx_rds_project` (`project_id`),
  KEY `idx_rds_doc` (`requirement_doc_id`)
);
