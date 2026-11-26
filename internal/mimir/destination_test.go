package mimir

import "testing"

// Refreshing a knowledge source records where it went, without the
// connection string's secrets (#1415).
func TestRemoteDestination(t *testing.T) {
	for _, c := range []struct {
		src  Source
		want string
	}{
		{Source{Name: "Tickets", Kind: KindAPI, Remote: &Remote{URL: "https://api.example.com/v1/tickets?key=secret"}}, "api.example.com"},
		{Source{Name: "Sales", Kind: KindDatabase, Remote: &Remote{Driver: DriverPostgres}}, "PostgreSQL (Sales)"},
		{Source{Name: "Shop", Kind: KindDatabase, Remote: &Remote{Driver: DriverMySQL}}, "MySQL (Shop)"},
		{Source{Name: "Local", Kind: KindDatabase, Remote: &Remote{Driver: DriverSQLite}}, ""},
		{Source{Name: "Notes"}, ""},
	} {
		if got := remoteDestination(c.src); got != c.want {
			t.Errorf("%s: %q, want %q", c.src.Name, got, c.want)
		}
	}
}
