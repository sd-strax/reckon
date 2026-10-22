-- 0023: predictions a person tests (01 §x-prediction, adr/0054).
-- test_kind: tool (a query runs it) | manual (a person's answer decides it —
-- the analyst records the result). attestation: the human observation the
-- outcome cites, mirrored from the STIX note so the tracker needs no second
-- lookup; NULL until an attested outcome lands.
ALTER TABLE prediction_current ADD COLUMN test_kind TEXT NOT NULL DEFAULT 'tool';
ALTER TABLE prediction_current ADD COLUMN attestation JSONB;
