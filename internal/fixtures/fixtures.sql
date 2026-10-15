-- Development fixtures. Shared by the pgx and gorm versions (same schema).
--
-- * Fixed UUIDs, so you can curl them directly (see README).
-- * Timestamps are relative to now(), so the default ordering of
--   GET /workspaces is meaningful right after seeding.
-- * Idempotent: rows that already exist are skipped (use `make db-reseed`
--   to start from scratch with fresh timestamps).
--
-- 11 live workspaces + 1 soft-deleted one ("old-vpn"), 20 live actions,
-- 14 live variables. Expected results are asserted in fixtures_integration_test.go.

BEGIN;

-- ------------------------------------------------------------------ workspaces
INSERT INTO workspaces AS w
    (id, name, vcs_url, vcs_key_str, vcs_branch, vcs_directory, status, env_vars, namespace, tofu_version, created_at, updated_at, deleted_at)
VALUES
    ('a0000000-0000-4000-8000-000000000001', 'platform-network',
     'git@github.com:acme/infra-platform.git', 'deploy-keys/platform', 'main', 'network', 'ACTIVE',
     '[{"name":"TF_LOG","value":"INFO","secure":false,"hidden":false},
       {"name":"AWS_REGION","value":"eu-west-3","secure":false,"hidden":false},
       {"name":"AWS_ROLE_ARN","value":"arn:aws:iam::123456789012:role/tofu-platform","secure":false,"hidden":false}]',
     'platform', '1.8.5', now() - interval '90 days', now() - interval '10 days', NULL),

    ('a0000000-0000-4000-8000-000000000002', 'platform-dns',
     'git@github.com:acme/infra-platform.git', 'deploy-keys/platform', 'main', 'dns', 'ACTIVE',
     '[{"name":"AWS_REGION","value":"eu-west-3","secure":false,"hidden":false},
       {"name":"CLOUDFLARE_API_TOKEN","value":"change-me","secure":true,"hidden":true}]',
     'platform', '1.8.5', now() - interval '88 days', now() - interval '20 days', NULL),

    ('a0000000-0000-4000-8000-000000000003', 'platform-k8s-cluster',
     'git@github.com:acme/infra-platform.git', 'deploy-keys/platform', 'main', 'kubernetes/eks', 'FAILURE',
     '[{"name":"AWS_REGION","value":"eu-west-3","secure":false,"hidden":false},
       {"name":"TF_VAR_cluster_admin_role","value":"arn:aws:iam::123456789012:role/eks-admin","secure":false,"hidden":false},
       {"name":"TF_LOG","value":"DEBUG","secure":false,"hidden":false}]',
     'platform', '1.7.3', now() - interval '75 days', now() - interval '10 minutes', NULL),

    ('a0000000-0000-4000-8000-000000000004', 'payments-api',
     'https://github.com/acme/payments-infra.git', '', 'main', 'services/api', 'ACTIVE',
     '[{"name":"AWS_REGION","value":"eu-west-1","secure":false,"hidden":false},
       {"name":"TF_VAR_datadog_api_key","value":"change-me","secure":true,"hidden":true}]',
     'team-payments', '1.8.5', now() - interval '60 days', now() - interval '5 days', NULL),

    ('a0000000-0000-4000-8000-000000000005', 'payments-db',
     'https://github.com/acme/payments-infra.git', '', 'main', 'databases/postgres', 'ACTIVE',
     '[{"name":"AWS_REGION","value":"eu-west-1","secure":false,"hidden":false},
       {"name":"TF_VAR_db_master_password","value":"change-me","secure":true,"hidden":true}]',
     'team-payments', '1.8.5', now() - interval '45 days', now() - interval '7 days', NULL),

    ('a0000000-0000-4000-8000-000000000006', 'payments-sandbox',
     'https://github.com/acme/payments-infra.git', '', 'feature/sandbox', 'sandbox', 'INACTIVE',
     '[]',
     'team-payments', '1.6.2', now() - interval '30 days', now() - interval '10 days', NULL),

    ('a0000000-0000-4000-8000-000000000007', 'data-warehouse',
     'git@gitlab.com:acme/data/terraform.git', 'deploy-keys/data', 'main', 'warehouse', 'ACTIVE',
     '[{"name":"GOOGLE_PROJECT","value":"acme-data-prod","secure":false,"hidden":false},
       {"name":"GOOGLE_CREDENTIALS","value":"change-me","secure":true,"hidden":true}]',
     'team-data', '1.8.5', now() - interval '40 days', now() - interval '15 days', NULL),

    ('a0000000-0000-4000-8000-000000000008', 'data-airflow',
     'git@gitlab.com:acme/data/terraform.git', 'deploy-keys/data', 'develop', 'airflow', 'ACTIVE',
     '[{"name":"GOOGLE_PROJECT","value":"acme-data-staging","secure":false,"hidden":false}]',
     'team-data', '1.8.5', now() - interval '12 days', now() - interval '12 days', NULL),

    ('a0000000-0000-4000-8000-000000000009', 'data-lake-new',
     'git@gitlab.com:acme/data/terraform.git', 'deploy-keys/data', 'main', 'lake', 'ACTIVE',
     '[]',
     'team-data', '1.8.5', now() - interval '1 hour', now() - interval '1 hour', NULL),

    ('a0000000-0000-4000-8000-000000000010', 'sandbox-alice',
     'https://github.com/alice/tofu-playground.git', '', 'main', '', 'INACTIVE',
     '[{"name":"TF_LOG","value":"TRACE","secure":false,"hidden":false}]',
     'sandbox', '1.8.5', now() - interval '25 days', now() - interval '6 days', NULL),

    ('a0000000-0000-4000-8000-000000000011', 'legacy-monitoring',
     'https://github.com/acme/legacy-ops.git', '', 'master', 'monitoring', 'DELETED',
     '[]',
     'ops', '1.6.2', now() - interval '400 days', now() - interval '45 days', NULL),

    -- Soft-deleted: must never appear in the API, even though it has the most
    -- recent action of all.
    ('a0000000-0000-4000-8000-000000000012', 'old-vpn',
     'https://github.com/acme/legacy-ops.git', '', 'master', 'vpn', 'ACTIVE',
     '[]',
     'ops', '1.6.2', now() - interval '500 days', now() - interval '30 seconds', now() - interval '30 seconds')
