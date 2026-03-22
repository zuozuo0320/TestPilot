-- 001_longtext_fields.sql
-- 将富文本相关字段从 TEXT 升级为 LONGTEXT，支持存储 base64 图片等大文本内容。
-- 本脚本幂等安全，可重复执行。

-- test_cases 表
ALTER TABLE test_cases MODIFY COLUMN precondition LONGTEXT;
ALTER TABLE test_cases MODIFY COLUMN steps LONGTEXT;
ALTER TABLE test_cases MODIFY COLUMN remark LONGTEXT;

-- case_histories 表
ALTER TABLE case_histories MODIFY COLUMN old_value LONGTEXT;
ALTER TABLE case_histories MODIFY COLUMN new_value LONGTEXT;
