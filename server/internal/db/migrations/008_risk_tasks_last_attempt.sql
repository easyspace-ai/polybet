-- Last FOK / abort snapshot for close_position tasks (JSON), for UI + postmortem.
ALTER TABLE risk_tasks ADD COLUMN last_attempt_detail TEXT;
