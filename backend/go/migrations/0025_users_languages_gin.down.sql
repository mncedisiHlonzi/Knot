-- 0025_users_languages_gin.down.sql
--
-- Drops the GIN index on users.preferred_languages added by
-- 0025_users_languages_gin.up.sql (KNOT-036-fix3, KNOT-ADR-095).
--
-- Dropping it costs nothing but the query plan: the audience query stays correct,
-- it just returns to scanning `users` for the language rule.

DROP INDEX IF EXISTS users_preferred_languages_gin;
