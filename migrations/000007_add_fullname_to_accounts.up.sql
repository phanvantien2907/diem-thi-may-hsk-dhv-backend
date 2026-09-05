-- Migration 002: Add account_full_name to accounts table
-- This allows both candidates and staff to have a full name at the account level.

ALTER TABLE accounts 
    ADD COLUMN IF NOT EXISTS account_full_name VARCHAR(150) NOT NULL DEFAULT '';
