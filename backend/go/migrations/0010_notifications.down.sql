-- 0010_notifications.down.sql
--
-- Rollback for 0010: drop the notifications table.
--
-- Notifications are derived, disposable data — they are only ever an in-app
-- reminder that something happened — so unlike media objects there is nothing to
-- preserve here. Dropping the table is the complete rollback.

DROP TABLE IF EXISTS notifications;
