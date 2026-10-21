-- 0020_entity_features.down.sql
--
-- Reverses 0020_entity_features.up.sql. The table holds only derived data, so
-- dropping it loses nothing that cannot be rebuilt.
--
-- The migration runner wraps this file in a transaction.

DROP TABLE IF EXISTS entity_features;
