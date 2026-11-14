# Platform implementations

Private PostgreSQL, broker, object-storage and observability implementations live here. Owner modules define the interfaces; domain packages never import this directory.

`internal/platform/postgres` owns database transactions, lock order, migrations, pooling, and SQL error translation. Domain callers use complete operations from owner-module interfaces.
