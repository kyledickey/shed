-- The 1Password Environment a service reads variables from; '' = none.
ALTER TABLE services ADD COLUMN onepassword_environment TEXT NOT NULL DEFAULT '';
