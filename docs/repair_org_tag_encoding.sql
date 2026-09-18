-- 修复：初始化导入时因客户端连接字符集为 latin1 导致的中文乱码（种子 org_tag 数据）
-- 适用：MySQL 已初始化但 organization_tags 的 PRIVATE_admin 显示乱码的库
-- 用法（对已存在的数据库执行，如：mysql -h127.0.0.1 -P3307 -uroot -p ZebraRAG < docs/repair_org_tag_encoding.sql）
SET NAMES utf8mb4;

UPDATE organization_tags
SET name = 'admin个人知识库', description = '用户的个人组织标签，仅用户本人可访问'
WHERE tag_id = 'PRIVATE_admin';
