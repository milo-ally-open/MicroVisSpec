-- Read-only expand/backfill audit for a migrated MicroVisSpec PostgreSQL DB.
-- Run with: psql "<database connection string>" -v ON_ERROR_STOP=1 -f project/scripts/audit_workspace_migration.sql
-- This reports counts only; it never repairs data or changes schema.
BEGIN TRANSACTION READ ONLY;

WITH audit AS (
    SELECT 'device.devices' AS relation, count(*) AS row_count,
           count(*) FILTER (WHERE workspace_id IS NULL) AS missing_workspace,
           count(*) FILTER (WHERE workspace_id IS DISTINCT FROM account_id) AS orphan_or_mismatch
      FROM device.devices
    UNION ALL
    SELECT 'device.cameras', count(*),
           count(*) FILTER (WHERE cameras.workspace_id IS NULL),
           count(*) FILTER (WHERE parent.id IS NULL OR cameras.workspace_id IS DISTINCT FROM parent.workspace_id)
      FROM device.cameras AS cameras
      LEFT JOIN device.devices AS parent ON parent.id = cameras.device_id
    UNION ALL
    SELECT 'device.algorithm_assignments', count(*),
           count(*) FILTER (WHERE children.workspace_id IS NULL),
           count(*) FILTER (WHERE parent.id IS NULL OR children.workspace_id IS DISTINCT FROM parent.workspace_id)
      FROM device.algorithm_assignments AS children
      LEFT JOIN device.devices AS parent ON parent.id = children.device_id
    UNION ALL
    SELECT 'device.activation_tickets', count(*),
           count(*) FILTER (WHERE children.workspace_id IS NULL),
           count(*) FILTER (WHERE parent.id IS NULL OR children.workspace_id IS DISTINCT FROM parent.workspace_id)
      FROM device.activation_tickets AS children
      LEFT JOIN device.devices AS parent ON parent.id = children.device_id
    UNION ALL
    SELECT 'device.command_records', count(*),
           count(*) FILTER (WHERE children.workspace_id IS NULL),
           count(*) FILTER (WHERE parent.id IS NULL OR children.workspace_id IS DISTINCT FROM parent.workspace_id)
      FROM device.command_records AS children
      LEFT JOIN device.devices AS parent ON parent.id = children.device_id
    UNION ALL
    SELECT 'device.config_snapshots', count(*),
           count(*) FILTER (WHERE children.workspace_id IS NULL),
           count(*) FILTER (WHERE parent.id IS NULL OR children.workspace_id IS DISTINCT FROM parent.workspace_id)
      FROM device.config_snapshots AS children
      LEFT JOIN device.devices AS parent ON parent.id = children.device_id
    UNION ALL
    SELECT 'device.provisioning_jobs', count(*),
           count(*) FILTER (WHERE children.workspace_id IS NULL),
           count(*) FILTER (WHERE parent.id IS NULL OR children.workspace_id IS DISTINCT FROM parent.workspace_id)
      FROM device.provisioning_jobs AS children
      LEFT JOIN device.devices AS parent ON parent.id = children.device_id
    UNION ALL
    SELECT 'device.broker_credentials', count(*),
           count(*) FILTER (WHERE children.workspace_id IS NULL),
           count(*) FILTER (WHERE parent.id IS NULL OR children.workspace_id IS DISTINCT FROM parent.workspace_id)
      FROM device.broker_credentials AS children
      LEFT JOIN device.devices AS parent ON parent.id = children.device_id
    UNION ALL
    SELECT 'device.outbox', count(*),
           count(*) FILTER (WHERE children.workspace_id IS NULL),
           count(*) FILTER (WHERE parent.id IS NULL OR children.workspace_id IS DISTINCT FROM parent.workspace_id)
      FROM device.outbox AS children
      LEFT JOIN device.devices AS parent ON parent.id::text = children.payload->>'device_id'
    UNION ALL
    SELECT 'media.streams', count(*),
           count(*) FILTER (WHERE workspace_id IS NULL),
           count(*) FILTER (WHERE workspace_id IS DISTINCT FROM account_id)
      FROM media.streams
    UNION ALL
    SELECT 'event.events', count(*),
           count(*) FILTER (WHERE workspace_id IS NULL),
           count(*) FILTER (WHERE workspace_id IS DISTINCT FROM account_id)
      FROM event.events
    UNION ALL
    SELECT 'event.outbox', count(*),
           count(*) FILTER (WHERE outboxes.workspace_id IS NULL),
           count(*) FILTER (WHERE events.id IS NULL OR outboxes.workspace_id IS DISTINCT FROM events.workspace_id)
      FROM event.outbox AS outboxes
      LEFT JOIN event.events AS events ON events.id = outboxes.id
    UNION ALL
    SELECT 'event.evidence', count(*),
           count(*) FILTER (WHERE evidence.workspace_id IS NULL),
           count(*) FILTER (WHERE events.id IS NULL OR evidence.workspace_id IS DISTINCT FROM events.workspace_id)
      FROM event.evidence AS evidence
      LEFT JOIN event.events AS events ON events.id = evidence.event_id
    UNION ALL
    SELECT 'event.dispositions', count(*),
           count(*) FILTER (WHERE dispositions.workspace_id IS NULL),
           count(*) FILTER (WHERE events.id IS NULL OR dispositions.workspace_id IS DISTINCT FROM events.workspace_id)
      FROM event.dispositions AS dispositions
      LEFT JOIN event.events AS events ON events.id = dispositions.event_id
)
SELECT relation, row_count, missing_workspace, orphan_or_mismatch
  FROM audit
 ORDER BY relation;

COMMIT;
