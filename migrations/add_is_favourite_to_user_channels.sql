-- Migration: Add per-user channel favourites
-- Stores whether a member has favourited a channel. One row per user per channel already exists
-- in user_channels, so the flag is private to each user. Additive and safe to re-run.

ALTER TABLE user_channels ADD COLUMN IF NOT EXISTS is_favourite BOOLEAN NOT NULL DEFAULT FALSE;
