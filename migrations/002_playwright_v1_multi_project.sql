-- ============================================================
-- 002_playwright_v1_multi_project.sql
-- Playwright V1 多项目工程化架构 — 数据库迁移
-- 
-- 变更摘要：
--   1. 新建 ai_script_files 表（生成文件明细）
--   2. 新建 ai_script_workspace_locks 表（项目级工作区锁）
--   3. ai_script_tasks 增加 project_key 字段
--   4. ai_script_versions 增加 V1 多项目工程化字段
--
-- 向后兼容：所有新增字段使用 DEFAULT NULL，不破坏现有数据。
-- ============================================================

-- 1. 生成文件明细表
CREATE TABLE IF NOT EXISTS ai_script_files (
    id                     BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    project_id             BIGINT UNSIGNED NOT NULL,
    task_id                BIGINT UNSIGNED NOT NULL,
    version_id             BIGINT UNSIGNED NOT NULL,
    file_type              VARCHAR(32)     NOT NULL COMMENT 'spec / page / shared / fixture / registry',
    relative_path          VARCHAR(512)    NOT NULL COMMENT '相对项目根的路径',
    content                LONGTEXT                 COMMENT '文件内容',
    content_hash           VARCHAR(64)              DEFAULT NULL COMMENT 'SHA-256 哈希',
    source_kind            VARCHAR(32)              DEFAULT NULL COMMENT 'create / update / generated / rebuilt',
    manual_review_required TINYINT(1)               DEFAULT 0,
    created_at             DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at             DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    INDEX idx_script_file_project (project_id),
    INDEX idx_script_file_task    (task_id),
    INDEX idx_script_file_version (version_id),
    UNIQUE INDEX uk_version_path  (version_id, relative_path)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='测试智编-生成文件明细表';


-- 2. 项目级工作区锁表
CREATE TABLE IF NOT EXISTS ai_script_workspace_locks (
    id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    project_id       BIGINT UNSIGNED NOT NULL,
    lock_key         VARCHAR(128)    NOT NULL COMMENT '锁标识',
    lock_type        VARCHAR(32)     NOT NULL COMMENT 'workspace_write / validate_run',
    owner_task_id    BIGINT UNSIGNED          DEFAULT NULL,
    owner_version_id BIGINT UNSIGNED          DEFAULT NULL,
    owner_request_id VARCHAR(64)              DEFAULT NULL,
    heartbeat_at     DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    expires_at       DATETIME(3)     NOT NULL,
    status           VARCHAR(32)     NOT NULL DEFAULT 'active' COMMENT 'active / released / expired',
    created_at       DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

    UNIQUE INDEX uk_lock_project (project_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='测试智编-项目级工作区锁表';


-- 3. ai_script_tasks 增加 project_key 字段
ALTER TABLE ai_script_tasks
    ADD COLUMN project_key VARCHAR(64) DEFAULT NULL COMMENT '项目标识键（如 foradar）'
    AFTER project_id;


-- 4. ai_script_versions 增加 V1 多项目工程化字段
ALTER TABLE ai_script_versions
    ADD COLUMN project_key_snapshot    VARCHAR(64)  DEFAULT NULL COMMENT '生成时的项目标识快照',
    ADD COLUMN version_status          VARCHAR(32)  DEFAULT NULL COMMENT 'V1 版本状态',
    ADD COLUMN generation_summary      TEXT         DEFAULT NULL COMMENT 'AI 生成摘要',
    ADD COLUMN manual_review_status    VARCHAR(32)  DEFAULT NULL COMMENT '人工审核状态',
    ADD COLUMN registry_snapshot_json  JSON         DEFAULT NULL COMMENT '生成时 registry 快照',
    ADD COLUMN workspace_root_snapshot VARCHAR(256) DEFAULT NULL COMMENT '生成时工作区根路径',
    ADD COLUMN base_fixture_hash       VARCHAR(64)  DEFAULT NULL COMMENT '生成时 base.fixture 哈希';
