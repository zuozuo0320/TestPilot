-- 002_project_owner_backfill.sql
-- 将历史项目负责人收敛为 projects.owner_id，并清理多 owner 脏数据。
-- 依赖 GORM AutoMigrate 先创建 projects.owner_id 列。

UPDATE projects p
LEFT JOIN (
    SELECT pm.project_id, pm.user_id
    FROM project_members pm
    INNER JOIN (
        SELECT project_id, MIN(id) AS min_id
        FROM project_members
        WHERE role = 'owner'
        GROUP BY project_id
    ) picked ON picked.min_id = pm.id
) owner_pick ON owner_pick.project_id = p.id
LEFT JOIN (
    SELECT pm.project_id, pm.user_id
    FROM project_members pm
    INNER JOIN (
        SELECT project_id, MIN(id) AS min_id
        FROM project_members
        GROUP BY project_id
    ) picked ON picked.min_id = pm.id
) member_pick ON member_pick.project_id = p.id
SET p.owner_id = COALESCE(owner_pick.user_id, member_pick.user_id, p.owner_id)
WHERE p.owner_id = 0;

UPDATE project_members pm
INNER JOIN projects p ON p.id = pm.project_id
SET pm.role = 'member'
WHERE pm.role = 'owner' AND pm.user_id <> p.owner_id;

UPDATE project_members pm
INNER JOIN projects p ON p.id = pm.project_id
SET pm.role = 'owner'
WHERE pm.user_id = p.owner_id AND pm.role <> 'owner';
