# PostgreSQL bootstrap and recovery

`database-bootstrap` runs after PostgreSQL accepts connections. It reads one admin
password and five service-role passwords from Compose secrets, creates/rotates the
five login roles, and assigns each schema to its matching service role. Each Go
service remains responsible for its own GORM migrations.

The current GORM PostgreSQL migrator executes `CREATE SCHEMA IF NOT EXISTS` for a
service schema on every startup migration, even when the schema already exists.
PostgreSQL therefore requires database-level `CREATE` for each service role. This
allows a role to create an additional empty schema; it does not grant `USAGE`, table
privileges, or ownership on another service's schema. Each role receives `USAGE`
and schema `CREATE` only on its own schema. Revoke database-level `CREATE` if the
migrator no longer needs it.

The bootstrap container is one-shot. If you explicitly rerun it, it reapplies the
passwords from the currently mounted secret files and reasserts role/schema grants.
An already initialized database volume does not rerun PostgreSQL image init files;
the Compose bootstrap service is the repeatable role/schema step.

The deployment README documents the custom-format `pg_dump` and `pg_restore`
commands. Database archives exclude ACLs and do not include TLS/service secrets,
Mosquitto state, or the evidence volume. Restore only after confirming the target
database and service-role files. See [deployment operations](../README.md).
