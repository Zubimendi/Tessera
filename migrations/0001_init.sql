CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE trip_status AS ENUM (
    'REQUESTED',
    'SEARCHING',
    'OFFERED',
    'ACCEPTED',
    'ARRIVED',
    'IN_PROGRESS',
    'COMPLETED',
    'CANCELLED',
    'NO_DRIVERS'
);

CREATE TYPE offer_status AS ENUM (
    'PENDING',
    'ACCEPTED',
    'DECLINED',
    'EXPIRED'
);

CREATE TYPE driver_status AS ENUM (
    'OFFLINE',
    'ONLINE',
    'AVAILABLE',
    'OFFERED'
);

CREATE TABLE IF NOT EXISTS allowed_transitions (
    from_status trip_status NOT NULL,
    to_status trip_status NOT NULL,
    PRIMARY KEY (from_status, to_status)
);

INSERT INTO allowed_transitions (from_status, to_status) VALUES
    ('REQUESTED', 'SEARCHING'),
    ('SEARCHING', 'OFFERED'),
    ('OFFERED', 'SEARCHING'),
    ('OFFERED', 'ACCEPTED'),
    ('SEARCHING', 'NO_DRIVERS'),
    ('ACCEPTED', 'ARRIVED'),
    ('ARRIVED', 'IN_PROGRESS'),
    ('IN_PROGRESS', 'COMPLETED'),
    ('REQUESTED', 'CANCELLED'),
    ('SEARCHING', 'CANCELLED'),
    ('OFFERED', 'CANCELLED'),
    ('ACCEPTED', 'CANCELLED'),
    ('ARRIVED', 'CANCELLED')
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS trips (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rider_id UUID NOT NULL,
    driver_id UUID,
    quote_id UUID NOT NULL,
    pickup_point geography(Point,4326) NOT NULL,
    dropoff_point geography(Point,4326) NOT NULL,
    status trip_status NOT NULL DEFAULT 'REQUESTED',
    version INT NOT NULL DEFAULT 1,
    fare_minor BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS trip_events (
    id BIGSERIAL PRIMARY KEY,
    trip_id UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    from_status trip_status NOT NULL,
    to_status trip_status NOT NULL,
    actor TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS offers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trip_id UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    driver_id UUID NOT NULL,
    status offer_status NOT NULL DEFAULT 'PENDING',
    fare_minor BIGINT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS quotes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rider_id UUID NOT NULL,
    pickup_point geography(Point,4326) NOT NULL,
    dropoff_point geography(Point,4326) NOT NULL,
    base_fare_minor BIGINT NOT NULL,
    multiplier_bps INT NOT NULL,
    total_fare_minor BIGINT NOT NULL,
    rule_set_version TEXT NOT NULL,
    applied_rules JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS drivers (
    id UUID PRIMARY KEY,
    status driver_status NOT NULL DEFAULT 'OFFLINE',
    last_lat DOUBLE PRECISION,
    last_lng DOUBLE PRECISION,
    last_ping_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS pricing_rule_sets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version TEXT NOT NULL UNIQUE,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_offers_driver_active
    ON offers (driver_id)
    WHERE status = 'PENDING';

CREATE UNIQUE INDEX IF NOT EXISTS ux_trips_driver_active
    ON trips (driver_id)
    WHERE status IN ('ACCEPTED', 'ARRIVED', 'IN_PROGRESS');

CREATE INDEX IF NOT EXISTS idx_trips_status ON trips (status);
CREATE INDEX IF NOT EXISTS idx_offers_trip_id ON offers (trip_id);
CREATE INDEX IF NOT EXISTS idx_trip_events_trip_id ON trip_events (trip_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_drivers_status ON drivers (status);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trips_updated_at
BEFORE UPDATE ON trips
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER offers_updated_at
BEFORE UPDATE ON offers
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER drivers_updated_at
BEFORE UPDATE ON drivers
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE OR REPLACE FUNCTION validate_trip_transition()
RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM allowed_transitions
        WHERE from_status = OLD.status
          AND to_status = NEW.status
    ) THEN
        RAISE EXCEPTION 'illegal trip transition: % -> %', OLD.status, NEW.status;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trips_transition_validation
BEFORE UPDATE ON trips
FOR EACH ROW
WHEN (OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION validate_trip_transition();

CREATE OR REPLACE FUNCTION enforce_trip_event_immutability()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'trip_events is append-only and cannot be updated or deleted';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trip_events_prevent_update
BEFORE UPDATE OR DELETE ON trip_events
FOR EACH ROW
EXECUTE FUNCTION enforce_trip_event_immutability();

CREATE OR REPLACE FUNCTION append_trip_event()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO trip_events (trip_id, from_status, to_status, actor, payload)
    VALUES (NEW.id, OLD.status, NEW.status, 'system', jsonb_build_object('version', NEW.version));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trips_append_event
AFTER UPDATE ON trips
FOR EACH ROW
WHEN (OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION append_trip_event();