ON CONFLICT (id) DO NOTHING;

-- --------------------------------------------------------------------- actions
INSERT INTO actions (id, workspace_id, type, status, created_at, updated_at, deleted_at)
VALUES
    -- platform-network: last activity 1h45 ago
    ('b0000000-0000-4000-8000-000000000101', 'a0000000-0000-4000-8000-000000000001', 'init',    'succeeded', now() - interval '30 days',             now() - interval '30 days',             NULL),
    ('b0000000-0000-4000-8000-000000000102', 'a0000000-0000-4000-8000-000000000001', 'plan',    'succeeded', now() - interval '2 hours',             now() - interval '1 hour 55 minutes',   NULL),
    ('b0000000-0000-4000-8000-000000000103', 'a0000000-0000-4000-8000-000000000001', 'apply',   'succeeded', now() - interval '1 hour 50 minutes',   now() - interval '1 hour 45 minutes',   NULL),
    -- platform-dns: last live activity 3 days ago; its recent destroy was deleted
    ('b0000000-0000-4000-8000-000000000201', 'a0000000-0000-4000-8000-000000000002', 'plan',    'succeeded', now() - interval '3 days 2 hours',      now() - interval '3 days 1 hour',       NULL),
    ('b0000000-0000-4000-8000-000000000202', 'a0000000-0000-4000-8000-000000000002', 'apply',   'succeeded', now() - interval '3 days 1 hour',       now() - interval '3 days',              NULL),
    ('b0000000-0000-4000-8000-000000000203', 'a0000000-0000-4000-8000-000000000002', 'destroy', 'queued',    now() - interval '2 minutes',           now() - interval '1 minute',            now() - interval '1 minute'),
    -- platform-k8s-cluster: apply failed 10 minutes ago
    ('b0000000-0000-4000-8000-000000000301', 'a0000000-0000-4000-8000-000000000003', 'plan',    'succeeded', now() - interval '25 minutes',          now() - interval '20 minutes',          NULL),
    ('b0000000-0000-4000-8000-000000000302', 'a0000000-0000-4000-8000-000000000003', 'apply',   'failed',    now() - interval '15 minutes',          now() - interval '10 minutes',          NULL),
    -- payments-api: last activity 23h ago
    ('b0000000-0000-4000-8000-000000000401', 'a0000000-0000-4000-8000-000000000004', 'init',    'succeeded', now() - interval '60 days',             now() - interval '60 days',             NULL),
    ('b0000000-0000-4000-8000-000000000402', 'a0000000-0000-4000-8000-000000000004', 'plan',    'succeeded', now() - interval '1 day 1 hour',        now() - interval '1 day',               NULL),
    ('b0000000-0000-4000-8000-000000000403', 'a0000000-0000-4000-8000-000000000004', 'apply',   'succeeded', now() - interval '1 day',               now() - interval '23 hours',            NULL),
    -- payments-db: plan running right now (most recent live activity)
    ('b0000000-0000-4000-8000-000000000501', 'a0000000-0000-4000-8000-000000000005', 'init',    'succeeded', now() - interval '7 days',              now() - interval '7 days',              NULL),
    ('b0000000-0000-4000-8000-000000000502', 'a0000000-0000-4000-8000-000000000005', 'plan',    'running',   now() - interval '6 minutes',           now() - interval '5 minutes',           NULL),
    -- payments-sandbox: no action
    -- data-warehouse: last activity 2 days ago
    ('b0000000-0000-4000-8000-000000000701', 'a0000000-0000-4000-8000-000000000007', 'init',    'succeeded', now() - interval '40 days',             now() - interval '40 days',             NULL),
    ('b0000000-0000-4000-8000-000000000702', 'a0000000-0000-4000-8000-000000000007', 'plan',    'succeeded', now() - interval '2 days 2 hours',      now() - interval '2 days 1 hour',       NULL),
    ('b0000000-0000-4000-8000-000000000703', 'a0000000-0000-4000-8000-000000000007', 'apply',   'succeeded', now() - interval '2 days 1 hour',       now() - interval '2 days',              NULL),
    -- data-airflow: plan waiting in the queue
    ('b0000000-0000-4000-8000-000000000801', 'a0000000-0000-4000-8000-000000000008', 'plan',    'queued',    now() - interval '30 minutes',          now() - interval '30 minutes',          NULL),
    -- data-lake-new: no action
    -- sandbox-alice: destroyed 6 days ago
    ('b0000000-0000-4000-8000-000000001001', 'a0000000-0000-4000-8000-000000000010', 'apply',   'succeeded', now() - interval '20 days',             now() - interval '20 days',             NULL),
    ('b0000000-0000-4000-8000-000000001002', 'a0000000-0000-4000-8000-000000000010', 'destroy', 'succeeded', now() - interval '6 days 1 hour',       now() - interval '6 days',              NULL),
    -- legacy-monitoring: destroyed 45 days ago
    ('b0000000-0000-4000-8000-000000001101', 'a0000000-0000-4000-8000-000000000011', 'plan',    'succeeded', now() - interval '46 days',             now() - interval '46 days',             NULL),
    ('b0000000-0000-4000-8000-000000001102', 'a0000000-0000-4000-8000-000000000011', 'destroy', 'succeeded', now() - interval '45 days 1 hour',      now() - interval '45 days',             NULL),
    -- old-vpn (soft-deleted workspace): actions deleted with it
    ('b0000000-0000-4000-8000-000000001201', 'a0000000-0000-4000-8000-000000000012', 'plan',    'succeeded', now() - interval '2 minutes',           now() - interval '1 minute',            now() - interval '30 seconds')
