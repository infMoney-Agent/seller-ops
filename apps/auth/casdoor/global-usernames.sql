CREATE UNIQUE INDEX IF NOT EXISTS seller_ops_user_name_unique ON user(lower(trim(name)));
CREATE TRIGGER IF NOT EXISTS seller_ops_user_name_insert BEFORE INSERT ON user WHEN EXISTS(SELECT 1 FROM user WHERE lower(trim(name))=lower(trim(NEW.name))) BEGIN SELECT RAISE(ABORT,'该账号已被使用，请更换账号'); END;
CREATE TRIGGER IF NOT EXISTS seller_ops_user_name_update BEFORE UPDATE OF name ON user WHEN EXISTS(SELECT 1 FROM user WHERE lower(trim(name))=lower(trim(NEW.name)) AND NOT(owner=OLD.owner AND name=OLD.name)) BEGIN SELECT RAISE(ABORT,'该账号已被使用，请更换账号'); END;
