-- 0006: store subtitle tracks on Telegram storage instead of local disk.
-- file_path kept for backward-compat (older rows); new uploads write to
-- Telegram and store telegram_file_id / telegram_message_id instead.
ALTER TABLE subtitles ADD COLUMN telegram_file_id TEXT;
ALTER TABLE subtitles ADD COLUMN telegram_message_id INTEGER;
CREATE INDEX IF NOT EXISTS idx_subtitles_telegram_file_id ON subtitles(telegram_file_id);