ON CONFLICT (id) DO NOTHING;

-- ------------------------------------------------------------------- variables
-- Variable names are globally unique, hence the per-workspace prefixes.
INSERT INTO variables (workspace_id, name, status, created_at, updated_at, deleted_at)
SELECT v.workspace_id::uuid, v.name, v.status, v.created_at, v.updated_at, v.deleted_at
FROM (VALUES
    ('a0000000-0000-4000-8000-000000000001', 'network_vpc_cidr',              'set',     now() - interval '90 days', now() - interval '90 days', NULL::timestamptz),
    ('a0000000-0000-4000-8000-000000000001', 'network_az_count',              'set',     now() - interval '90 days', now() - interval '12 days', NULL),
    ('a0000000-0000-4000-8000-000000000002', 'dns_zone_name',                 'set',     now() - interval '88 days', now() - interval '88 days', NULL),
    ('a0000000-0000-4000-8000-000000000003', 'k8s_version',                   'set',     now() - interval '75 days', now() - interval '1 day',   NULL),
    ('a0000000-0000-4000-8000-000000000003', 'k8s_node_count',                'pending', now() - interval '75 days', now() - interval '30 minutes', NULL),
    ('a0000000-0000-4000-8000-000000000004', 'payments_api_instance_type',    'set',     now() - interval '60 days', now() - interval '60 days', NULL),
    ('a0000000-0000-4000-8000-000000000004', 'payments_api_replicas',         'set',     now() - interval '60 days', now() - interval '2 days',  NULL),
    ('a0000000-0000-4000-8000-000000000005', 'payments_db_instance_class',    'set',     now() - interval '45 days', now() - interval '45 days', NULL),
    ('a0000000-0000-4000-8000-000000000005', 'payments_db_backup_retention',  'set',     now() - interval '45 days', now() - interval '45 days', NULL),
    ('a0000000-0000-4000-8000-000000000006', 'payments_sandbox_ttl_hours',    'pending', now() - interval '30 days', now() - interval '30 days', NULL),
    ('a0000000-0000-4000-8000-000000000007', 'warehouse_dataset',             'set',     now() - interval '40 days', now() - interval '40 days', NULL),
    ('a0000000-0000-4000-8000-000000000007', 'warehouse_location',            'set',     now() - interval '40 days', now() - interval '40 days', NULL),
    ('a0000000-0000-4000-8000-000000000008', 'airflow_dag_bucket',            'pending', now() - interval '12 days', now() - interval '12 days', NULL),
    ('a0000000-0000-4000-8000-000000000010', 'alice_playground_region',       'set',     now() - interval '25 days', now() - interval '25 days', NULL),
    -- deleted with old-vpn: the name is free again
    ('a0000000-0000-4000-8000-000000000012', 'vpn_psk_ref',                   'set',     now() - interval '500 days', now() - interval '500 days', now() - interval '30 seconds')
) AS v (workspace_id, name, status, created_at, updated_at, deleted_at)
WHERE NOT EXISTS (
    SELECT 1 FROM variables x WHERE x.workspace_id = v.workspace_id::uuid AND x.name = v.name
);

COMMIT;
