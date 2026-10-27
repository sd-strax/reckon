-- 0027: the Gate 2 evaluation folded onto the action (adr/0099; binding gap
-- 16). policy is the projection of action.policy_evaluated: the effect that
-- drove authorization, the matched policy, where the approval routes, and the
-- shadow policies that would have fired. NULL on an action requested before
-- the evaluation was folded, or when no evaluation was recorded.
ALTER TABLE action_current ADD COLUMN IF NOT EXISTS policy JSONB;
