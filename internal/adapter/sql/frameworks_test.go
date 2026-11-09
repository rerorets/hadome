package sql

import "testing"

// viewpoint: behaviour
// target: sql.tapFramework.Name
// Each way of writing tests in SQL is named as its users know it.
func TestFrameworkNames(t *testing.T) {
	got := []string{tapFramework{}.Name(), tsqltFramework{}.Name(), utplsqlFramework{}.Name()}
	want := []string{"pgTAP", "tSQLt", "utPLSQL"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("framework %d is named %q, want %q", i, got[i], want[i])
		}
	}
}
