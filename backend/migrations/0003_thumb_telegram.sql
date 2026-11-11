-- 0003_thumb_telegram.sql: thumbnail Telegram refs.
-- Idempotent: older checkouts run Migrate twice (db_test).
ALTER TABLE movies ADD COLUMN thumb_telegram_file_id TEXT;
ALTER TABLE movies ADD COLUMN thumb_telegram_message_id INTEGER;
