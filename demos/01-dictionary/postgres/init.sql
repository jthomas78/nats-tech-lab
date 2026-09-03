-- Phase 53 / ADR-052: one Postgres instance, one database and one role per
-- service. Runs once, on an empty `pg-data` volume, via
-- /docker-entrypoint-initdb.d/. Services still run their own idempotent
-- migrations at startup; this file only creates the containers they run in.
--
-- Isolation is credential-enforced: CONNECT on every database is revoked
-- from PUBLIC and granted back to the owning role only, so a service role
-- cannot reach another service's tables even by accident.
--
-- Passwords equal role names — lab convention, matching the old per-instance
-- POSTGRES_PASSWORD values.

CREATE ROLE dict         LOGIN PASSWORD 'dict';
CREATE ROLE refdata      LOGIN PASSWORD 'refdata';
CREATE ROLE accounts     LOGIN PASSWORD 'accounts';
CREATE ROLE mfe_registry LOGIN PASSWORD 'mfe_registry';
CREATE ROLE pricing      LOGIN PASSWORD 'pricing';
CREATE ROLE organizations LOGIN PASSWORD 'organizations';

CREATE DATABASE dictionary    OWNER dict;
CREATE DATABASE refdata       OWNER refdata;
CREATE DATABASE accounts      OWNER accounts;
CREATE DATABASE mfe_registry  OWNER mfe_registry;
CREATE DATABASE pricing       OWNER pricing;
CREATE DATABASE organizations OWNER organizations;

REVOKE CONNECT ON DATABASE dictionary    FROM PUBLIC;
REVOKE CONNECT ON DATABASE refdata       FROM PUBLIC;
REVOKE CONNECT ON DATABASE accounts      FROM PUBLIC;
REVOKE CONNECT ON DATABASE mfe_registry  FROM PUBLIC;
REVOKE CONNECT ON DATABASE pricing       FROM PUBLIC;
REVOKE CONNECT ON DATABASE organizations FROM PUBLIC;

GRANT CONNECT ON DATABASE dictionary    TO dict;
GRANT CONNECT ON DATABASE refdata       TO refdata;
GRANT CONNECT ON DATABASE accounts      TO accounts;
GRANT CONNECT ON DATABASE mfe_registry  TO mfe_registry;
GRANT CONNECT ON DATABASE pricing       TO pricing;
GRANT CONNECT ON DATABASE organizations TO organizations;
