package postgres

import "testing"

func TestMigrationInventory(t *testing.T) {
	t.Parallel()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != int(CurrentSchemaVersion) {
		t.Fatalf("migration count = %d, current version = %d", len(migrations), CurrentSchemaVersion)
	}
	if migrations[0].Name != "foundation" || len(migrations[0].Checksum) != 64 {
		t.Fatalf("migration = %#v", migrations[0])
	}
}
