ALTER TABLE agent_user_facts
    ADD COLUMN confidence DOUBLE PRECISION NOT NULL DEFAULT 1.0;

ALTER TABLE agent_user_facts
    ADD CONSTRAINT agent_user_facts_confidence_range
        CHECK (confidence >= 0.0 AND confidence <= 1.0);
