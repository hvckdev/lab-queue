\getenv accounts_password ACCOUNTS_DB_PASSWORD
\getenv queue_password QUEUE_DB_PASSWORD

CREATE USER labqueue_accounts WITH PASSWORD :'accounts_password';
CREATE USER labqueue_queue WITH PASSWORD :'queue_password';
CREATE DATABASE accounts OWNER labqueue_accounts;
CREATE DATABASE queue OWNER labqueue_queue;
REVOKE CONNECT ON DATABASE accounts FROM PUBLIC;
REVOKE CONNECT ON DATABASE queue FROM PUBLIC;
GRANT CONNECT ON DATABASE accounts TO labqueue_accounts;
GRANT CONNECT ON DATABASE queue TO labqueue_queue;
