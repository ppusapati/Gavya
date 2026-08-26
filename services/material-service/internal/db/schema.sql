-- Material flow: where milk is, and what happened when it moved.
--
-- balance-service already reconciles a network of flows with uncertainty
-- weighting and says UNOBSERVABLE where the data cannot support an answer. What
-- it does not have is anything physical: a node is a string, and nothing says
-- whether it names a cooler that exists, a tanker with a registration, or a
-- typo. This is that layer.
--
-- The claim it makes is narrow and it is the one a plant manager already cares
-- about: milk left a cooler, milk arrived at a plant, and the difference is a
-- number somebody can be asked about.

-- ---------------------------------------------------------------------------
-- Nodes
-- ---------------------------------------------------------------------------

-- A place milk is held or passes through.
--
-- A tanker is a node, not a carrier attached to a movement. Milk sits in it
-- between loading and discharge, sometimes for hours, and a mass balance that
-- treats the journey as one hop cannot say whether the missing forty litres
-- never left the cooler or never left the tanker. Two movements — cooler to
-- tanker, tanker to plant — put the tanker's own holdup where it can be seen.
CREATE TABLE IF NOT EXISTS material_nodes (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,

    -- The society's own name for it: a route code, a registration plate, a
    -- chilling centre number. What a person says on the telephone.
    code        VARCHAR(64) NOT NULL,
    name        VARCHAR(160) NOT NULL,

    kind        VARCHAR(24) NOT NULL CHECK (kind IN (
                    'COLLECTION_CENTRE', 'BULK_COOLER', 'CHILLING_UNIT', 'TANKER', 'PLANT')),

    -- What it holds when full, in the unit it is measured in. Optional: a
    -- collection centre has no capacity worth recording, a tanker does.
    capacity_value BIGINT CHECK (capacity_value IS NULL OR capacity_value > 0),
    capacity_unit  VARCHAR(16) CHECK (capacity_unit IS NULL OR capacity_unit IN ('LITRES', 'KILOGRAMS')),
    CONSTRAINT material_nodes_capacity_is_all_or_nothing CHECK (
        (capacity_value IS NULL) = (capacity_unit IS NULL)),

    active      BOOLEAN NOT NULL DEFAULT TRUE,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,
    updated_by  VARCHAR(26) NOT NULL,
    deleted_at  TIMESTAMPTZ
);

-- One node per code. Two coolers both called BMC-04 is a reconciliation that
-- balances against the wrong one, and the wrong one is on somebody's route.
CREATE UNIQUE INDEX IF NOT EXISTS material_nodes_one_per_code
    ON material_nodes (tenant_id, code) WHERE deleted_at IS NULL;

