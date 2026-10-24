package store

import (
	"strings"
	"testing"
)

func TestMigrationStatementsIncludeGroupObservationTables(t *testing.T) {
	statements := strings.Join(migrationStatements(), "\n")
	for _, table := range []string{"wecom_group_sync_state", "wecom_group_observations"} {
		if !strings.Contains(statements, table) {
			t.Fatalf("migration statements must create %s", table)
		}
	}
}
