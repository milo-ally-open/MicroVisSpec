# PostgreSQL bootstrap and recovery

Run `database-bootstrap.sh` once PostgreSQL is ready, as the Linux `postgres` OS
user. It reads five service-role password files from `/etc/microvisspec`, creates
or rotates the login roles, and assigns each schema to its matching role. Each Go
service remains responsible for its own GORM migrations. The command and required
file permissions are documented in the repository [deployment guide](../../../../DEPLOY.md).

The current GORM PostgreSQL migrator executes `CREATE SCHEMA IF NOT EXISTS` during
startup migration. Each service role therefore needs database-level `CREATE` as
well as `USAGE` and `CREATE` on its own schema. It cannot access another service's
schema. Revoke database-level `CREATE` if the migrator no longer needs it.

The database bootstrap script is safe to rerun: it reapplies passwords from the
current protected files and reasserts role/schema grants. PostgreSQL data remains
in the host's normal data directory. Custom-format database backups do not include
TLS/service secrets, Mosquitto state, or the evidence directory; back those up
separately when required. See [deployment operations](../../../../DEPLOY.md).
