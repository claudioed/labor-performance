ALTER TABLE labor_standards ADD COLUMN travel_component_seconds BIGINT
    CHECK (travel_component_seconds IS NULL OR (travel_component_seconds >= 0 AND travel_component_seconds <= expected_seconds));
