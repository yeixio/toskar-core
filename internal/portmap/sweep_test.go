package portmap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A permanent mapping a crash left is removed at the next start, only when
// the router still forwards it here under Toskar's name (#1262).
func TestSweepRemovesLeftPermanentMapping(t *testing.T) {
	for _, tc := range []struct {
		name, client, desc string
		deleted            bool
	}{
		{"ours", "127.0.0.1", "Toskar", true},
		{"another device's", "192.168.1.9", "Toskar", false},
		{"another program's", "127.0.0.1", "Plex", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var deleted []string
			mux := http.NewServeMux()
			mux.HandleFunc("/desc.xml", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `<?xml version="1.0"?><root><device><serviceList><service>
<serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType><controlURL>/ctl/IPConn</controlURL>
</service></serviceList></device></root>`)
			})
			mux.HandleFunc("/ctl/IPConn", func(w http.ResponseWriter, r *http.Request) {
				action := strings.Trim(strings.SplitN(r.Header.Get("SOAPAction"), "#", 2)[1], `"`)
				body, _ := io.ReadAll(r.Body)
				switch action {
				case "GetSpecificPortMappingEntry":
					fmt.Fprintf(w, `<NewInternalPort>7333</NewInternalPort><NewInternalClient>%s</NewInternalClient><NewPortMappingDescription>%s</NewPortMappingDescription>`, tc.client, tc.desc)
				case "DeletePortMapping":
					mu.Lock()
					deleted = append(deleted, between(string(body), "<NewExternalPort>", "</NewExternalPort>"))
					mu.Unlock()
				}
			})
			fakeIGD(t, mux)
			rec := filepath.Join(t.TempDir(), "port-mapping.json")
			if err := os.WriteFile(rec, []byte(`{"external_port":7333}`), 0o600); err != nil {
				t.Fatal(err)
			}
			k := &Keeper{Record: rec}
			k.Sweep(context.Background())
			mu.Lock()
			defer mu.Unlock()
			if got := strings.Join(deleted, ","); (got == "7333") != tc.deleted {
				t.Fatalf("deleted %q", got)
			}
			if _, err := os.Stat(rec); !os.IsNotExist(err) {
				t.Fatalf("the note is still there: %v", err)
			}
		})
	}
}

// A mapping with a lease leaves no note; a permanent one does.
func TestKeeperNotesOnlyPermanentMappings(t *testing.T) {
	rec := filepath.Join(t.TempDir(), "port-mapping.json")
	k := &Keeper{Record: rec}
	k.note(Mapping{Method: MethodUPnP, permanent: true}, nil)
	if _, err := os.Stat(rec); err != nil {
		t.Fatalf("no note for a permanent mapping: %v", err)
	}
	k.note(Mapping{Method: MethodUPnP}, nil)
	if _, err := os.Stat(rec); !os.IsNotExist(err) {
		t.Fatalf("a note for a leased mapping: %v", err)
	}
}
