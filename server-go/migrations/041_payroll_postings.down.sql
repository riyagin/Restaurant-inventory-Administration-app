-- Dropping the queue does not un-post anything: the journal entries it produced
-- stay, and are reversed through the journal if that is actually wanted.
DROP INDEX IF EXISTS payroll_postings_unposted_idx;
DROP TABLE IF EXISTS payroll_postings;
