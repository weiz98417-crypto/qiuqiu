UPDATE fact_conflict_resolution_facts AS resolution
SET
  reason = CASE WHEN resolution.reason = '' THEN conflict.reason ELSE resolution.reason END,
  resolved_by = CASE WHEN resolution.resolved_by = '' THEN conflict.resolved_by ELSE resolution.resolved_by END
FROM fact_conflicts AS conflict
WHERE conflict.id = resolution.conflict_id
  AND (resolution.reason = '' OR resolution.resolved_by = '')
  AND (
    SELECT COUNT(*)
    FROM fact_conflict_resolution_facts AS sibling
    WHERE sibling.conflict_id = resolution.conflict_id
  ) = 1;
