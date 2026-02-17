-- Add PR metadata to backlog items
ALTER TABLE backlog_items ADD COLUMN IF NOT EXISTS pr_url TEXT;
ALTER TABLE backlog_items ADD COLUMN IF NOT EXISTS branch_name TEXT;