-- The composite key movements reach through, so a movement cannot name another
-- tenant's cooler. A single-column reference would permit it: a foreign key is
-- checked by the system rather than by the querying role, so the row-level
-- policies do not stop one.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'material_nodes_tenant_id_key') THEN
        ALTER TABLE material_nodes ADD CONSTRAINT material_nodes_tenant_id_key UNIQUE (tenant_id, id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS material_nodes_by_kind
    ON material_nodes (tenant_id, kind) WHERE deleted_at IS NULL AND active;

-- ---------------------------------------------------------------------------
-- Movements
-- ---------------------------------------------------------------------------

-- One physical transfer, measured twice.
--
-- Two measurements of one event is the whole design. A dispatch is what the
-- sending end says left; a receipt is what the receiving end says arrived. They
-- disagree, always, and the size of the disagreement is the product.
--
-- The two ends may measure in different units — a cooler dips litres, a
-- weighbridge weighs kilograms — and the difference between them is then not a
-- loss but the density of milk, about three per cent. So the variance is
-- computed only when the two are comparable, and a movement that is not
-- comparable says so instead of reporting three per cent of a tanker as
-- missing.
CREATE TABLE IF NOT EXISTS material_movements (
    id           VARCHAR(26) PRIMARY KEY,
    tenant_id    VARCHAR(26) NOT NULL,

    from_node_id VARCHAR(26) NOT NULL,
    to_node_id   VARCHAR(26) NOT NULL,
    CONSTRAINT material_movements_go_somewhere CHECK (from_node_id <> to_node_id),

    -- The dispatch. Always present: a movement begins when milk leaves.
    dispatched_at    TIMESTAMPTZ NOT NULL,
    dispatched_value BIGINT NOT NULL CHECK (dispatched_value > 0),
    dispatched_unit  VARCHAR(16) NOT NULL CHECK (dispatched_unit IN ('LITRES', 'KILOGRAMS')),
    -- How it was measured. A dipstick, a flowmeter and a weighbridge have
    -- different uncertainties, and balance-service weights a reconciliation by
    -- them — so a movement that does not say how it was measured cannot be
    -- weighted and would be given somebody's guess.
    dispatch_method  VARCHAR(20) NOT NULL CHECK (dispatch_method IN (
                         'DIP', 'FLOWMETER', 'WEIGHBRIDGE', 'DECLARED')),
    dispatched_by    VARCHAR(26) NOT NULL,

    -- The receipt. Null until the milk arrives.
    received_at    TIMESTAMPTZ,
    received_value BIGINT CHECK (received_value IS NULL OR received_value >= 0),
    received_unit  VARCHAR(16) CHECK (received_unit IS NULL OR received_unit IN ('LITRES', 'KILOGRAMS')),
    receipt_method VARCHAR(20) CHECK (receipt_method IS NULL OR receipt_method IN (
                       'DIP', 'FLOWMETER', 'WEIGHBRIDGE', 'DECLARED')),
    received_by    VARCHAR(26),

    -- A receipt is all of its parts or none of them. Half a receipt is a
    -- movement that looks arrived and has no quantity, and every total it
    -- appears in is short by a tanker.
    CONSTRAINT material_movements_receipt_is_whole CHECK (
        (received_at IS NULL AND received_value IS NULL AND received_unit IS NULL
            AND receipt_method IS NULL AND received_by IS NULL) OR
        (received_at IS NOT NULL AND received_value IS NOT NULL AND received_unit IS NOT NULL
            AND receipt_method IS NOT NULL AND received_by IS NOT NULL)),

    -- Milk does not arrive before it leaves.
    CONSTRAINT material_movements_arrive_after_leaving CHECK (
        received_at IS NULL OR received_at >= dispatched_at),

    -- What stayed behind in the sending vessel: the film on a tanker's walls,
    -- the heel a cooler cannot pump out. Declared at dispatch.
    --
    -- Recorded separately rather than deducted from the dispatch, because they
    -- are different claims. "4960 left" and "5000 left, 40 stayed" balance the
    -- same and mean different things, and only the second can be checked
    -- against the vessel.
    holdup_value BIGINT CHECK (holdup_value IS NULL OR holdup_value >= 0),
    holdup_unit  VARCHAR(16) CHECK (holdup_unit IS NULL OR holdup_unit IN ('LITRES', 'KILOGRAMS')),
    CONSTRAINT material_movements_holdup_is_all_or_nothing CHECK (
        (holdup_value IS NULL) = (holdup_unit IS NULL)),

    -- The density used to compare two ends measured in different units, and
    -- where it came from. Null when both ends share a unit, because then
    -- nothing needs converting.
    density_numerator BIGINT CHECK (density_numerator IS NULL OR density_numerator > 0),
    density_scale     SMALLINT CHECK (density_scale IS NULL OR density_scale BETWEEN 0 AND 9),
    density_celsius   SMALLINT,
    density_source    VARCHAR(16) CHECK (density_source IS NULL OR density_source IN (
                          'LACTOMETER', 'ANALYSED', 'DECLARED')),
    CONSTRAINT material_movements_density_is_whole CHECK (
        (density_numerator IS NULL AND density_scale IS NULL AND density_source IS NULL) OR
        (density_numerator IS NOT NULL AND density_scale IS NOT NULL AND density_source IS NOT NULL)),

    -- What the comparison came to, stored rather than recomputed on read.
    --
    -- A density corrected next week must not silently change a variance that
    -- has already been reported to a transporter. If it should change, that is
    -- a correction and leaves its own record.
    variance_value BIGINT,
    variance_unit  VARCHAR(16) CHECK (variance_unit IS NULL OR variance_unit IN ('LITRES', 'KILOGRAMS')),
    CONSTRAINT material_movements_variance_is_all_or_nothing CHECK (
        (variance_value IS NULL) = (variance_unit IS NULL)),

    -- Why there is no variance, when there is none. An empty column and "the
    -- two ends were measured in different units and nobody supplied a density"
    -- look identical in a report, and only one of them is a thing somebody can
    -- go and fix.
    variance_unavailable_reason TEXT,
    CONSTRAINT material_movements_unexplained_variance CHECK (
        variance_value IS NOT NULL OR received_at IS NULL
            OR (variance_unavailable_reason IS NOT NULL AND variance_unavailable_reason <> '')),

    status VARCHAR(16) NOT NULL CHECK (status IN ('IN_TRANSIT', 'RECEIVED', 'ABANDONED')),
    CONSTRAINT material_movements_received_means_received CHECK (
        (status = 'RECEIVED') = (received_at IS NOT NULL)),

    -- Why a movement was abandoned. A tanker that never arrived is an event
    -- worth a sentence, and a status alone does not carry one.
    abandoned_reason TEXT,
    CONSTRAINT material_movements_abandonment_has_a_reason CHECK (
        status <> 'ABANDONED' OR (abandoned_reason IS NOT NULL AND abandoned_reason <> '')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,

    FOREIGN KEY (tenant_id, from_node_id) REFERENCES material_nodes (tenant_id, id),
    FOREIGN KEY (tenant_id, to_node_id) REFERENCES material_nodes (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS material_movements_in_transit
    ON material_movements (tenant_id, to_node_id, dispatched_at)
    WHERE deleted_at IS NULL AND status = 'IN_TRANSIT';
CREATE INDEX IF NOT EXISTS material_movements_by_window
    ON material_movements (tenant_id, dispatched_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS material_movements_by_node
    ON material_movements (tenant_id, from_node_id, dispatched_at DESC) WHERE deleted_at IS NULL;

-- A tanker is in one place at a time.
--
-- The constraint that catches the commonest data-entry error on this table: a
-- load recorded against a tanker that is already out on a route, because the
-- clerk picked the wrong registration from a list. Without it the milk balances
-- against a vessel that was fifty kilometres away, and the route it actually
-- came from is short.
--
-- Only tankers. A plant receives from many coolers at once and a cooler sends
-- to several tankers in a morning; neither is a vessel that can only be in one
-- place. Enforced as a trigger rather than an exclusion constraint because the
-- rule depends on the node's kind, which lives in another table.
CREATE OR REPLACE FUNCTION gavya_tanker_is_in_one_place()
RETURNS TRIGGER AS $fn$
DECLARE
    v_kind text;
    v_other varchar(26);
BEGIN
    IF NEW.status <> 'IN_TRANSIT' THEN
        RETURN NEW;
    END IF;
    SELECT kind INTO v_kind FROM material_nodes
        WHERE tenant_id = NEW.tenant_id AND id = NEW.to_node_id;
    IF v_kind IS DISTINCT FROM 'TANKER' THEN
        RETURN NEW;
    END IF;
    SELECT id INTO v_other FROM material_movements
        WHERE tenant_id = NEW.tenant_id AND to_node_id = NEW.to_node_id
          AND status = 'IN_TRANSIT' AND deleted_at IS NULL AND id <> NEW.id
        LIMIT 1;
    IF v_other IS NOT NULL THEN
        RAISE EXCEPTION 'tanker % is already carrying movement %, so it cannot also be '
                        'receiving movement %; one of the two names the wrong vehicle',
            NEW.to_node_id, v_other, NEW.id
            USING ERRCODE = '23505';
    END IF;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS material_movements_tanker_in_one_place ON material_movements;
CREATE TRIGGER material_movements_tanker_in_one_place
    BEFORE INSERT OR UPDATE ON material_movements
    FOR EACH ROW EXECUTE FUNCTION gavya_tanker_is_in_one_place();

-- A movement that has been received does not change afterwards.
--
-- The same rule the producer payables carry, for the same reason: the variance
-- on a received movement is a figure somebody has been shown, often a
-- transporter being asked about forty litres. A quantity edited afterwards is
-- an argument nobody can reconstruct. A movement received in error is corrected
-- by a movement, not by an edit.
--
-- A trigger rather than a status check, so it holds for whatever reaches the
-- table: the service, a migration, a repair somebody ran at eleven at night.
CREATE OR REPLACE FUNCTION gavya_movement_is_final_once_received()
RETURNS TRIGGER AS $fn$
BEGIN
    IF OLD.received_at IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.received_at IS DISTINCT FROM OLD.received_at
       OR NEW.received_value IS DISTINCT FROM OLD.received_value
       OR NEW.received_unit IS DISTINCT FROM OLD.received_unit
       OR NEW.dispatched_value IS DISTINCT FROM OLD.dispatched_value
       OR NEW.dispatched_unit IS DISTINCT FROM OLD.dispatched_unit
       OR NEW.variance_value IS DISTINCT FROM OLD.variance_value
       OR NEW.from_node_id IS DISTINCT FROM OLD.from_node_id
       OR NEW.to_node_id IS DISTINCT FROM OLD.to_node_id THEN
        RAISE EXCEPTION 'movement % was received on % and cannot be changed; a receipt that '
                        'turned out to be wrong is corrected by a further movement, not by '
                        'editing the record of the one that happened',
            OLD.id, OLD.received_at
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS material_movements_final_once_received ON material_movements;
CREATE TRIGGER material_movements_final_once_received
    BEFORE UPDATE ON material_movements
    FOR EACH ROW EXECUTE FUNCTION gavya_movement_is_final_once_received();

-- ---------------------------------------------------------------------------
-- Instruments
-- ---------------------------------------------------------------------------

-- What measures at a node, and how well it is known.
--
-- balance-service weights a reconciliation by each flow's standard uncertainty:
-- a stream measured by a verified instrument pulls the solution harder than an
-- estimated one. Until now nothing could supply that figure, because it is not a
-- property of the method — a dipstick is not a number — it is a property of the
-- particular instrument at the particular node, and it comes off a calibration
-- certificate.
--
-- So there is no table of typical uncertainties by method here, and there will
-- not be one. A weighbridge is around a tenth of a per cent and a society's
-- weighbridge is whatever its certificate says. Inventing the first would put a
-- number nobody measured into the weighting of a settlement.
CREATE TABLE IF NOT EXISTS material_instruments (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,

    node_id     VARCHAR(26) NOT NULL,
    -- The method this instrument is the instrument for. A node that dips and
    -- also has a flowmeter has two rows, because they are two instruments and
    -- they are known to different precisions.
    method      VARCHAR(20) NOT NULL CHECK (method IN ('DIP', 'FLOWMETER', 'WEIGHBRIDGE', 'DECLARED')),

    -- The society's own name for it, so a certificate can be matched to a row.
    label       VARCHAR(120) NOT NULL,

    -- The standard uncertainty, one way or the other and never both.
    --
    -- An instrument's specification is written one of two ways and the
    -- difference matters across the range: a weighbridge is a fixed number of
    -- kilograms whatever the load, a flowmeter is a percentage of the reading.
    -- Storing only one shape would make every instrument of the other kind wrong
    -- at one end of its range.
    relative_ppm  BIGINT CHECK (relative_ppm IS NULL OR relative_ppm > 0),
    absolute_value BIGINT CHECK (absolute_value IS NULL OR absolute_value > 0),
    absolute_unit  VARCHAR(16) CHECK (absolute_unit IS NULL OR absolute_unit IN ('LITRES', 'KILOGRAMS')),
    CONSTRAINT material_instruments_absolute_is_whole CHECK (
        (absolute_value IS NULL) = (absolute_unit IS NULL)),
    CONSTRAINT material_instruments_one_kind_of_uncertainty CHECK (
        (relative_ppm IS NOT NULL AND absolute_value IS NULL) OR
        (relative_ppm IS NULL AND absolute_value IS NOT NULL)),

    -- Where the figure came from. An uncertainty with no certificate behind it
    -- is a number somebody remembered, and this one weights money.
    certificate_ref VARCHAR(120) NOT NULL,
    calibrated_on   DATE NOT NULL,
    -- When the calibration stops being current.
    --
    -- Not optional. Every calibration expires, and one recorded with no expiry
    -- is one that never does — which is how an instrument nobody has checked in
    -- three years goes on pulling a settlement towards its own reading.
    valid_until     DATE NOT NULL,
    CONSTRAINT material_instruments_calibration_is_forwards CHECK (valid_until > calibrated_on),

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,
    updated_by  VARCHAR(26) NOT NULL,
    deleted_at  TIMESTAMPTZ,

    FOREIGN KEY (tenant_id, node_id) REFERENCES material_nodes (tenant_id, id)
);

-- One instrument per node per method at a time.
--
-- Two rows would mean the platform holds two uncertainties for the same
-- measurement and picks one, and which it picked would be invisible in the
-- reconciliation that used it.
CREATE UNIQUE INDEX IF NOT EXISTS material_instruments_one_per_node_method
    ON material_instruments (tenant_id, node_id, method) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS material_instruments_by_expiry
    ON material_instruments (tenant_id, valid_until) WHERE deleted_at IS NULL;
