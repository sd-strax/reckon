-- 0031: the write binding chosen when an action was requested (adr/0406).
-- The engine picks the binding at request time and freezes it with the
-- targets' resolved identifiers; dispatch uses it and no other. The value is
-- in the event log (action.requested → ActionRequested.Binding); this
-- materializes it so the approval and dispatch paths read it without
-- walking events.
ALTER TABLE action_current ADD COLUMN binding_adapter TEXT NOT NULL DEFAULT '';
ALTER TABLE action_current ADD COLUMN binding_operation TEXT NOT NULL DEFAULT '';
